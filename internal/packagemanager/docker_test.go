package packagemanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerParsing(t *testing.T) {
	build := "# syntax=docker/dockerfile:1\r\nARG TAG=22\r\nFROM --platform=$BUILDPLATFORM node:22.1-alpine AS build\r\nRUN echo hi\r\nFROM build AS again\r\nFROM scratch\r\nFROM \\\r\n  ghcr.io/org/app:1.2.3\r\n"
	sources, err := dockerParse("Dockerfile", []byte(build))
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || sources[0].ref != "node:22.1-alpine" || sources[1].ref != "ghcr.io/org/app:1.2.3" || sources[1].name != "stage:3" {
		t.Fatalf("sources = %+v", sources)
	}
	for _, s := range sources {
		if build[s.start:s.end] != s.ref {
			t.Fatalf("bad span %+v", s)
		}
	}
	compose := "x-meta:\n  image: ignored:9\nservices:\n  web: {image: 'node:22.1-alpine', environment: {image: ignored:2}} # keep\n  worker:\n    image: \"ghcr.io/org/app:1.2.3\" # pinned\n"
	sources, err = dockerParse("compose.yaml", []byte(compose))
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || sources[0].name != "service:web" || sources[1].name != "service:worker" {
		t.Fatalf("sources=%+v", sources)
	}
	for _, text := range []string{"services: {web: &web {image: 'node:22.1'}, other: {environment: *web}}", "services: &services {web: {image: 'node:22.1'}}", "&root {services: {web: {image: 'node:22.1'}}}", "services: {web: {image: '${IMAGE}'}}", "services: {web: {image: &app 'node:22'}}", "services: {web: {image: node:22, image: node:23}}", "services: {web: {image: |\n      node:22\n}}", "services: {web: {<<: *other}}", "services: {web: {image: node:22}}\n---\nservices: {}"} {
		if _, err := dockerParse("compose.yml", []byte(text)); err == nil {
			t.Errorf("accepted %q", text)
		}
	}
	for _, text := range []string{"ARG V=22\nFROM node:$V\n", "FROM node:22 AS build\nFROM build AS build\n", "FROM\n", "RUN <<EOF\nFROM fake:1\nEOF\n"} {
		if _, err := dockerParse("Containerfile", []byte(text)); err == nil {
			t.Errorf("accepted %q", text)
		}
	}
}

func TestDockerVersionsAndReferences(t *testing.T) {
	for _, tag := range []string{"1", "22.1", "24.04", "1.2.3-alpine3.20", "1.2-bookworm-slim"} {
		if _, ok := dockerParseVersion(tag); !ok {
			t.Errorf("rejected %s", tag)
		}
	}
	for _, tag := range []string{"latest", "v1.2.3", "1.2.3.4", "1.2.3-rc1", "1.2-beta", "01.2", "1.2.3-dev", "1.2.3-alpine-rc1"} {
		if _, ok := dockerParseVersion(tag); ok {
			t.Errorf("accepted unstable %s", tag)
		}
	}
	r, err := dockerParseReference("ubuntu:24.04")
	if err != nil || r.registry != "registry-1.docker.io" || r.repository != "library/ubuntu" {
		t.Fatalf("ref %+v %v", r, err)
	}
	r, err = dockerParseReference("docker.io/ubuntu:24.04")
	if err != nil || r.repository != "library/ubuntu" {
		t.Fatalf("ref %+v %v", r, err)
	}
	r, err = dockerParseReference("registry.example:5443/org/app:1.2")
	if err != nil || r.registry != "registry.example:5443" {
		t.Fatalf("ref %+v %v", r, err)
	}
	for _, ref := range []string{"https://registry.io/app:1", "user:pass@registry.io/app", "node:${TAG}", "node:1@sha256:no", "registry.io/../app:1", "Node:1", "app:1\n"} {
		if _, err := dockerParseReference(ref); err == nil {
			t.Errorf("accepted ref %q", ref)
		}
	}
	for _, tc := range []struct {
		a, b        string
		bump, valid bool
	}{
		{"ubuntu:24.04", "ubuntu:24.10", false, true}, {"ubuntu:24.04", "ubuntu:26.04", true, true}, {"ubuntu:24.04", "ubuntu:26.04", false, false},
		{"node:22.1", "node:22.2", false, true}, {"node:22.1", "node:23.1", false, false}, {"node:22.1", "node:23.1", true, true}, {"node:22", "node:23", false, false}, {"node:22", "node:23", true, true}, {"node:22.1-alpine", "node:22.2-slim", true, false}, {"node:22.1", "node:22.2.0", true, false}, {"node:22.1", "other:22.2", true, false}, {"node:22.1", "node:22.1", false, false},
	} {
		err := dockerValidateChange(tc.a, tc.b, tc.bump)
		if (err == nil) != tc.valid {
			t.Errorf("validate %+v: %v", tc, err)
		}
	}
}

func dockerTestRegistry(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	old := dockerRegistryTransport
	dockerRegistryTransport = server.Client().Transport
	t.Cleanup(func() { dockerRegistryTransport = old; server.Close() })
	return server
}
func dockerTestHost(server *httptest.Server) string {
	return strings.TrimPrefix(server.URL, "https://")
}

func TestDockerRegistryVersionsAndDigests(t *testing.T) {
	manifest := `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}`
	sum := sha256.Sum256([]byte(manifest))
	digest := "sha256:" + hex.EncodeToString(sum[:])
	oldDigest := "sha256:" + strings.Repeat("0", 64)
	tagCalls, manifestCalls := 0, 0
	server := dockerTestRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/org/app/tags/list" {
			tagCalls++
			if r.URL.Query().Get("last") == "1.2" {
				fmt.Fprint(w, `{"name":"org/app","tags":["1.4","2.1","2.2-rc1","1.5-alpine","2","3","1.9.9"]}`)
			} else {
				w.Header().Set("Link", `</v2/org/app/tags/list?n=1000&last=1.2>; rel="next"`)
				fmt.Fprint(w, `{"name":"org/app","tags":["1.2","1.3"]}`)
			}
		} else if strings.HasPrefix(r.URL.Path, "/v2/org/app/manifests/") {
			manifestCalls++
			w.Header().Set("Docker-Content-Digest", digest)
			fmt.Fprint(w, manifest)
		} else {
			http.NotFound(w, r)
		}
	})
	prefix := dockerTestHost(server) + "/org/app:"
	for _, tc := range []struct {
		tag             string
		bump            bool
		want            string
		tags, manifests bool
	}{
		{"1.2", false, "1.4", true, false}, {"1.2", true, "2.1", true, false}, {"2", false, "", false, false}, {"2", true, "3", true, false}, {"latest", false, "", false, false}, {"1.2@" + oldDigest, false, "1.2@" + digest, false, true}, {"1.2@" + oldDigest, true, "2.1@" + digest, true, true}, {"weird@" + oldDigest, false, "weird@" + digest, false, true}, {"1.2@" + digest, false, "", false, true},
	} {
		beforeTags, beforeManifests := tagCalls, manifestCalls
		latest, err := dockerLatest(context.Background(), prefix+tc.tag, tc.bump)
		if err != nil {
			t.Fatal(err)
		}
		want := ""
		if tc.want != "" {
			want = prefix + tc.want
		}
		if latest != want {
			t.Errorf("%+v latest=%s want=%s", tc, latest, want)
		}
		if (tagCalls > beforeTags) != tc.tags || (manifestCalls > beforeManifests) != tc.manifests {
			t.Errorf("unexpected requests %+v", tc)
		}
	}
	if latest, err := dockerLatest(context.Background(), dockerTestHost(server)+"/org/app@"+oldDigest, false); err != nil || latest != "" {
		t.Fatalf("digest-only %s %v", latest, err)
	}
}

func TestDockerRegistryAuthenticationAndBoundaries(t *testing.T) {
	var server *httptest.Server
	authenticated := false
	server = dockerTestRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			if r.Header.Get("Authorization") != "" || r.URL.Query().Get("scope") != "repository:org/app:pull" {
				t.Error("unsafe token request")
			}
			fmt.Fprint(w, `{"access_token":"public-token"}`)
			return
		}
		if r.Header.Get("Authorization") == "Bearer public-token" {
			authenticated = true
			fmt.Fprint(w, `{"tags":["1.2"]}`)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="`+server.URL+`/token",service="registry",scope="repository:evil:push"`)
		w.WriteHeader(401)
	})
	ref, _ := dockerParseReference(dockerTestHost(server) + "/org/app:1.1")
	if _, err := dockerTags(context.Background(), ref); err != nil || !authenticated {
		t.Fatalf("auth %v %v", authenticated, err)
	}
	for _, test := range []string{"realm", "basic", "redirect", "pagination", "loop", "digest", "private", "oversize"} {
		t.Run(test, func(t *testing.T) {
			server := dockerTestRegistry(t, func(w http.ResponseWriter, r *http.Request) {
				switch test {
				case "realm":
					w.Header().Set("WWW-Authenticate", `Bearer realm="https://evil.example/token"`)
					w.WriteHeader(401)
				case "basic":
					w.Header().Set("WWW-Authenticate", `Basic realm="private"`)
					w.WriteHeader(401)
				case "redirect":
					http.Redirect(w, r, "https://evil.example/", 302)
				case "pagination":
					w.Header().Set("Link", `<https://evil.example/v2/org/app/tags/list>; rel="next"`)
					fmt.Fprint(w, `{"tags":[]}`)
				case "loop":
					w.Header().Set("Link", `</v2/org/app/tags/list?n=1000>; rel="next"`)
					fmt.Fprint(w, `{"tags":[]}`)
				case "digest":
					w.Header().Set("Docker-Content-Digest", "sha256:"+strings.Repeat("0", 64))
					fmt.Fprint(w, `{"schemaVersion":2}`)
				case "private":
					w.WriteHeader(403)
				case "oversize":
					fmt.Fprint(w, strings.Repeat("x", (4<<20)+1))
				}
			})
			ref, _ := dockerParseReference(dockerTestHost(server) + "/org/app:1.1")
			var err error
			if test == "digest" {
				_, err = dockerManifestDigest(context.Background(), ref, "1.1")
			} else {
				_, err = dockerTags(context.Background(), ref)
			}
			if err == nil {
				t.Fatal("unsafe response accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dockerTags(ctx, ref); err == nil {
		t.Fatal("cancel ignored")
	}
}

func TestDockerNativeEndToEnd(t *testing.T) {
	server := dockerTestRegistry(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"tags":["1.1","1.2","2.1"]}`) })
	root := t.TempDir()
	prefix := dockerTestHost(server) + "/org/app:"
	build := "FROM " + prefix + "1.1 AS builder\r\nFROM builder\r\n"
	compose := "x-meta: {image: untouched:9}\nservices:\n  web: {image: '" + prefix + "1.1'} # keep\n"
	for name, data := range map[string]string{"Dockerfile": build, "compose.yaml": compose} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0640); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "Containerfile"), []byte("FROM invalid:$TAG\n"), 0600); err != nil {
		t.Fatal(err)
	}
	scope := Scope{Kind: "project", Path: root}
	packages := Packages{}
	if err := (Docker{}).listOutdatedScope(context.Background(), "", scope, ManagerOptions{}, packages); err != nil {
		t.Fatal(err)
	}
	records := Records(packages)
	if len(records) != 2 {
		t.Fatalf("records %+v", records)
	}
	for _, p := range records {
		if p.Current != prefix+"1.1" || p.Latest != prefix+"1.2" || p.Executable != "" || p.Manifest == "" {
			t.Fatalf("record %+v", p)
		}
	}
	plan := UpdatePlan{Manager: PM_DOCKER, Scope: "project", Directory: root, Packages: records}
	if err := dockerVerify(plan); err == nil {
		t.Fatal("verified unapplied plan")
	}
	if err := dockerApply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if err := dockerVerify(plan); err != nil {
		t.Fatal(err)
	}
	for name, before := range map[string]string{"Dockerfile": build, "compose.yaml": compose} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		want := strings.ReplaceAll(before, prefix+"1.1", prefix+"1.2")
		if string(data) != want {
			t.Fatalf("rewrite %s=%s", name, data)
		}
		info, _ := os.Stat(filepath.Join(root, name))
		if info.Mode().Perm() != 0640 {
			t.Fatal("mode lost")
		}
	}
	if err := dockerApply(context.Background(), plan); err == nil {
		t.Fatal("stale plan accepted")
	}
	inventory, err := (Docker{}).listInstalled(context.Background(), "", scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range records {
		found := false
		for _, installed := range inventory {
			if installed.Name == p.Name {
				found = true
			}
		}
		if !found {
			t.Fatal("identity changed")
		}
	}
	packages = Packages{}
	if err := (Docker{}).listOutdatedScope(context.Background(), "", scope, ManagerOptions{IncludeBumpCandidates: true}, packages); err != nil {
		t.Fatal(err)
	}
	bump := UpdatePlan{Manager: PM_DOCKER, Scope: "project", Directory: root, Bump: true, Packages: Records(packages)}
	if err := dockerApply(context.Background(), bump); err != nil {
		t.Fatal(err)
	}
	if err := dockerVerify(bump); err != nil {
		t.Fatal(err)
	}
}

func TestDockerApplyValidationBeforeWrites(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "Dockerfile")
	second := filepath.Join(root, "compose.yaml")
	if err := os.WriteFile(first, []byte("FROM node:22.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("services: {web: {image: node:22.1}}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p := Package{Manager: PM_DOCKER, Name: "Dockerfile#stage:0", Manifest: first, Current: "node:22.1", Latest: "node:22.2"}
	q := Package{Manager: PM_DOCKER, Name: "compose.yaml#service:web", Manifest: second, Current: "node:21.1", Latest: "node:21.2"}
	plan := UpdatePlan{Manager: PM_DOCKER, Scope: "project", Directory: root, Packages: []Package{p, q}}
	if err := dockerApply(context.Background(), plan); err == nil {
		t.Fatal("stale file accepted")
	}
	data, _ := os.ReadFile(first)
	if string(data) != "FROM node:22.1\n" {
		t.Fatal("partial validation write")
	}
	plan.Packages = []Package{p}
	plan.Packages[0].Manifest = filepath.Join(root, "..", "Dockerfile")
	if err := dockerApply(context.Background(), plan); err == nil {
		t.Fatal("path escape accepted")
	}
	if err := os.Remove(second); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(first, second); err != nil {
		t.Fatal(err)
	}
	if _, err := dockerDiscover(Scope{Kind: "workspace", Path: root}); err == nil {
		t.Fatal("symlink accepted")
	}
}
