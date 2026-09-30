package config

import (
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
