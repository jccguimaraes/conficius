package packagemanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelmConstraints(t *testing.T) {
	for _, tc := range []struct {
		constraint, version string
		want                bool
	}{
		{"~1.2.3", "1.2.9", true}, {"~1.2.3", "1.3.0", false}, {"^0.2.3", "0.3.0", false}, {"^0.0.3", "0.0.4", false}, {"^1.2.3", "1.9.0", true}, {">=1.2.3 <2.0.0", "1.4.0", true}, {">= 1.2.3, < 2.0.0", "2.0.0", false}, {"1.2.3", "1.2.4", false},
	} {
		match, err := helmConstraint(tc.constraint)
		if err != nil || match(tc.version) != tc.want {
			t.Fatalf("%s %s: %v", tc.constraint, tc.version, err)
		}
	}
	for _, s := range []string{"", "1.2", "*", "1.x", "1.0.0 || 2.0.0", "1.0.0-beta", "1.0.0 - 2.0.0"} {
		if _, err := helmConstraint(s); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
func helmTestProject(t *testing.T, repo string) string {
	t.Helper()
	root := t.TempDir()
	chart := "apiVersion: v2\nname: app\nversion: 1.0.0\n# preserve me\ndependencies:\n  - name: child\n    version: '~1.0.0' # range\n    repository: " + repo + "\n  - name: other\n    version: ^1.0.0\n    repository: " + repo + "\n"
	lock := "dependencies:\n- name: child\n  version: 1.0.0\n  repository: " + repo + "\n- name: other\n  version: 1.0.0\n  repository: " + repo + "\n"
	for name, b := range map[string]string{"Chart.yaml": chart, "Chart.lock": lock} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(b), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
func helmTestIndex(t *testing.T) string {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index.yaml" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("apiVersion: v1\nentries:\n  child:\n  - {name: child, version: 1.0.0, urls: [child-1.0.0.tgz]}\n  - {name: child, version: 1.0.1, urls: [child-1.0.1.tgz]}\n  - {name: child, version: 2.0.0, urls: [child-2.0.0.tgz]}\n  other:\n  - {name: other, version: 1.0.0, urls: [other.tgz]}\n  - {name: other, version: 1.9.0, urls: [other-new.tgz]}\n"))
	}))
	old := helmRegistryTransport
	helmRegistryTransport = server.Client().Transport
	t.Cleanup(func() { helmRegistryTransport = old; server.Close() })
	return server.URL
}
func TestHelmOutdatedAndFormatting(t *testing.T) {
	repo := helmTestIndex(t)
	root := helmTestProject(t, repo)
	exe := filepath.Join(t.TempDir(), "helm")
	writeExecutable(t, exe, "#!/bin/sh\nexit 0\n")
	ps := Packages{}
	err := (Helm{}).listOutdatedScope(context.Background(), exe, Scope{Kind: "project", Path: root}, ManagerOptions{}, ps)
	if err != nil {
		t.Fatal(err)
	}
	records := Records(ps)
	if len(records) != 2 {
		t.Fatalf("records %+v", records)
	}
	for _, p := range records {
		if p.Name == "Chart.yaml#child" && p.Latest != "1.0.1" {
			t.Fatalf("target %+v", p)
		}
	}
	c, err := helmRead(Scope{Kind: "project", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	edited, err := helmEditVersions(c, map[string]string{"child": "2.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(string(c.data), "'~1.0.0'", "'2.0.0'", 1)
	if string(edited) != want {
		t.Fatalf("source formatting changed: %s", edited)
	}
}
func TestHelmApplyPinsUnselectedAndVerifies(t *testing.T) {
	repo := helmTestIndex(t)
	root := helmTestProject(t, repo)
	exe := filepath.Join(t.TempDir(), "helm")
	writeExecutable(t, exe, `#!/bin/sh
[ "$1 $2 $3 $4" = 'dependency update . --skip-refresh' ] || exit 3
shift 4
while [ "$#" -gt 0 ]; do
 if [ "$1" = --repository-cache ]; then cache=$2; fi
 shift
done
case "$(/bin/cat "$cache/conficius-0-index.yaml")" in *1.9.0*|*2.0.0*) exit 4;; esac
printf 'dependencies:\n- name: child\n  version: 1.0.1\n  repository: %s\n- name: other\n  version: 1.0.0\n  repository: %s\n' "$TEST_HELM_REPO" "$TEST_HELM_REPO" > Chart.lock
/bin/mkdir -p charts
printf child > charts/child-1.0.1.tgz
printf other > charts/other-1.0.0.tgz
`)
	t.Setenv("TEST_HELM_REPO", repo)
	plan := UpdatePlan{Manager: PM_HELM, Executable: exe, Scope: "project", Directory: root, Packages: []Package{{Manager: PM_HELM, Name: "Chart.yaml#child", Manifest: filepath.Join(root, "Chart.yaml"), Current: "1.0.0", Latest: "1.0.1", Requested: "~1.0.0"}}}
	plan.Args = (Helm{}).updateArgs(plan.Packages)
	if err := os.MkdirAll(filepath.Join(root, "charts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "charts", "child-1.0.0.tgz"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "Chart.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err = helmApply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if err = helmVerify(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "charts", "child-1.0.0.tgz")); !os.IsNotExist(err) {
		t.Fatalf("superseded archive retained: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "Chart.yaml"))
	if string(before) != string(after) {
		t.Fatal("Update changed declared range")
	}
}
func TestHelmRejectsUnsafeRepositoriesAndPlans(t *testing.T) {
	for _, repo := range []string{"http://example.com", "oci://example.com/charts", "@stable", "file://../charts", "https://user:pass@example.com", "https://example.com?token=x"} {
		if _, err := helmHTTPS(repo); err == nil {
			t.Fatalf("accepted %s", repo)
		}
	}
	root := helmTestProject(t, "https://example.com")
	plan := UpdatePlan{Manager: PM_HELM, Scope: "project", Directory: root, Packages: []Package{{Manager: PM_HELM, Name: "Chart.yaml#child", Manifest: filepath.Join(root, "Chart.yaml"), Current: "1.0.0", Latest: "2.0.0", Requested: "~1.0.0"}}}
	if _, err := helmPrepare(plan, false); err == nil {
		t.Fatal("Update crossed declared range")
	}
	plan.Bump = true
	if _, err := helmPrepare(plan, false); err != nil {
		t.Fatal(err)
	}
}

func TestHelmRejectsRedirectAndCancelledRequest(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/index.yaml", http.StatusFound)
	}))
	defer server.Close()
	old := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	defer func() { http.DefaultTransport = old }()
	if _, err := helmFetchIndex(context.Background(), server.URL); err == nil {
		t.Fatal("accepted HTTPS downgrade")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := helmFetchIndex(ctx, server.URL); err == nil {
		t.Fatal("ignored cancellation")
	}
}
func TestHelmNativeFailurePreservesOriginalFiles(t *testing.T) {
	repo := helmTestIndex(t)
	root := helmTestProject(t, repo)
	exe := filepath.Join(t.TempDir(), "helm")
	writeExecutable(t, exe, "#!/bin/sh\nprintf broken > Chart.lock\nexit 7\n")
	plan := UpdatePlan{Manager: PM_HELM, Executable: exe, Scope: "project", Directory: root, Bump: true, Packages: []Package{{Manager: PM_HELM, Name: "Chart.yaml#child", Manifest: filepath.Join(root, "Chart.yaml"), Current: "1.0.0", Latest: "2.0.0", Requested: "~1.0.0"}}}
	plan.Args = (Helm{}).updateArgs(plan.Packages)
	before := snapshotProjectFiles(plan)
	err := helmApply(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "original files unchanged") {
		t.Fatalf("error %v", err)
	}
	for name, expected := range before {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(data) != string(expected) {
			t.Fatalf("native failure changed %s: %v", name, err)
		}
	}
}
