package packagemanager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestYarnAdapterQueriesProjectInventoryAndReportsUnsupportedOperations(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	writeExecutable(t, filepath.Join(dir, PM_YARN), `#!/bin/sh
printf '%s|%s\n' "$PWD" "$*" >>"$ARGS_FILE"
if [ "$1" = "--version" ]; then printf '4.18.1'; else printf '{"value":"lodash@npm:4.17.21","children":{"Version":"4.17.21"}}\n'; fi
`)
	t.Setenv("PATH", dir)
	t.Setenv("ARGS_FILE", argsFile)
	scope := Scope{Kind: "project", Path: dir}
	installed, err := QueryInstalledScopes(context.Background(), []string{PM_YARN}, []Scope{scope})
	if err != nil {
		t.Fatal(err)
	}
	outdated, err := QueryOutdatedScopes(context.Background(), []string{PM_YARN}, []Scope{scope})
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(installed.Packages) != 1 || len(outdated.Packages) != 0 || !strings.Contains(string(args), dir+"|info --json") || outdated.Managers[0].Operations[0].Status != StatusUnsupported {
		t.Fatalf("installed = %#v, outdated = %#v, args = %q", installed, outdated, args)
	}
}

func TestParseYarnInstalledReadsNDJSONInventory(t *testing.T) {
	output := []byte("{\"value\":\"lodash@npm:4.17.21\",\"children\":{\"Version\":\"4.17.21\"}}\n{\"value\":\"typescript@npm:5.7.0\",\"children\":{\"Version\":\"5.7.0\"}}\n")
	packages, err := parseYarnInstalled(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 2 || packages[0].Manager != PM_YARN || packages[0].Name != "lodash" || packages[0].Version != "4.17.21" || packages[0].Role != RoleLibrary || packages[1].Name != "typescript" {
		t.Fatalf("packages = %#v", packages)
	}
}

func TestParseYarnInstalledRejectsChangedOutput(t *testing.T) {
	if _, err := parseYarnInstalled([]byte(`{"value":"lodash","children":{}}`)); err == nil {
		t.Fatal("parse succeeded")
	}
}
