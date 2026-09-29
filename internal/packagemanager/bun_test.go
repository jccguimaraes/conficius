package packagemanager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBunAdapterQueriesProjectInventoryAndReportsUnsupportedOperations(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	writeExecutable(t, filepath.Join(dir, PM_BUN), `#!/bin/sh
printf '%s|%s\n' "$PWD" "$*" >>"$ARGS_FILE"
if [ "$1" = "--version" ]; then printf '1.4.2'; else printf '%s node_modules (1 installed)\n└── react@19.0.0\n' "$PWD"; fi
`)
	t.Setenv("PATH", dir)
	t.Setenv("ARGS_FILE", argsFile)
	scope := Scope{Kind: "project", Path: dir}
	installed, err := QueryInstalledScopes(context.Background(), []string{PM_BUN}, []Scope{scope})
	if err != nil {
		t.Fatal(err)
	}
	outdated, err := QueryOutdatedScopes(context.Background(), []string{PM_BUN}, []Scope{scope})
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(installed.Packages) != 1 || len(outdated.Packages) != 0 || !strings.Contains(string(args), dir+"|pm ls") || outdated.Managers[0].Operations[0].Status != StatusUnsupported {
		t.Fatalf("installed = %#v, outdated = %#v, args = %q", installed, outdated, args)
	}
}

func TestParseBunInstalledReadsInventory(t *testing.T) {
	output := []byte("/project node_modules (3 installed)\n├── @types/bun@1.3.0\n├── react@19.0.0\n└── typescript@5.7.0\n")
	packages, err := parseBunInstalled(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 3 || packages[0].Manager != PM_BUN || packages[0].Name != "@types/bun" || packages[0].Version != "1.3.0" || packages[0].Role != RoleLibrary || packages[2].Name != "typescript" {
		t.Fatalf("packages = %#v", packages)
	}
}

func TestParseBunInstalledRejectsChangedOutput(t *testing.T) {
	if _, err := parseBunInstalled([]byte("react 19.0.0\n")); err == nil {
		t.Fatal("parse succeeded")
	}
}
