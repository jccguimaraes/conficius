package packagemanager

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMASAdapterQueriesGlobalInventoryAndOutdated(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	executable := filepath.Join(dir, PM_MAS)
	writeExecutable(t, executable, `#!/bin/sh
printf '%s\n' "$*" >>"$ARGS_FILE"
if [ "$1" = "--version" ]; then printf '7.0.0'; elif [ "$1" = "list" ]; then printf '{"adamID":497799835,"name":"Xcode","version":"26.0"}'; else printf '{"adamID":497799835,"version":"26.0","newVersion":"27.0"}'; fi
`)
	t.Setenv("PATH", dir)
	t.Setenv("ARGS_FILE", argsFile)
	installed, err := QueryInstalledScopes(context.Background(), []string{PM_MAS}, []Scope{{Kind: "global"}})
	if err != nil {
		t.Fatal(err)
	}
	outdated, err := QueryOutdatedScopes(context.Background(), []string{PM_MAS}, []Scope{{Kind: "global"}})
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(installed.Packages) != 1 || len(Records(outdated.Packages)) != 1 || !strings.Contains(string(args), "list --json") || !strings.Contains(string(args), "outdated --json --inaccurate --no-check-min-os") {
		t.Fatalf("installed = %#v, outdated = %#v, args = %q", installed.Packages, Records(outdated.Packages), args)
	}
}

func TestParseMASInstalledSupportsJSONArray(t *testing.T) {
	output := []byte(`[{"adamID":497799835,"name":"Xcode","version":"27.0"},{"adamID":0,"name":"invalid","version":"1"}]`)
	packages, err := parseMASInstalled(output)
	if err != nil {
		t.Fatal(err)
	}
	want := []InstalledPackage{{Manager: PM_MAS, Name: "497799835", Version: "27.0", Role: RoleApplication}}
	if !reflect.DeepEqual(packages, want) {
		t.Fatalf("packages = %#v; want %#v", packages, want)
	}
}

func TestParseMASInstalledSupportsOneJSONObject(t *testing.T) {
	output := []byte(`{"adamID":497799835,"name":"Xcode","version":"27.0"}`)
	packages, err := parseMASInstalled(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || packages[0].Name != "497799835" {
		t.Fatalf("packages = %#v", packages)
	}
}

func TestParseMASOutdatedSupportsJSONLines(t *testing.T) {
	output := []byte("{\"adamID\":497799835,\"version\":\"26.0\",\"newVersion\":\"27.0\"}\n{\"adamID\":123,\"version\":\"1.0\",\"newVersion\":\"1.1\"}\n")
	packages, err := parseMASOutdated(output, "/opt/homebrew/bin/mas")
	if err != nil {
		t.Fatal(err)
	}
	records := Records(packages)
	if len(records) != 2 || records[0].Manager != PM_MAS || records[0].Name != "123" || records[0].Current != "1.0" || records[0].Latest != "1.1" {
		t.Fatalf("records = %#v", records)
	}
}

func TestParseMASOutdatedRejectsChangedOutput(t *testing.T) {
	if _, err := parseMASOutdated([]byte(`not json`), "mas"); err == nil {
		t.Fatal("parse succeeded")
	}
}

func TestParseMASInstalledRejectsChangedOutput(t *testing.T) {
	if _, err := parseMASInstalled([]byte(`not json`)); err == nil {
		t.Fatal("parse succeeded")
	}
}
