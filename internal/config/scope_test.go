package config

import (
	"os"
	"path/filepath"
	"reflect"
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
