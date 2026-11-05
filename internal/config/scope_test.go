package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolveScopesDetectsCurrentProjectAndWorkspaces(t *testing.T) {
	root := t.TempDir()
	withDirectory(t, root)
	mustWrite(t, filepath.Join(root, "package.json"), `{"workspaces":["packages/*"]}`)
	workspace := filepath.Join(root, "packages", "web")
	mustMkdirAll(t, workspace)
	mustWrite(t, filepath.Join(workspace, "package.json"), `{}`)

	scopes, err := ResolveScopes(&Project{Root: root}, ResolvedProfile{Profile: defaultProfile()}, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 2 || scopes[1].Kind != "project" || scopes[1].Path != resolvedRoot {
		t.Fatalf("scopes = %#v", scopes)
	}
	resolvedWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scopes[1].Workspaces, []string{resolvedWorkspace}) {
		t.Fatalf("workspaces = %#v", scopes[1].Workspaces)
	}
}

func TestResolveScopesDetectsDockerOnlyProject(t *testing.T) {
	for _, name := range []string{"Dockerfile", "Containerfile", "compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			withDirectory(t, root)
			mustWrite(t, filepath.Join(root, name), "")
			scopes, err := ResolveScopes(nil, ResolvedProfile{Profile: defaultProfile()}, false, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			resolvedRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(scopes) != 2 || scopes[0].Kind != "global" || scopes[1].Kind != "project" || scopes[1].Path != resolvedRoot || len(scopes[1].Workspaces) != 0 {
				t.Fatalf("scopes = %#v", scopes)
			}
		})
	}
}

func TestResolveScopesDetectsKubernetesProject(t *testing.T) {
	for _, test := range []struct {
		name, path, data string
		wantProject      bool
	}{
		{name: "helm", path: "Chart.yaml", wantProject: true},
		{name: "kustomize yaml", path: "kustomization.yaml", wantProject: true},
		{name: "kustomize yml", path: "kustomization.yml", wantProject: true},
		{name: "kustomize extensionless", path: "Kustomization", wantProject: true},
		{name: "pod", path: "pod.yaml", data: "apiVersion: v1\nkind: Pod\n", wantProject: true},
		{name: "deployment", path: "app.yml", data: "apiVersion: apps/v1\nkind: Deployment\n", wantProject: true},
		{name: "statefulset", path: "app.yaml", data: "apiVersion: apps/v1\nkind: StatefulSet\n", wantProject: true},
		{name: "daemonset", path: "app.yaml", data: "apiVersion: apps/v1\nkind: DaemonSet\n", wantProject: true},
		{name: "replicaset", path: "app.yaml", data: "apiVersion: apps/v1\nkind: ReplicaSet\n", wantProject: true},
		{name: "replicationcontroller", path: "app.yaml", data: "apiVersion: v1\nkind: ReplicationController\n", wantProject: true},
		{name: "job", path: "app.yaml", data: "apiVersion: batch/v1\nkind: Job\n", wantProject: true},
		{name: "cronjob", path: "app.yaml", data: "apiVersion: batch/v1\nkind: CronJob\n", wantProject: true},
		{name: "list", path: "app.yaml", data: "apiVersion: v1\nkind: List\nitems: []\n", wantProject: true},
		{name: "kustomize document", path: "custom.yaml", data: "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\n", wantProject: true},
		{name: "multiple documents", path: "app.yaml", data: "apiVersion: v1\nkind: Service\n---\napiVersion: v1\nkind: Pod\n", wantProject: true},
		{name: "k8s", path: "k8s/app.yaml", data: "apiVersion: v1\nkind: Pod\n", wantProject: true},
		{name: "kubernetes", path: "kubernetes/app.yaml", data: "apiVersion: v1\nkind: Pod\n", wantProject: true},
		{name: "manifests", path: "manifests/app.yaml", data: "apiVersion: v1\nkind: Pod\n", wantProject: true},
		{name: "deploy", path: "deploy/kustomization.yaml", wantProject: true},
		{name: "arbitrary yaml", path: "settings.yaml", data: "name: app\n"},
		{name: "CI yaml", path: ".gitlab-ci.yml", data: "image: alpine\nbuild:\n  script: echo hello\n"},
		{name: "wrong API", path: "app.yaml", data: "apiVersion: example.com/v1\nkind: Deployment\n"},
		{name: "missing API", path: "app.yaml", data: "kind: Pod\n"},
		{name: "missing kind", path: "app.yaml", data: "apiVersion: v1\n"},
		{name: "non workload", path: "app.yaml", data: "apiVersion: v1\nkind: ConfigMap\n"},
		{name: "malformed", path: "app.yaml", data: "apiVersion: [\nkind: Pod\n"},
		{name: "template", path: "templates/app.yaml", data: "apiVersion: v1\nkind: Pod\n"},
		{name: "nested", path: "k8s/base/app.yaml", data: "apiVersion: v1\nkind: Pod\n"},
		{name: "unrelated directory", path: "examples/app.yaml", data: "apiVersion: v1\nkind: Pod\n"},
		{name: "oversized", path: "app.yaml", data: "apiVersion: v1\nkind: Pod\n#" + strings.Repeat("x", 1<<20)},
		{name: "too many documents", path: "app.yaml", data: strings.Repeat("---\n{}\n", 32) + "---\napiVersion: v1\nkind: Pod\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			withDirectory(t, root)
			path := filepath.Join(root, test.path)
			mustMkdirAll(t, filepath.Dir(path))
			mustWrite(t, path, test.data)
			scopes, err := ResolveScopes(nil, ResolvedProfile{Profile: defaultProfile()}, false, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(scopes) == 2 && scopes[1].Kind == "project"; got != test.wantProject {
				t.Fatalf("scopes = %#v, want project = %t", scopes, test.wantProject)
			}
		})
	}
}

func TestResolveScopesDetectsCurrentRustProject(t *testing.T) {
	root := t.TempDir()
	withDirectory(t, root)
	mustWrite(t, filepath.Join(root, "Cargo.toml"), "[package]\nname = \"app\"\nversion = \"0.1.0\"\n")

	scopes, err := ResolveScopes(&Project{Root: root}, ResolvedProfile{Profile: defaultProfile()}, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 2 || scopes[0].Kind != "global" || scopes[1].Kind != "project" || scopes[1].Path != resolvedRoot {
		t.Fatalf("scopes = %#v", scopes)
	}
}

func TestResolveScopesDetectsCurrentGoProject(t *testing.T) {
	for _, withMise := range []bool{false, true} {
		t.Run(fmt.Sprintf("mise=%t", withMise), func(t *testing.T) {
			root := t.TempDir()
			withDirectory(t, root)
			mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.26\n")
			if withMise {
				mustWrite(t, filepath.Join(root, "mise.toml"), "[tools]\ngo = '1.26'\n")
			}
			scopes, err := ResolveScopes(nil, ResolvedProfile{Profile: defaultProfile()}, false, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			resolvedRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(scopes) != 2 || scopes[0].Kind != "global" || scopes[1].Kind != "project" || scopes[1].Path != resolvedRoot || len(scopes[1].Workspaces) != 0 {
				t.Fatalf("scopes = %#v", scopes)
			}
		})
	}
}

func TestResolveScopesSelectsNamedProfileProject(t *testing.T) {
	root := t.TempDir()
	profile := ResolvedProfile{Name: "work", Profile: Profile{Projects: []ProfileProject{{Name: "api", Path: root}}}}
	scopes, err := ResolveScopes(nil, profile, false, nil, []string{"api"})
	if err != nil {
		t.Fatal(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 1 || scopes[0].Name != "api" || scopes[0].Path != resolvedRoot {
		t.Fatalf("scopes = %#v", scopes)
	}
}

func TestResolveScopesRejectsDuplicatePaths(t *testing.T) {
	root := t.TempDir()
	if _, err := ResolveScopes(nil, ResolvedProfile{Profile: defaultProfile()}, false, []string{root, root}, nil); err == nil {
		t.Fatal("ResolveScopes succeeded; want duplicate error")
	}
}

func TestResolveScopesCanonicalizesRoot(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}

	scopes, err := ResolveScopes(nil, ResolvedProfile{Profile: defaultProfile()}, false, []string{link}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if scopes[0].Path != want {
		t.Fatalf("project path = %q; want %q", scopes[0].Path, want)
	}
}

func TestNPMWorkspacesRejectsMalformedGlob(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "package.json"), `{"workspaces":["["]}`)

	if _, err := npmWorkspaces(root); err == nil {
		t.Fatal("npmWorkspaces succeeded; want malformed glob error")
	}
}

func TestNPMWorkspacesAllowsZeroMatches(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "package.json"), `{"workspaces":["missing/*"]}`)

	workspaces, err := npmWorkspaces(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 0 {
		t.Fatalf("workspaces = %#v; want none", workspaces)
	}
}

func TestNPMWorkspacesRejectsBrokenSymlink(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "packages"))
	mustWrite(t, filepath.Join(root, "package.json"), `{"workspaces":["packages/*"]}`)
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "packages", "broken")); err != nil {
		t.Fatal(err)
	}

	if _, err := npmWorkspaces(root); err == nil {
		t.Fatal("npmWorkspaces succeeded; want broken symlink error")
	}
}

func TestNPMWorkspacesRejectsCanonicalMatchOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "packages"))
	mustWrite(t, filepath.Join(root, "package.json"), `{"workspaces":["packages/*"]}`)
	mustWrite(t, filepath.Join(outside, "package.json"), `{}`)
	if err := os.Symlink(outside, filepath.Join(root, "packages", "outside")); err != nil {
		t.Fatal(err)
	}

	if _, err := npmWorkspaces(root); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("npmWorkspaces error = %v; want outside-root error", err)
	}
}

func TestNPMWorkspacesSkipsNonDirectories(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "packages"))
	mustWrite(t, filepath.Join(root, "package.json"), `{"workspaces":["packages/*"]}`)
	mustWrite(t, filepath.Join(root, "packages", "README.md"), "not a workspace")

	workspaces, err := npmWorkspaces(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 0 {
		t.Fatalf("workspaces = %#v; want none", workspaces)
	}
}

func TestNPMWorkspacesRejectsMatchInspectionError(t *testing.T) {
	root := t.TempDir()
	packages := filepath.Join(root, "packages")
	mustMkdirAll(t, packages)
	mustWrite(t, filepath.Join(root, "package.json"), `{"workspaces":["packages/*"]}`)
	if err := os.Symlink("loop", filepath.Join(packages, "loop")); err != nil {
		t.Fatal(err)
	}

	if _, err := npmWorkspaces(root); err == nil {
		t.Fatal("npmWorkspaces succeeded; want inspection error")
	}
}

func TestNPMWorkspacesSkipsMissingManifestAndRejectsOtherStatErrors(t *testing.T) {
	root := t.TempDir()
	packages := filepath.Join(root, "packages")
	missing := filepath.Join(packages, "missing")
	broken := filepath.Join(packages, "broken")
	mustMkdirAll(t, missing)
	mustMkdirAll(t, broken)
	mustWrite(t, filepath.Join(root, "package.json"), `{"workspaces":["packages/*"]}`)
	if err := os.Symlink("package.json", filepath.Join(broken, "package.json")); err != nil {
		t.Fatal(err)
	}

	if _, err := npmWorkspaces(root); err == nil {
		t.Fatal("npmWorkspaces succeeded; want manifest inspection error")
	}
}

func TestNPMWorkspacesRejectsRootManifestReadError(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "package.json"))

	if _, err := npmWorkspaces(root); err == nil {
		t.Fatal("npmWorkspaces succeeded; want manifest read error")
	}
}

func TestNPMWorkspacesDeduplicatesCanonicalPathsAndSorts(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "packages", "a")
	second := filepath.Join(root, "packages", "b")
	mustMkdirAll(t, first)
	mustMkdirAll(t, second)
	mustMkdirAll(t, filepath.Join(root, "aliases"))
	mustWrite(t, filepath.Join(root, "package.json"), `{"workspaces":["packages/*","aliases/*"]}`)
	mustWrite(t, filepath.Join(first, "package.json"), `{}`)
	mustWrite(t, filepath.Join(second, "package.json"), `{}`)
	if err := os.Symlink(second, filepath.Join(root, "aliases", "second")); err != nil {
		t.Fatal(err)
	}

	workspaces, err := npmWorkspaces(root)
	if err != nil {
		t.Fatal(err)
	}
	canonicalFirst, err := filepath.EvalSymlinks(first)
	if err != nil {
		t.Fatal(err)
	}
	canonicalSecond, err := filepath.EvalSymlinks(second)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{canonicalFirst, canonicalSecond}; !reflect.DeepEqual(workspaces, want) {
		t.Fatalf("workspaces = %#v; want %#v", workspaces, want)
	}
}

func TestResolveScopesCollectsConfiguredProjectErrors(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	mustWrite(t, filepath.Join(first, "package.json"), `{"workspaces":["["]}`)
	mustWrite(t, filepath.Join(second, "package.json"), `{"workspaces":["["]}`)
	profile := ResolvedProfile{Profile: Profile{Projects: []ProfileProject{
		{Name: "first", Path: first},
		{Name: "second", Path: second},
	}}}

	_, err := ResolveScopes(nil, profile, false, nil, nil)
	if err == nil || !strings.Contains(err.Error(), `project "first"`) || !strings.Contains(err.Error(), `project "second"`) {
		t.Fatalf("ResolveScopes error = %v; want errors for both projects", err)
	}
}

func withDirectory(t *testing.T, path string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
}
