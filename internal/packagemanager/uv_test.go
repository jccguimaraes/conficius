package packagemanager

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUVAdapterQueriesGlobalInventoryAndOutdated(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	writeExecutable(t, filepath.Join(dir, PM_UV), `#!/bin/sh
printf '%s\n' "$*" >>"$ARGS_FILE"
if [ "$1" = "--version" ]; then printf 'uv 0.12.19'; elif [ "$3" = "--outdated" ]; then printf 'black v24.2.0 [latest: 25.1.0]\n- black\n'; else printf 'black v24.2.0\n- black\n'; fi
`)
	t.Setenv("PATH", dir)
	t.Setenv("ARGS_FILE", argsFile)
	installed, err := QueryInstalledScopes(context.Background(), []string{PM_UV}, []Scope{{Kind: "global"}})
	if err != nil {
		t.Fatal(err)
	}
	outdated, err := QueryOutdatedScopes(context.Background(), []string{PM_UV}, []Scope{{Kind: "global"}})
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(installed.Packages) != 1 || len(Records(outdated.Packages)) != 1 || !strings.Contains(string(args), "tool list\n") || !strings.Contains(string(args), "tool list --outdated") {
		t.Fatalf("installed = %#v, outdated = %#v, args = %q", installed.Packages, Records(outdated.Packages), args)
	}
}

func TestParseUVToolsReadsInventoryAndOutdatedVersions(t *testing.T) {
	output := []byte("black v24.2.0 [latest: 25.1.0]\n- black\n- blackd\nruff v0.9.0\n- ruff\n")
	installed, outdated, err := parseUVTools(output, "/usr/local/bin/uv")
	if err != nil {
		t.Fatal(err)
	}
	wantInstalled := []InstalledPackage{
		{Manager: PM_UV, Name: "black", Version: "24.2.0", Role: RoleApplication},
		{Manager: PM_UV, Name: "ruff", Version: "0.9.0", Role: RoleApplication},
	}
	if !reflect.DeepEqual(installed, wantInstalled) {
		t.Fatalf("installed = %#v; want %#v", installed, wantInstalled)
	}
	records := Records(outdated)
	if len(records) != 1 || records[0].Name != "black" || records[0].Current != "24.2.0" || records[0].Latest != "25.1.0" {
		t.Fatalf("outdated = %#v", records)
	}
}

func TestParseUVToolsRejectsChangedOutput(t *testing.T) {
	if _, _, err := parseUVTools([]byte("black 24.2.0 -> 25.1.0"), "uv"); err == nil {
		t.Fatal("parse succeeded")
	}
}
