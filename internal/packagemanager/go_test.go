package packagemanager

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGoAdapterReadsJournaledGlobalInventory(t *testing.T) {
	dir := t.TempDir()
	state := t.TempDir()
	journalPath := filepath.Join(state, "conficius", "go-tools.json")
	if err := WriteGoToolJournal(journalPath, NewGoToolJournal([]GoToolImport{{Module: "golang.org/x/tools/gopls", Version: "v0.20.0", Commands: []string{"gopls"}}})); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(dir, PM_GO), "#!/bin/sh\nprintf 'go version go1.27.1'\n")
	t.Setenv("PATH", dir)
	t.Setenv("XDG_STATE_HOME", state)

	result, err := QueryInstalledScopes(context.Background(), []string{PM_GO}, []Scope{{Kind: "global"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Packages) != 1 || result.Packages[0].Name != "golang.org/x/tools/gopls" || result.Packages[0].Version != "v0.20.0" || result.Packages[0].Role != RoleDevelopmentTool {
		t.Fatalf("packages = %#v", result.Packages)
	}
	if len(result.Commands) != 1 || result.Commands[0] != (ExposedCommand{Package: PackageIdentity{Manager: PM_GO, Name: "golang.org/x/tools/gopls"}, Name: "gopls", Scope: "global"}) {
		t.Fatalf("commands = %#v", result.Commands)
	}
}

func TestGoAdapterTreatsMissingJournalAsEmptyInventory(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_GO), "#!/bin/sh\nprintf 'go version go1.27.1'\n")
	t.Setenv("PATH", dir)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	result, err := QueryInstalledScopes(context.Background(), []string{PM_GO}, []Scope{{Kind: "global"}})
	if err != nil || len(result.Packages) != 0 {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestGoAdapterReportsInvalidJournal(t *testing.T) {
	dir := t.TempDir()
	state := t.TempDir()
	path := filepath.Join(state, "conficius", "go-tools.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":2,"imports":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(dir, PM_GO), "#!/bin/sh\nprintf 'go version go1.27.1'\n")
	t.Setenv("PATH", dir)
	t.Setenv("XDG_STATE_HOME", state)

	result, err := QueryInstalledScopes(context.Background(), []string{PM_GO}, []Scope{{Kind: "global"}})
	if err == nil || len(result.Managers) != 1 || result.Managers[0].Operations[0].Category != ErrorParser {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}
