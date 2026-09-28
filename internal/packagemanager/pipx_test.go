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

func TestPipxUpdateArgsUseSelectedPackageNames(t *testing.T) {
	adapter := Pipx{}
	packages := []Package{{Name: "black"}, {Name: "ruff"}}
	want := []string{"upgrade", "black", "ruff"}
	if got := adapter.updateArgs(packages); !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v; want %#v", got, want)
	}
}

func TestPipxAdapterQueriesGlobalInventoryAndOutdated(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	writeExecutable(t, filepath.Join(dir, PM_PIPX), `#!/bin/sh
printf '%s\n' "$*" >>"$ARGS_FILE"
if [ "$1" = "--version" ]; then printf '1.17.7'; elif [ "$2" = "--outdated" ]; then printf '{"pipx_result_version":"1","status":"success","data":{"packages":[{"package":"black","version":"24.2.0","latest_version":"25.1.0"}]}}'; else printf '{"pipx_spec_version":"0.1","venvs":{"black":{"metadata":{"main_package":{"package":"black","package_version":"24.2.0","apps":["black"]}}}}}'; fi
`)
	t.Setenv("PATH", dir)
	t.Setenv("ARGS_FILE", argsFile)
	installed, err := QueryInstalledScopes(context.Background(), []string{PM_PIPX}, []Scope{{Kind: "global"}})
	if err != nil {
		t.Fatal(err)
	}
	outdated, err := QueryOutdatedScopes(context.Background(), []string{PM_PIPX}, []Scope{{Kind: "global"}})
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(installed.Packages) != 1 || len(Records(outdated.Packages)) != 1 || !strings.Contains(string(args), "list --output json\n") || !strings.Contains(string(args), "list --outdated --output json") {
		t.Fatalf("installed = %#v, outdated = %#v, args = %q", installed.Packages, Records(outdated.Packages), args)
	}
}

func TestPipxOutdatedClassifiesNetworkAndRateLimitFailures(t *testing.T) {
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
			writeExecutable(t, filepath.Join(dir, PM_PIPX), "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf '1.17.7'; else printf '%s' \"$FAILURE\" >&2; exit 1; fi\n")
			t.Setenv("PATH", dir)
			t.Setenv("FAILURE", test.stderr)
			result, err := QueryOutdatedScopes(context.Background(), []string{PM_PIPX}, []Scope{{Kind: "global"}})
			if err == nil || len(result.Managers) != 1 || len(result.Managers[0].Operations) != 1 || result.Managers[0].Operations[0].Category != test.category {
				t.Fatalf("result = %#v, error = %v", result, err)
			}
		})
	}
}

func TestPipxOutdatedClassifiesAuthenticationFailure(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_PIPX), `#!/bin/sh
if [ "$1" = "--version" ]; then printf '1.17.7'; else printf 'HTTP 401 Unauthorized' >&2; exit 1; fi
`)
	t.Setenv("PATH", dir)
	result, err := QueryOutdatedScopes(context.Background(), []string{PM_PIPX}, []Scope{{Kind: "global"}})
	if err == nil || len(result.Managers) != 1 || len(result.Managers[0].Operations) != 1 || result.Managers[0].Operations[0].Category != ErrorAuthentication {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestPipxInventoryClassifiesParserFailure(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_PIPX), `#!/bin/sh
if [ "$1" = "--version" ]; then printf '1.17.7'; else printf 'changed output'; fi
`)
	t.Setenv("PATH", dir)
	result, err := QueryInstalledScopes(context.Background(), []string{PM_PIPX}, []Scope{{Kind: "global"}})
	var operationErr *OperationError
	if !errors.As(err, &operationErr) || operationErr.Category != ErrorParser || result.Managers[0].Operations[0].Category != ErrorParser {
		t.Fatalf("result = %#v, error = %#v", result, err)
	}
}

func TestParsePipxOutdatedReadsVersions(t *testing.T) {
	output := []byte(`{"pipx_result_version":"1","status":"success","data":{"packages_checked":1,"packages":[{"environment":"black","package":"black","version":"24.2.0","latest_version":"25.1.0","pinned":false,"injected":false}],"skipped":[]},"errors":[],"exit_code":0}`)
	packages, err := parsePipxOutdated(output, "/usr/local/bin/pipx")
	if err != nil {
		t.Fatal(err)
	}
	records := Records(packages)
	if len(records) != 1 || records[0].Manager != PM_PIPX || records[0].Name != "black" || records[0].Current != "24.2.0" || records[0].Latest != "25.1.0" || records[0].Executable != "/usr/local/bin/pipx" {
		t.Fatalf("records = %#v", records)
	}
}

func TestParsePipxOutdatedRejectsChangedOutput(t *testing.T) {
	if _, err := parsePipxOutdated([]byte(`{"status":"success","data":{}}`), "pipx"); err == nil {
		t.Fatal("parse succeeded")
	}
}

func TestParsePipxInstalledReadsSortedInventory(t *testing.T) {
	output := []byte(`{"pipx_spec_version":"0.1","venvs":{"ruff":{"metadata":{"main_package":{"package":"ruff","package_version":"0.9.0","apps":["ruff"]}}},"black":{"metadata":{"main_package":{"package":"black","package_version":"24.2.0","apps":["black","blackd"]}}}}}`)
	got, err := parsePipxInstalled(output)
	if err != nil {
		t.Fatal(err)
	}
	want := []InstalledPackage{
		{Manager: PM_PIPX, Name: "black", Version: "24.2.0", Role: RoleApplication},
		{Manager: PM_PIPX, Name: "ruff", Version: "0.9.0", Role: RoleApplication},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packages = %#v; want %#v", got, want)
	}
}

func TestParsePipxInstalledRejectsChangedOutput(t *testing.T) {
	if _, err := parsePipxInstalled([]byte(`{"venvs":[]}`)); err == nil {
		t.Fatal("parse succeeded")
	}
}
