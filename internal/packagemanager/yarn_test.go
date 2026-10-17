package packagemanager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestYarnAdapterQueriesProjectInventoryAndOutdated(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	writeExecutable(t, filepath.Join(dir, PM_YARN), `#!/bin/sh
printf '%s|%s\n' "$PWD" "$*" >>"$ARGS_FILE"
if [ "$1" = "--version" ]; then printf '4.18.1'; elif [ "$1" = "npm" ]; then printf '{"name":"lodash","dist-tags":{"latest":"5.0.0"}}\n'; else printf '{"value":"lodash@npm:4.17.21","children":{"Version":"4.17.21"}}\n'; fi
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
	records := Records(outdated.Packages)
	if len(installed.Packages) != 1 || len(records) != 1 || !strings.Contains(string(args), dir+"|info --json") || !strings.Contains(string(args), dir+"|npm info lodash --json --fields name,dist-tags") {
		t.Fatalf("installed = %#v, outdated = %#v, args = %q", installed, outdated, args)
	}
	if records[0].Manager != PM_YARN || records[0].Current != "4.17.21" || records[0].Latest != "5.0.0" || records[0].Requested != "5.0.0" || records[0].Executable != filepath.Join(dir, PM_YARN) {
		t.Fatalf("records = %#v", records)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"standalone","packageManager":"yarn@4.18.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "yarn.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	packages := make(Packages)
	if err := (Yarn{}).listOutdatedScope(context.Background(), filepath.Join(dir, PM_YARN), scope, ManagerOptions{}, packages); err != nil || len(packages) != 1 {
		t.Fatalf("standalone packages = %#v, error = %v", packages, err)
	}
	plans, err := PlanUpdates(outdated, nil, nil)
	if err != nil || len(plans) != 1 || !reflect.DeepEqual(plans[0].Args, []string{"up", "lodash"}) {
		t.Fatalf("plans = %#v, error = %v", plans, err)
	}
}

func TestYarnOutdatedRejectsWorkspaceProjectsBeforeQueries(t *testing.T) {
	for _, test := range []struct {
		name, manifest, marker string
		child                  bool
	}{
		{name: "root", manifest: `{"workspaces":["packages/*"]}`},
		{name: "root empty workspaces", manifest: `{"workspaces":[]}`},
		{name: "child", manifest: `{"workspaces":["packages/*"]}`, child: true},
		{name: "child object workspaces", manifest: `{"workspaces":{"packages":["packages/*"]}}`, child: true},
		{name: "ancestor lockfile", manifest: `{}`, marker: "yarn.lock", child: true},
		{name: "ancestor configuration", manifest: `{}`, marker: ".yarnrc.yml", child: true},
		{name: "ancestor package manager", manifest: `{"packageManager":"yarn@4.18.1"}`, child: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(test.manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			if test.marker != "" {
				if err := os.WriteFile(filepath.Join(dir, test.marker), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			scope := Scope{Kind: "project", Path: dir}
			if test.child {
				scope.Path = filepath.Join(dir, "packages", "child")
				if err := os.MkdirAll(scope.Path, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(scope.Path, "package.json"), []byte(`{"name":"child"}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			argsFile := filepath.Join(dir, "args")
			writeExecutable(t, filepath.Join(dir, PM_YARN), `#!/bin/sh
printf '%s\n' "$*" >>"$ARGS_FILE"
if [ "$1" = "--version" ]; then printf '4.18.1'; else printf '{"value":"pkg@npm:1.0.0","children":{"Version":"1.0.0"}}'; fi
`)
			t.Setenv("PATH", dir)
			t.Setenv("ARGS_FILE", argsFile)
			packages := make(Packages)
			executable := filepath.Join(dir, PM_YARN)
			err := (Yarn{}).listOutdatedScope(context.Background(), executable, scope, ManagerOptions{}, packages)
			if err == nil || !strings.Contains(err.Error(), "Yarn outdated/update planning is unsupported for workspace projects") || len(packages) != 0 {
				t.Fatalf("packages = %#v, error = %v", packages, err)
			}
			args, err := os.ReadFile(argsFile)
			if !os.IsNotExist(err) {
				t.Fatalf("unexpected queries: %q, error = %v", args, err)
			}
			installed, err := (Yarn{}).listInstalled(context.Background(), executable, scope)
			if err != nil || len(installed) != 1 {
				t.Fatalf("inventory = %#v, error = %v", installed, err)
			}
		})
	}
}

func TestYarnInventoryClassifiesCommandFailure(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_YARN), `#!/bin/sh
if [ "$1" = "--version" ]; then printf '4.18.1'; else printf 'inventory failed' >&2; exit 2; fi
`)
	t.Setenv("PATH", dir)
	result, err := QueryInstalledScopes(context.Background(), []string{PM_YARN}, []Scope{{Kind: "project", Path: dir}})
	if err == nil || len(result.Managers) != 1 || result.Managers[0].Operations[0].Category != ErrorCommand {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestYarnInventoryClassifiesParserFailure(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_YARN), `#!/bin/sh
if [ "$1" = "--version" ]; then printf '4.18.1'; else printf 'changed output'; fi
`)
	t.Setenv("PATH", dir)
	result, err := QueryInstalledScopes(context.Background(), []string{PM_YARN}, []Scope{{Kind: "project", Path: dir}})
	var operationErr *OperationError
	if !errors.As(err, &operationErr) || operationErr.Category != ErrorParser || result.Managers[0].Operations[0].Category != ErrorParser {
		t.Fatalf("result = %#v, error = %#v", result, err)
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
	for _, output := range []string{
		`{}`, `null`, `changed output`,
		`{"value":"lodash","children":{}}`,
		`{"value":"pkg@npm:1.0.0","children":{}}`,
		`{"value":"pkg@npm:1.0.0","children":{"Version":"latest"}}`,
	} {
		if _, err := parseYarnInstalled([]byte(output)); err == nil {
			t.Fatalf("parse succeeded for %q", output)
		}
	}
}

func TestYarnUpdateArgsUseSelectedNames(t *testing.T) {
	adapter := Yarn{}
	if got := adapter.updateArgs([]Package{{Name: "react"}, {Name: "@types/node"}}); !reflect.DeepEqual(got, []string{"up", "react", "@types/node"}) {
		t.Fatalf("args = %#v", got)
	}
	if got := adapter.updateArgs(nil); got != nil {
		t.Fatalf("empty selection args = %#v", got)
	}
	for _, name := range []string{"*", "--all", "react@latest", "@types/*"} {
		if got := adapter.updateArgs([]Package{{Name: name}}); got != nil {
			t.Fatalf("unsafe name %q: %#v", name, got)
		}
	}
	if !adapter.updateImpact(Scope{}).MayChangeManifests || !hasCapability(adapter, CapabilityProjectUpdate) {
		t.Fatal("missing project mutation contract")
	}
}

func TestParseYarnInstalledSkipsNonRegistryLocators(t *testing.T) {
	output := []byte(`{"value":"@scope/pkg@virtual:hash#npm:1.0.0","children":{"Version":"1.0.0"}}
{"value":"local@workspace:packages/local","children":{"Version":"1.0.0"}}
{"value":"file@file:../file#hash","children":{"Version":"1.0.0"}}
{"value":"link@link:../link","children":{"Version":"0.0.0"}}
{"value":"portal@portal:../portal","children":{"Version":"1.0.0"}}
{"value":"patch@patch:pkg@npm%3A1.0.0#hash","children":{"Version":"1.0.0"}}
{"value":"git@git+https://example.com/pkg.git#commit=hash","children":{"Version":"1.0.0"}}
{"value":"alias@npm:other@1.0.0","children":{"Version":"1.0.0"}}
`)
	packages, err := parseYarnInstalled(output)
	if err != nil || len(packages) != 1 || packages[0].Name != "@scope/pkg" {
		t.Fatalf("packages = %#v, error = %v", packages, err)
	}
}

func TestYarnOutdatedSkipsCurrentNewerAndLocalPackages(t *testing.T) {
	for _, latest := range []string{"1.0.0", "0.9.0", "1.0.0-beta.1"} {
		t.Run(latest, func(t *testing.T) {
			dir := t.TempDir()
			argsFile := filepath.Join(dir, "args")
			writeExecutable(t, filepath.Join(dir, PM_YARN), `#!/bin/sh
printf '%s\n' "$*" >>"$ARGS_FILE"
if [ "$1" = "info" ]; then
printf '%s\n' '{"value":"pkg@npm:1.0.0","children":{"Version":"1.0.0"}}' '{"value":"local@workspace:packages/local","children":{"Version":"1.0.0"}}'
else printf '{"name":"pkg","dist-tags":{"latest":"%s"}}' "$LATEST"; fi
`)
			t.Setenv("ARGS_FILE", argsFile)
			t.Setenv("LATEST", latest)
			packages := make(Packages)
			err := (Yarn{}).listOutdatedScope(context.Background(), filepath.Join(dir, PM_YARN), Scope{Kind: "project", Path: dir}, ManagerOptions{}, packages)
			args, readErr := os.ReadFile(argsFile)
			if err != nil || readErr != nil || len(packages) != 0 || strings.Contains(string(args), "npm info local") {
				t.Fatalf("packages = %#v, args = %q, errors = %v, %v", packages, args, err, readErr)
			}
		})
	}
}

func TestYarnOutdatedClassifiesFailures(t *testing.T) {
	for _, test := range []struct {
		name, output, stderr string
		category             ErrorCategory
	}{
		{name: "network", stderr: "network connection refused", category: ErrorNetwork},
		{name: "authentication", stderr: "HTTP 401 Unauthorized", category: ErrorAuthentication},
		{name: "rate limit", stderr: "HTTP 429 Too Many Requests", category: ErrorRateLimit},
		{name: "command", stderr: "query failed", category: ErrorCommand},
		{name: "malformed", output: "changed output", category: ErrorParser},
		{name: "empty", category: ErrorParser},
		{name: "missing latest", output: `{"name":"pkg","dist-tags":{}}`, category: ErrorParser},
		{name: "wrong name", output: `{"name":"other","dist-tags":{"latest":"2.0.0"}}`, category: ErrorParser},
		{name: "invalid version", output: `{"name":"pkg","dist-tags":{"latest":"next"}}`, category: ErrorParser},
		{name: "extra record", output: "{\"name\":\"pkg\",\"dist-tags\":{\"latest\":\"2.0.0\"}}\n{}", category: ErrorParser},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeExecutable(t, filepath.Join(dir, PM_YARN), `#!/bin/sh
if [ "$1" = "--version" ]; then printf '4.18.1'; elif [ "$1" = "info" ]; then printf '{"value":"pkg@npm:1.0.0","children":{"Version":"1.0.0"}}'; else
printf '%s' "$OUTPUT"
if [ -n "$FAILURE" ]; then printf '%s' "$FAILURE" >&2; exit 2; fi
fi
`)
			t.Setenv("PATH", dir)
			t.Setenv("OUTPUT", test.output)
			t.Setenv("FAILURE", test.stderr)
			result, err := QueryOutdatedScopes(context.Background(), []string{PM_YARN}, []Scope{{Kind: "project", Path: dir}})
			var operationErr *OperationError
			if !errors.As(err, &operationErr) || operationErr.Category != test.category || len(result.Managers) != 1 || result.Managers[0].Operations[0].Category != test.category || len(result.Packages) != 0 {
				t.Fatalf("result = %#v, error = %#v", result, err)
			}
		})
	}
}
