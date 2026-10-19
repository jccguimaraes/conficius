package packagemanager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCargoAdapterQueriesGlobalInventoryAndEmptyUpdates(t *testing.T) {
	cargoGlobalTestHome(t)
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	writeExecutable(t, filepath.Join(dir, PM_CARGO), `#!/bin/sh
printf '%s\n' "$*" >>"$ARGS_FILE"
if [ "$1" = "--version" ]; then printf 'cargo 1.98.1'; else printf 'cargo-edit v0.13.3:\n    cargo-add\n'; fi
`)
	t.Setenv("PATH", dir)
	t.Setenv("ARGS_FILE", argsFile)
	scope := Scope{Kind: "global"}
	installed, err := QueryInstalledScopes(context.Background(), []string{PM_CARGO}, []Scope{scope})
	if err != nil {
		t.Fatal(err)
	}
	outdated, err := QueryOutdatedScopes(context.Background(), []string{PM_CARGO}, []Scope{scope})
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(installed.Packages) != 1 || len(outdated.Packages) != 0 || !strings.Contains(string(args), "install --list") || outdated.Managers[0].Operations[0].Status != StatusAvailable {
		t.Fatalf("installed = %#v, outdated = %#v, args = %q", installed, outdated, args)
	}
}

func TestCargoAdapterReadsProjectLockfileInventory(t *testing.T) {
	project := t.TempDir()
	lockfile := `version = 4

[[package]]
name = "app"
version = "0.1.0"
dependencies = ["serde"]

[[package]]
name = "serde"
version = "1.0.219"
source = "registry+https://github.com/rust-lang/crates.io-index"
`
	if err := os.WriteFile(filepath.Join(project, "Cargo.lock"), []byte(lockfile), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_CARGO), "#!/bin/sh\nprintf 'cargo 1.98.1'\n")
	t.Setenv("PATH", dir)

	result, err := QueryInstalledScopes(context.Background(), []string{PM_CARGO}, []Scope{{Kind: "project", Path: project}})
	if err != nil {
		t.Fatal(err)
	}
	want := []InstalledPackage{{Manager: PM_CARGO, Name: "serde", Version: "1.0.219", Role: RoleLibrary, Scope: "project", Path: project, Manifest: filepath.Join(project, "Cargo.lock"), OSVEcosystem: "crates.io", OSVName: "serde"}}
	if len(result.Packages) != 1 || result.Packages[0] != want[0] {
		t.Fatalf("packages = %#v; want %#v", result.Packages, want)
	}
}

func TestCargoInventoryClassifiesCommandFailure(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_CARGO), `#!/bin/sh
if [ "$1" = "--version" ]; then printf 'cargo 1.98.1'; else printf 'inventory failed' >&2; exit 2; fi
`)
	t.Setenv("PATH", dir)
	result, err := QueryInstalledScopes(context.Background(), []string{PM_CARGO}, []Scope{{Kind: "global"}})
	if err == nil || len(result.Managers) != 1 || result.Managers[0].Operations[0].Category != ErrorCommand {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestCargoInventoryClassifiesParserFailure(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_CARGO), `#!/bin/sh
if [ "$1" = "--version" ]; then printf 'cargo 1.98.1'; else printf 'changed output'; fi
`)
	t.Setenv("PATH", dir)
	result, err := QueryInstalledScopes(context.Background(), []string{PM_CARGO}, []Scope{{Kind: "global"}})
	var operationErr *OperationError
	if !errors.As(err, &operationErr) || operationErr.Category != ErrorParser || result.Managers[0].Operations[0].Category != ErrorParser {
		t.Fatalf("result = %#v, error = %#v", result, err)
	}
}

func TestParseCargoInstalledReadsInventory(t *testing.T) {
	output := []byte("cargo-edit v0.13.3:\n    cargo-add\n    cargo-rm\ndioxus-cli v0.7.10:\n    dx\n")
	packages, err := parseCargoInstalled(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 2 || packages[0].Manager != PM_CARGO || packages[0].Name != "cargo-edit" || packages[0].Version != "0.13.3" || packages[0].Role != RoleApplication || packages[1].Name != "dioxus-cli" {
		t.Fatalf("packages = %#v", packages)
	}
}

func TestParseCargoInstalledRejectsChangedOutput(t *testing.T) {
	if _, err := parseCargoInstalled([]byte("cargo-edit 0.13.3\n")); err == nil {
		t.Fatal("parse succeeded")
	}
}
