package packagemanager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestPNPMUpdateArgsUseSelectedPackageNames(t *testing.T) {
	adapter := PNPM{}
	packages := []Package{{Name: "react"}, {Name: "typescript"}}
	want := []string{"update", "react", "typescript"}
	if got := adapter.updateArgs(packages); !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v; want %#v", got, want)
	}
}

func TestPNPMAdapterQueriesProjectInventoryAndOutdated(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	writeExecutable(t, filepath.Join(dir, PM_PNPM), `#!/bin/sh
printf '%s|%s\n' "$PWD" "$*" >>"$ARGS_FILE"
if [ "$1" = "--version" ]; then printf '12.6.0'; elif [ "$1" = "outdated" ]; then printf '{"react":{"current":"18.0.0","wanted":"18.3.0","latest":"19.0.0","dependencyType":"dependencies"}}'; exit 1; else printf '[{"dependencies":{"react":{"version":"18.0.0"}}}]'; fi
`)
	t.Setenv("PATH", dir)
	t.Setenv("ARGS_FILE", argsFile)
	scope := Scope{Kind: "project", Path: dir}
	installed, err := QueryInstalledScopes(context.Background(), []string{PM_PNPM}, []Scope{scope})
	if err != nil {
		t.Fatal(err)
	}
	outdated, err := QueryOutdatedScopes(context.Background(), []string{PM_PNPM}, []Scope{scope})
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(installed.Packages) != 1 || len(Records(outdated.Packages)) != 1 || !strings.Contains(string(args), dir+"|list --depth 0 --json") || !strings.Contains(string(args), dir+"|outdated --format json") {
		t.Fatalf("installed = %#v, outdated = %#v, args = %q", installed.Packages, Records(outdated.Packages), args)
	}
}

func TestPNPMOutdatedClassifiesNetworkAndRateLimitFailures(t *testing.T) {
	for _, test := range []struct {
		name     string
		stderr   string
		category ErrorCategory
	}{
		{name: "network", stderr: "network connection refused", category: ErrorNetwork},
		{name: "rate limit", stderr: "HTTP 429 Too Many Requests", category: ErrorRateLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeExecutable(t, filepath.Join(dir, PM_PNPM), "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf '12.6.0'; else printf '%s' \"$FAILURE\" >&2; exit 2; fi\n")
			t.Setenv("PATH", dir)
			t.Setenv("FAILURE", test.stderr)
			result, err := QueryOutdatedScopes(context.Background(), []string{PM_PNPM}, []Scope{{Kind: "project", Path: dir}})
			if err == nil || len(result.Managers) != 1 || len(result.Managers[0].Operations) != 1 || result.Managers[0].Operations[0].Category != test.category {
				t.Fatalf("result = %#v, error = %v", result, err)
			}
		})
	}
}

func TestPNPMOutdatedClassifiesAuthenticationFailure(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_PNPM), `#!/bin/sh
if [ "$1" = "--version" ]; then printf '12.6.0'; else printf 'HTTP 401 Unauthorized' >&2; exit 2; fi
`)
	t.Setenv("PATH", dir)
	result, err := QueryOutdatedScopes(context.Background(), []string{PM_PNPM}, []Scope{{Kind: "project", Path: dir}})
	if err == nil || len(result.Managers) != 1 || len(result.Managers[0].Operations) != 1 || result.Managers[0].Operations[0].Category != ErrorAuthentication {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestPNPMInventoryClassifiesParserFailure(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_PNPM), `#!/bin/sh
if [ "$1" = "--version" ]; then printf '12.6.0'; else printf 'changed output'; fi
`)
	t.Setenv("PATH", dir)
	result, err := QueryInstalledScopes(context.Background(), []string{PM_PNPM}, []Scope{{Kind: "project", Path: dir}})
	var operationErr *OperationError
	if !errors.As(err, &operationErr) || operationErr.Category != ErrorParser || result.Managers[0].Operations[0].Category != ErrorParser {
		t.Fatalf("result = %#v, error = %#v", result, err)
	}
}

func TestParsePNPMInstalledReadsDependencyClasses(t *testing.T) {
	output := []byte(`[{"name":"project","version":"1.0.0","path":"/project","dependencies":{"react":{"version":"19.0.0"}},"devDependencies":{"typescript":{"version":"5.7.0"}},"optionalDependencies":{"fsevents":{"version":"2.3.3"}}}]`)
	packages, err := parsePNPMInstalled(output)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].Name < packages[j].Name })
	if len(packages) != 3 || packages[0].Name != "fsevents" || packages[0].DependencyClass != "optional" || packages[1].Role != RoleLibrary || packages[2].Role != RoleDevelopmentTool {
		t.Fatalf("packages = %#v", packages)
	}
}

func TestParsePNPMInstalledRejectsChangedOutput(t *testing.T) {
	if _, err := parsePNPMInstalled([]byte(`{}`)); err == nil {
		t.Fatal("parse succeeded")
	}
}

func TestParsePNPMOutdatedReadsVersions(t *testing.T) {
	output := []byte(`{"is-negative":{"current":"1.0.0","latest":"2.1.0","wanted":"1.0.0","isDeprecated":false,"dependencyType":"dependencies"},"is-positive":{"current":"1.0.0","latest":"3.1.0","wanted":"2.0.0","isDeprecated":false,"dependencyType":"devDependencies"}}`)
	packages, err := parsePNPMOutdated(output, "/usr/local/bin/pnpm")
	if err != nil {
		t.Fatal(err)
	}
	records := Records(packages)
	if len(records) != 2 || records[0].Manager != PM_PNPM || records[0].Name != "is-negative" || records[0].Current != "1.0.0" || records[0].Requested != "1.0.0" || records[0].Latest != "2.1.0" || records[0].Executable != "/usr/local/bin/pnpm" {
		t.Fatalf("records = %#v", records)
	}
}

func TestParsePNPMOutdatedRejectsChangedOutput(t *testing.T) {
	if _, err := parsePNPMOutdated([]byte(`[]`), "pnpm"); err == nil {
		t.Fatal("parse succeeded")
	}
}
