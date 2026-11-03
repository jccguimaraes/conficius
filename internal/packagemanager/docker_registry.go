package packagemanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var dockerRegistryTransport http.RoundTripper = http.DefaultTransport

var dockerTagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
var dockerRepositoryPattern = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*)*$`)
var dockerDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var dockerVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(?:\.([0-9]+))?(?:\.([0-9]+))?(-[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*)?$`)

type dockerReference struct{ original, registry, repository, tag, digest string }

func dockerParseReference(value string) (dockerReference, error) {
	r := dockerReference{}
	if value == "" || strings.ContainsAny(value, "$`\\ \t\r\n?#%") {
		return r, fmt.Errorf("docker: invalid or dynamic image reference %q (interpolation unsupported)", value)
	}
	base, digest, pinned := strings.Cut(value, "@")
	if pinned {
		if !dockerDigestPattern.MatchString(digest) {
			return r, fmt.Errorf("docker: only sha256 image digests supported")
		}
		r.digest = digest
	}
	r.tag = "latest"
	if pinned {
		r.tag = ""
	}
	lastSlash := strings.LastIndex(base, "/")
	lastColon := strings.LastIndex(base, ":")
	if lastColon > lastSlash {
		r.tag = base[lastColon+1:]
		base = base[:lastColon]
	}
	if r.tag != "" && !dockerTagPattern.MatchString(r.tag) {
		return r, fmt.Errorf("docker: invalid image tag")
	}
	r.original = base
	r.registry = "registry-1.docker.io"
	r.repository = base
	first, rest, slash := strings.Cut(base, "/")
	if slash && (strings.ContainsAny(first, ".:") || first == "localhost") {
		u, err := url.Parse("https://" + first)
		if err != nil || u.Host != first || u.User != nil || u.Hostname() == "" {
			return r, fmt.Errorf("docker: invalid registry host")
		}
		if port := u.Port(); port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				return r, fmt.Errorf("docker: invalid registry port")
			}
		}
		r.registry = first
		r.repository = rest
	}
	if r.registry == "docker.io" || r.registry == "index.docker.io" {
		r.registry = "registry-1.docker.io"
	}
	if r.registry == "registry-1.docker.io" && !strings.Contains(r.repository, "/") {
		r.repository = "library/" + r.repository
	}
	if len(r.repository) > 255 || !dockerRepositoryPattern.MatchString(r.repository) {
		return r, fmt.Errorf("docker: invalid image repository")
	}
	return r, nil
}

func (r dockerReference) reference(tag, digest string) string {
	value := r.original + ":" + tag
	if digest != "" {
		value += "@" + digest
	}
	return value
}

type dockerVersion struct {
	parts     [3]uint64
	precision int
	suffix    string
}

func dockerParseVersion(tag string) (dockerVersion, bool) {
	v := dockerVersion{}
	m := dockerVersionPattern.FindStringSubmatch(tag)
	if m == nil {
		return v, false
	}
	v.suffix = m[4]
	for _, part := range strings.FieldsFunc(strings.TrimPrefix(v.suffix, "-"), func(r rune) bool { return r == '.' || r == '-' }) {
		if strings.HasPrefix(part, "rc") || strings.HasPrefix(part, "alpha") || strings.HasPrefix(part, "beta") || strings.HasPrefix(part, "preview") || strings.HasPrefix(part, "pre") || strings.HasPrefix(part, "dev") || strings.HasPrefix(part, "nightly") || strings.HasPrefix(part, "canary") || strings.HasPrefix(part, "snapshot") {
			return v, false
		}
	}
	for i := 0; i < 3 && m[i+1] != ""; i++ {
		n, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return v, false
		}
		v.parts[i] = n
		v.precision++
	}
	return v, true
}
func (v dockerVersion) newer(other dockerVersion) bool {
	for i, n := range v.parts {
		if n != other.parts[i] {
			return n > other.parts[i]
		}
	}
	return false
}

func dockerLatest(ctx context.Context, current string, bump bool) (string, error) {
	r, err := dockerParseReference(current)
	if err != nil {
		return "", err
	}
	if r.tag == "" {
		return "", nil
	}
	v, known := dockerParseVersion(r.tag)
	tag := r.tag
	if known && (bump || v.precision > 1) && (bump || r.digest == "") {
		tags, err := dockerTags(ctx, r)
		if err != nil {
			return "", err
		}
		best := v
		for _, candidate := range tags {
			cv, ok := dockerParseVersion(candidate)
			if ok && cv.precision == v.precision && cv.suffix == v.suffix && (bump || cv.parts[0] == v.parts[0]) && cv.newer(best) {
				tag = candidate
				best = cv
			}
		}
	}
	if r.digest != "" {
		digest, err := dockerManifestDigest(ctx, r, tag)
		if err != nil {
			return "", err
		}
		if tag == r.tag && digest == r.digest {
			return "", nil
		}
		return r.reference(tag, digest), nil
	}
	if tag == r.tag {
		return "", nil
	}
	return r.reference(tag, ""), nil
}

func dockerHTTP(ctx context.Context, raw, token string) ([]byte, http.Header, int, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, nil, 0, fmt.Errorf("docker: unsafe registry URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json, application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := http.Client{Transport: dockerRegistryTransport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return fmt.Errorf("docker: registry redirects unsupported")
	}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("docker: registry request failed")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil {
		return nil, nil, 0, fmt.Errorf("docker: registry response read failed")
	}
	if len(data) > 4<<20 {
		return nil, nil, 0, fmt.Errorf("docker: registry response exceeds 4 MiB")
	}
	return data, resp.Header, resp.StatusCode, nil
}

func dockerRegistryGet(ctx context.Context, r dockerReference, raw string) ([]byte, http.Header, error) {
	data, headers, status, err := dockerHTTP(ctx, raw, "")
	if err != nil {
		return nil, nil, err
	}
	if status == http.StatusUnauthorized {
		challenge := headers.Get("WWW-Authenticate")
		scheme, params, ok := strings.Cut(challenge, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") {
			return nil, nil, fmt.Errorf("docker: private registry or unsupported authentication (public Bearer only)")
		}
		_, attrs, err := mime.ParseMediaType("application/auth;" + strings.ReplaceAll(params, ",", ";"))
		if err != nil {
			return nil, nil, fmt.Errorf("docker: malformed Bearer challenge")
		}
		realm, err := url.Parse(attrs["realm"])
		if err != nil || realm.Scheme != "https" || realm.Host == "" || realm.User != nil || realm.Fragment != "" {
			return nil, nil, fmt.Errorf("docker: unsafe Bearer realm")
		}
		safe := realm.Host == r.registry || (r.registry == "registry-1.docker.io" && realm.Host == "auth.docker.io")
		if !safe {
			return nil, nil, fmt.Errorf("docker: cross-origin Bearer realm unsupported")
		}
		if ip := net.ParseIP(realm.Hostname()); ip != nil && realm.Host != r.registry {
			return nil, nil, fmt.Errorf("docker: unsafe Bearer realm address")
		}
		q := realm.Query()
		q.Set("scope", "repository:"+r.repository+":pull")
		if service := attrs["service"]; service != "" {
			q.Set("service", service)
		}
		realm.RawQuery = q.Encode()
		auth, _, code, err := dockerHTTP(ctx, realm.String(), "")
		if err != nil {
			return nil, nil, err
		}
		if code != 200 {
			return nil, nil, fmt.Errorf("docker: anonymous token unavailable; private registries unsupported (HTTP %d)", code)
		}
		var response struct {
			Token       string `json:"token"`
			AccessToken string `json:"access_token"`
		}
		if err := json.Unmarshal(auth, &response); err != nil {
			return nil, nil, fmt.Errorf("docker: invalid token response")
		}
		token := response.Token
		if token == "" {
			token = response.AccessToken
		}
		if token == "" || len(token) > 65536 || strings.ContainsAny(token, "\r\n") {
			return nil, nil, fmt.Errorf("docker: invalid anonymous token")
		}
		data, headers, status, err = dockerHTTP(ctx, raw, token)
		if err != nil {
			return nil, nil, err
		}
	}
	if status != 200 {
		if status == 401 || status == 403 {
			return nil, nil, fmt.Errorf("docker: private repository unsupported (HTTP %d)", status)
		}
		return nil, nil, fmt.Errorf("docker: registry HTTP %d", status)
	}
	return data, headers, nil
}

func dockerTags(ctx context.Context, r dockerReference) ([]string, error) {
	base := "https://" + r.registry + "/v2/" + r.repository + "/tags/list"
	next := base + "?n=1000"
	seen := map[string]bool{}
	var tags []string
	for page := 0; next != ""; page++ {
		if page >= 32 || seen[next] {
			return nil, fmt.Errorf("docker: registry pagination limit or loop")
		}
		seen[next] = true
		data, headers, err := dockerRegistryGet(ctx, r, next)
		if err != nil {
			return nil, err
		}
		var response struct {
			Name string   `json:"name"`
			Tags []string `json:"tags"`
		}
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, fmt.Errorf("docker: invalid tag list")
		}
		if response.Name != "" && response.Name != r.repository {
			return nil, fmt.Errorf("docker: unexpected tag repository")
		}
		for _, tag := range response.Tags {
			if !dockerTagPattern.MatchString(tag) {
				return nil, fmt.Errorf("docker: invalid registry tag")
			}
		}
		tags = append(tags, response.Tags...)
		if len(tags) > 20000 {
			return nil, fmt.Errorf("docker: registry tag count exceeds 20000")
		}
		next = ""
		for _, link := range headers.Values("Link") {
			for _, entry := range strings.Split(link, ",") {
				parts := strings.Split(strings.TrimSpace(entry), ";")
				if len(parts) < 2 {
					return nil, fmt.Errorf("docker: malformed pagination link")
				}
				isNext := false
				for _, part := range parts[1:] {
					if strings.TrimSpace(part) == `rel="next"` || strings.TrimSpace(part) == "rel=next" {
						isNext = true
					}
				}
				if !isNext {
					continue
				}
				target := strings.TrimSpace(parts[0])
				if !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") || next != "" {
					return nil, fmt.Errorf("docker: malformed next link")
				}
				origin, _ := url.Parse(base)
				u, err := url.Parse(target[1 : len(target)-1])
				if err != nil {
					return nil, fmt.Errorf("docker: invalid pagination URL")
				}
				u = origin.ResolveReference(u)
				if u.Scheme != "https" || u.Host != origin.Host || u.User != nil || u.Fragment != "" || u.Path != origin.Path || u.RawPath != "" {
					return nil, fmt.Errorf("docker: unsafe pagination URL")
				}
				next = u.String()
			}
		}
	}
	return tags, nil
}

func dockerManifestDigest(ctx context.Context, r dockerReference, tag string) (string, error) {
	data, headers, err := dockerRegistryGet(ctx, r, "https://"+r.registry+"/v2/"+r.repository+"/manifests/"+tag)
	if err != nil {
		return "", err
	}
	var manifest struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.SchemaVersion != 2 {
		return "", fmt.Errorf("docker: invalid schema 2 manifest")
	}
	sum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if declared := headers.Get("Docker-Content-Digest"); declared != "" && declared != digest {
		return "", fmt.Errorf("docker: manifest digest mismatch")
	}
	return digest, nil
}
