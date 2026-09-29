package packagemanager

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPoetryUpdateArgsUseSelectedPackageNames(t *testing.T) {
	adapter := Poetry{}
	packages := []Package{{Name: "requests"}, {Name: "urllib3"}}
	want := []string{"update", "requests", "urllib3"}
	if got := adapter.updateArgs(packages); !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v; want %#v", got, want)
	}
}

func TestPoetryAdapterQueriesProjectInventoryAndOutdated(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	writeExecutable(t, filepath.Join(dir, PM_POETRY), `#!/bin/sh
printf '%s|%s\n' "$PWD" "$*" >>"$ARGS_FILE"
if [ "$1" = "--version" ]; then printf 'Poetry (version 2.5.1)'; elif [ "$2" = "--outdated" ]; then printf '[{"name":"requests","installed_status":"installed","version":"2.31.0","latest_version":"2.32.0"}]'; else printf '[{"name":"requests","installed_status":"installed","version":"2.31.0"}]'; fi
`)
	t.Setenv("PATH", dir)
	t.Setenv("ARGS_FILE", argsFile)
	scope := Scope{Kind: "project", Path: dir}
	installed, err := QueryInstalledScopes(context.Background(), []string{PM_POETRY}, []Scope{scope})
	if err != nil {
		t.Fatal(err)
	}
	outdated, err := QueryOutdatedScopes(context.Background(), []string{PM_POETRY}, []Scope{scope})
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(installed.Packages) != 1 || len(Records(outdated.Packages)) != 1 || !strings.Contains(string(args), dir+"|show --format json") || !strings.Contains(string(args), dir+"|show --outdated --format json") {
		t.Fatalf("installed = %#v, outdated = %#v, args = %q", installed.Packages, Records(outdated.Packages), args)
	}
}

func TestParsePoetryPackagesReadsInventoryAndOutdated(t *testing.T) {
	output := []byte(`[{"name":"requests","installed_status":"installed","version":"2.31.0","latest_version":"2.32.0","description":"HTTP"},{"name":"urllib3","installed_status":"installed","version":"2.2.0","latest_version":"2.2.0","description":"HTTP"}]`)
	installed, outdated, err := parsePoetryPackages(output, "/usr/local/bin/poetry")
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 2 || installed[0].Manager != PM_POETRY || installed[0].Name != "requests" || installed[0].Role != RoleLibrary {
		t.Fatalf("installed = %#v", installed)
	}
	records := Records(outdated)
	if len(records) != 1 || records[0].Name != "requests" || records[0].Current != "2.31.0" || records[0].Latest != "2.32.0" {
		t.Fatalf("outdated = %#v", records)
	}
}

func TestParsePoetryPackagesRejectsChangedOutput(t *testing.T) {
	if _, _, err := parsePoetryPackages([]byte(`{"packages":[]}`), "poetry"); err == nil {
		t.Fatal("parse succeeded")
	}
}
