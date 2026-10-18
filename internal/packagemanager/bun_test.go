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

func TestBunAdapterQueriesProjectInventoryAndOutdated(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	writeExecutable(t, filepath.Join(dir, PM_BUN), `#!/bin/sh
printf '%s|%s\n' "$PWD" "$*" >>"$ARGS_FILE"
if [ "$1" = "--version" ]; then printf '1.4.2'; elif [ "$1" = "outdated" ]; then printf '| Package | Current | Update | Latest |\n| react | 19.0.0 | 19.1.0 | 20.0.0 |\n'; else printf '%s node_modules (1 installed)\n└── react@19.0.0\n' "$PWD"; fi
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
	plans, err := PlanUpdates(outdated, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || len(plans[0].Packages) != 1 || plans[0].Packages[0].Requested != "19.1.0" || plans[0].Packages[0].Latest != "20.0.0" || !reflect.DeepEqual(plans[0].Args, []string{"update", "react"}) {
		t.Fatalf("plans = %#v", plans)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(installed.Packages) != 1 || len(Records(outdated.Packages)) != 1 || !strings.Contains(string(args), dir+"|pm ls") || !strings.Contains(string(args), dir+"|outdated\n") || outdated.Managers[0].Operations[0].Status == StatusUnsupported {
		t.Fatalf("installed = %#v, outdated = %#v, args = %q", installed, outdated, args)
	}
}

func TestBunInventoryClassifiesCommandFailure(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_BUN), `#!/bin/sh
if [ "$1" = "--version" ]; then printf '1.4.2'; else printf 'inventory failed' >&2; exit 2; fi
`)
	t.Setenv("PATH", dir)
	result, err := QueryInstalledScopes(context.Background(), []string{PM_BUN}, []Scope{{Kind: "project", Path: dir}})
	if err == nil || len(result.Managers) != 1 || result.Managers[0].Operations[0].Category != ErrorCommand {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestBunInventoryClassifiesParserFailure(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_BUN), `#!/bin/sh
if [ "$1" = "--version" ]; then printf '1.4.2'; else printf 'changed output'; fi
`)
	t.Setenv("PATH", dir)
	result, err := QueryInstalledScopes(context.Background(), []string{PM_BUN}, []Scope{{Kind: "project", Path: dir}})
	var operationErr *OperationError
	if !errors.As(err, &operationErr) || operationErr.Category != ErrorParser || result.Managers[0].Operations[0].Category != ErrorParser {
		t.Fatalf("result = %#v, error = %#v", result, err)
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

func TestBunUpdateArgsUseSelectedPackageNames(t *testing.T) {
	packages := []Package{{Name: "react", Latest: "20.0.0"}, {Name: "@types/bun", Latest: "2.0.0"}}
	if got, want := (Bun{}).updateArgs(packages), []string{"update", "react", "@types/bun"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v; want %#v", got, want)
	}
	if !(Bun{}).updateImpact(Scope{Kind: "project"}).MayChangeManifests || !hasCapability(Bun{}, CapabilityProjectUpdate) || hasCapability(Bun{}, CapabilityGlobalUpdate) {
		t.Fatal("incorrect update capabilities or impact")
	}
}

func TestParseBunOutdatedReadsVersions(t *testing.T) {
	for _, separator := range []string{"|", "│"} {
		t.Run(separator, func(t *testing.T) {
			output := "bun outdated v1.3.0 (abc)\n┌────┬────┐\n| Package | Current | Update | Latest | Workspace |\n├────┼────┤\n| \x1b[1m@types/bun\x1b[0m (dev) | 1.3.0 | 1.3.1 * | 2.0.0 * | root |\n| react (peer) | 18.0.0 | 18.3.0 | 19.0.0 | root |\n| lodash3@npm:lodash (optional) | 3.0.0 | 3.10.1 | 4.17.21 | root |\n└────┴────┘\nNote: The * indicates that version isn't true latest due to minimum release age\n"
			packages, err := parseBunOutdated([]byte(strings.ReplaceAll(output, "|", separator)), "/bin/bun")
			if err != nil {
				t.Fatal(err)
			}
			if len(packages) != 3 || len(packages["@types/bun"]) != 1 || len(packages["react"]) != 1 || len(packages["lodash3"]) != 1 {
				t.Fatalf("packages = %#v", packages)
			}
			pkg := packages["@types/bun"][0]
			if pkg.GetName() != "@types/bun" || pkg.GetPackageManager() != PM_BUN || pkg.GetCurrentVersion() != "1.3.0" || pkg.GetRequestedVersion() != "1.3.1" || pkg.GetLatestVersion() != "2.0.0" || pkg.GetExecutable() != "/bin/bun" {
				t.Fatalf("package = %#v", pkg)
			}
		})
	}
}

func TestParseBunOutdatedExcludesCurrentRangeTargets(t *testing.T) {
	output := "| Package | Current | Update | Latest |\n| pinned | 1.0.0 | 1.0.0 | 2.0.0 |\n| range-current | 1.3.1 | 1.3.1 * | 2.0.0 * |\n| actionable | 1.3.0 | 1.3.1 | 2.0.0 |\n"
	packages, err := parseBunOutdated([]byte(output), "/bin/bun")
	if err != nil || len(packages) != 1 || len(packages["actionable"]) != 1 {
		t.Fatalf("packages = %#v, error = %v", packages, err)
	}
}

func TestBunVerifyRangeUpdateDespiteNewerMajor(t *testing.T) {
	for _, test := range []struct {
		current, status string
	}{
		{current: "1.3.1", status: "completed"},
		{current: "1.3.0", status: "verification-failed"},
	} {
		t.Run(test.status, func(t *testing.T) {
			dir := t.TempDir()
			bun := filepath.Join(dir, PM_BUN)
			writeExecutable(t, bun, "#!/bin/sh\nprintf '| Package | Current | Update | Latest |\\n| tool | "+test.current+" | 1.3.1 | 2.0.0 |\\n'\n")
			plans := []UpdatePlan{{Manager: PM_BUN, Executable: bun, Scope: "project", Directory: dir, StateHash: "plan", Packages: []Package{{Manager: PM_BUN, Name: "tool", Current: "1.3.0", Requested: "1.3.1", Latest: "2.0.0"}}}}
			outcomes, err := VerifyUpdateOutcomes(context.Background(), plans, []UpdateOutcome{{StateHash: "plan", Status: "completed"}}, QueryOptions{})
			if (err != nil) != (test.status == "verification-failed") || len(outcomes) != 1 || outcomes[0].Status != test.status {
				t.Fatalf("outcomes = %#v, error = %v", outcomes, err)
			}
		})
	}
}

func TestParseBunOutdatedEmpty(t *testing.T) {
	for _, output := range []string{"", "\n", "bun outdated v1.4.2 (abc)\n", "| Package | Current | Update | Latest |\n"} {
		packages, err := parseBunOutdated([]byte(output), "bun")
		if err != nil || len(packages) != 0 {
			t.Fatalf("output = %q, packages = %#v, error = %v", output, packages, err)
		}
	}
}

func TestParseBunOutdatedRejectsChangedOutput(t *testing.T) {
	for _, output := range []string{
		`{"react":{"current":"18.0.0"}}`,
		"react 18.0.0 19.0.0",
		"| Package | Current | Wanted | Latest |\n",
		"| Package | Current | Update | Latest |\n| react | 18.0.0 | 18.3.0 |\n",
		"| Package | Current | Update | Latest |\n| react | | 18.3.0 | 19.0.0 |\n",
		"| Package | Current | Update | Latest |\n| react (unknown) | 18.0.0 | 18.3.0 | 19.0.0 |\n",
		"| Package | Current | Update | Latest |\n| --latest | 18.0.0 | 18.3.0 | 19.0.0 |\n",
		"| Package | Current | Update | Latest |\n| react | 18.0.0 | 18.3.0 | 19.0.0 |\nunexpected trailer",
	} {
		packages, err := parseBunOutdated([]byte(output), "bun")
		var operationErr *OperationError
		if !errors.As(err, &operationErr) || operationErr.Category != ErrorParser || packages != nil {
			t.Fatalf("output = %q, packages = %#v, error = %v", output, packages, err)
		}
	}
}

func TestBunOutdatedClassifiesFailures(t *testing.T) {
	for _, test := range []struct {
		name, output, stderr string
		exit                 string
		category             ErrorCategory
	}{
		{name: "command", exit: "2", stderr: "query failed", category: ErrorCommand},
		{name: "exit one is not outdated success", exit: "1", output: "| Package | Current | Update | Latest |", category: ErrorCommand},
		{name: "network", exit: "2", stderr: "network connection refused", category: ErrorNetwork},
		{name: "rate limit", exit: "2", stderr: "HTTP 429 Too Many Requests", category: ErrorRateLimit},
		{name: "authentication", exit: "2", stderr: "HTTP 401 Unauthorized", category: ErrorAuthentication},
		{name: "parser", exit: "0", output: "changed output", category: ErrorParser},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeExecutable(t, filepath.Join(dir, PM_BUN), `#!/bin/sh
if [ "$1" = "--version" ]; then printf '1.4.2'; else printf '%s' "$OUTPUT"; printf '%s' "$FAILURE" >&2; exit "$EXIT_CODE"; fi
`)
			t.Setenv("PATH", dir)
			t.Setenv("OUTPUT", test.output)
			t.Setenv("FAILURE", test.stderr)
			t.Setenv("EXIT_CODE", test.exit)
			result, err := QueryOutdatedScopes(context.Background(), []string{PM_BUN}, []Scope{{Kind: "project", Path: dir}})
			var operationErr *OperationError
			if !errors.As(err, &operationErr) || operationErr.Category != test.category || result.Managers[0].Operations[0].Category != test.category || len(result.Packages) != 0 {
				t.Fatalf("result = %#v, error = %v", result, err)
			}
		})
	}
}
