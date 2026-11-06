package packagemanager

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"
	"golang.org/x/mod/semver"
)

type helmEntry struct {
	Name       string   `yaml:"name"`
	Version    string   `yaml:"version"`
	URLs       []string `yaml:"urls"`
	Digest     string   `yaml:"digest,omitempty"`
	APIVersion string   `yaml:"apiVersion,omitempty"`
}
type helmIndex struct {
	APIVersion string                 `yaml:"apiVersion"`
	Entries    map[string][]helmEntry `yaml:"entries"`
}

var helmRegistryTransport http.RoundTripper = http.DefaultTransport

var helmVersionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(\+[0-9A-Za-z.-]+)?$`)

func helmStable(v string) bool {
	return helmVersionPattern.MatchString(v) && semver.IsValid("v"+strings.TrimPrefix(v, "v"))
}
func helmCompare(a, b string) int {
	return semver.Compare("v"+strings.TrimPrefix(a, "v"), "v"+strings.TrimPrefix(b, "v"))
}

var helmTermPattern = regexp.MustCompile(`^(>=|<=|>|<|=|~|\^)?\s*(v?(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:\+[0-9A-Za-z.-]+)?)(\s*,\s*|\s+|$)`)

func helmConstraint(s string) (func(string) bool, error) {
	type term struct{ op, v string }
	var terms []term
	rest := strings.TrimSpace(s)
	for rest != "" {
		m := helmTermPattern.FindStringSubmatch(rest)
		if m == nil || !helmStable(m[2]) {
			return nil, fmt.Errorf("helm: unsupported constraint %q", s)
		}
		terms = append(terms, term{m[1], m[2]})
		rest = rest[len(m[0]):]
		if rest == "" && strings.Contains(m[3], ",") {
			return nil, fmt.Errorf("helm: unsupported constraint %q", s)
		}
	}
	if len(terms) == 0 {
		return nil, fmt.Errorf("helm: empty constraint")
	}
	return func(v string) bool {
		if !helmStable(v) {
			return false
		}
		for _, t := range terms {
			c := helmCompare(v, t.v)
			ok := false
			switch t.op {
			case "", "=":
				ok = c == 0
			case ">":
				ok = c > 0
			case ">=":
				ok = c >= 0
			case "<":
				ok = c < 0
			case "<=":
				ok = c <= 0
			case "~", "^":
				a := strings.Split(strings.Split(strings.TrimPrefix(t.v, "v"), "+")[0], ".")
				b := strings.Split(strings.Split(strings.TrimPrefix(v, "v"), "+")[0], ".")
				ok = c >= 0 && a[0] == b[0]
				if t.op == "~" || a[0] == "0" {
					ok = ok && a[1] == b[1]
				}
				if t.op == "^" && a[0] == "0" && a[1] == "0" {
					ok = ok && a[2] == b[2]
				}
			}
			if !ok {
				return false
			}
		}
		return true
	}, nil
}
func helmHTTPS(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("helm: only anonymous HTTPS classic repository URLs supported (no OCI, aliases, local paths or credentials)")
	}
	return u, nil
}
func helmFetchIndex(ctx context.Context, repository string) (helmIndex, error) {
	var index helmIndex
	u, err := helmHTTPS(repository)
	if err != nil {
		return index, err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/index.yaml"
	u.RawPath = ""
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client := &http.Client{Transport: helmRegistryTransport, Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("helm: too many redirects")
		}
		if req.URL.Scheme != "https" || req.URL.Host != u.Host || req.URL.User != nil {
			return fmt.Errorf("helm: unsafe repository redirect")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return index, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return index, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return index, fmt.Errorf("helm: repository index HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil {
		return index, err
	}
	if len(b) > 16<<20 {
		return index, fmt.Errorf("helm: repository index exceeds 16 MiB")
	}
	if err = yaml.Unmarshal(b, &index); err != nil {
		return index, err
	}
	if index.APIVersion != "v1" || index.Entries == nil {
		return index, fmt.Errorf("helm: invalid repository index")
	}
	return index, nil
}
