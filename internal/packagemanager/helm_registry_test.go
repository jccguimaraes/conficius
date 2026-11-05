package packagemanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHelmRegistryConstraints(t *testing.T) {
	for _, tt := range []struct {
		constraint string
		matches    []string
		rejects    []string
	}{
		{"1.2.3", []string{"1.2.3", "v1.2.3+build"}, []string{"1.2.2", "1.2.4"}},
		{"= v1.2.3+build", []string{"1.2.3"}, []string{"1.2.4"}},
		{">1.2.3", []string{"1.2.4", "2.0.0"}, []string{"1.2.3"}},
		{">= 1.2.3", []string{"1.2.3", "10.0.0"}, []string{"1.2.2"}},
		{"<2.0.0", []string{"1.10.0"}, []string{"2.0.0", "10.0.0"}},
		{"<=2.0.0", []string{"1.10.0", "2.0.0"}, []string{"2.0.1"}},
		{">=1.2.3 <2.0.0", []string{"1.2.3", "1.10.0"}, []string{"1.2.2", "2.0.0"}},
		{" >= 1.2.3, < 2.0.0 ", []string{"1.2.3", "1.10.0"}, []string{"1.2.2", "2.0.0"}},
		{">=1.2.3,<=1.10.0", []string{"1.2.3", "1.10.0"}, []string{"1.10.1"}},
		{"~1.2.3", []string{"1.2.3", "1.2.10"}, []string{"1.2.2", "1.3.0", "2.2.3"}},
		{"~0.0.3", []string{"0.0.3", "0.0.10"}, []string{"0.0.2", "0.1.0"}},
		{"^1.2.3", []string{"1.2.3", "1.10.0"}, []string{"1.2.2", "2.0.0"}},
		{"^0.2.3", []string{"0.2.3", "0.2.10"}, []string{"0.2.2", "0.3.0"}},
		{"^0.0.3", []string{"0.0.3", "0.0.3+build"}, []string{"0.0.2", "0.0.4", "0.1.0"}},
	} {
		t.Run(tt.constraint, func(t *testing.T) {
			match, err := helmConstraint(tt.constraint)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range tt.matches {
				if !match(v) {
					t.Errorf("must match %q", v)
				}
			}
			for _, v := range append(tt.rejects, "1.2.3-rc.1", "01.2.3", "1.2", "garbage") {
				if match(v) {
					t.Errorf("must reject %q", v)
				}
			}
		})
	}
	for _, s := range []string{"", " ", ">=1.0.0<2.0.0", "1.0.0=1.0.0", ">=1.0.0,", ">=1.0.0, ", ">=1.0.0,,<2.0.0", ">=1.0.0, ,<2.0.0", ",1.0.0", "1.0.0;", "1.0.0 || 2.0.0", "1.0", "*", "1.x", "!=1.0.0", "1.0.0-rc.1", "01.0.0", "1.0.0+"} {
		if _, err := helmConstraint(s); err == nil {
			t.Errorf("accepted malformed or unsupported constraint %q", s)
		}
	}
}

func TestHelmRegistryHTTPS(t *testing.T) {
	for _, s := range []string{"http://example.test", "oci://example.test/chart", "alias/chart", "/tmp/chart", "https://user:pass@example.test", "https://example.test?token=secret", "https://example.test#fragment", "https:///chart"} {
		if _, err := helmHTTPS(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
	if _, err := helmHTTPS("https://127.0.0.1/charts"); err != nil {
		t.Fatal(err)
	}
}

func TestHelmRegistryIndex(t *testing.T) {
	const valid = "apiVersion: v1\nentries:\n  demo:\n    - name: demo\n      version: 1.2.3\n      urls: [demo-1.2.3.tgz]\n"
	var base string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("index request must be anonymous")
		}
		switch r.URL.Path {
		case "/charts/index.yaml", "/redirected":
			_, _ = w.Write([]byte(valid))
		case "/redirect/index.yaml":
			http.Redirect(w, r, "/redirected", http.StatusFound)
		case "/downgrade/index.yaml":
			http.Redirect(w, r, strings.Replace(base, "https:", "http:", 1)+"/redirected", http.StatusFound)
		case "/host/index.yaml":
			http.Redirect(w, r, strings.Replace(base, "127.0.0.1", "localhost", 1)+"/redirected", http.StatusFound)
		case "/credentials/index.yaml":
			http.Redirect(w, r, strings.Replace(base, "https://", "https://user:pass@", 1)+"/redirected", http.StatusFound)
		case "/loop/index.yaml":
			http.Redirect(w, r, "/loop/index.yaml", http.StatusFound)
		case "/auth/index.yaml":
			w.Header().Set("WWW-Authenticate", `Basic realm="charts"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/large/index.yaml":
			_, _ = w.Write([]byte(strings.Repeat(" ", (16<<20)+1)))
		case "/invalid/index.yaml":
			_, _ = w.Write([]byte("entries: ["))
		case "/version/index.yaml":
			_, _ = w.Write([]byte("apiVersion: v2\nentries: {}\n"))
		case "/missing/index.yaml":
			_, _ = w.Write([]byte("apiVersion: v1\n"))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	base = server.URL
	old := helmRegistryTransport
	helmRegistryTransport = server.Client().Transport
	t.Cleanup(func() { helmRegistryTransport = old; server.Close() })
	for _, path := range []string{"/charts/", "/redirect"} {
		index, err := helmFetchIndex(context.Background(), base+path)
		if err != nil {
			t.Fatal(err)
		}
		if entries := index.Entries["demo"]; len(entries) != 1 || entries[0].Version != "1.2.3" {
			t.Fatalf("unexpected index: %+v", index)
		}
	}
	for _, tt := range []struct{ path, message string }{
		{"/downgrade", "unsafe repository redirect"},
		{"/host", "unsafe repository redirect"},
		{"/credentials", "unsafe repository redirect"},
		{"/loop", "too many redirects"},
		{"/auth", "HTTP 401"},
		{"/large", "exceeds 16 MiB"},
		{"/invalid", ""},
		{"/version", "invalid repository index"},
		{"/missing", "invalid repository index"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			if _, err := helmFetchIndex(context.Background(), base+tt.path); err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("want error containing %q, got %v", tt.message, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := helmFetchIndex(ctx, base+"/charts"); err == nil {
		t.Fatal("canceled request succeeded")
	}
}
