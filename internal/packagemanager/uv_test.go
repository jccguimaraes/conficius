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

func TestUVUpdateArgsUseSelectedToolNames(t *testing.T) {
	adapter := UV{}
	packages := []Package{{Name: "black"}, {Name: "ruff"}}
	want := []string{"tool", "upgrade", "black", "ruff"}
	if got := adapter.updateArgs(packages); !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v; want %#v", got, want)
	}
}

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

func TestUVOutdatedClassifiesNetworkAndRateLimitFailures(t *testing.T) {
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
			writeExecutable(t, filepath.Join(dir, PM_UV), "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'uv 0.12.19'; else printf '%s' \"$FAILURE\" >&2; exit 1; fi\n")
			t.Setenv("PATH", dir)
			t.Setenv("FAILURE", test.stderr)
			result, err := QueryOutdatedScopes(context.Background(), []string{PM_UV}, []Scope{{Kind: "global"}})
			if err == nil || len(result.Managers) != 1 || len(result.Managers[0].Operations) != 1 || result.Managers[0].Operations[0].Category != test.category {
				t.Fatalf("result = %#v, error = %v", result, err)
			}
		})
	}
}

func TestUVOutdatedClassifiesAuthenticationFailure(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_UV), `#!/bin/sh
if [ "$1" = "--version" ]; then printf 'uv 0.12.19'; else printf 'HTTP 401 Unauthorized' >&2; exit 1; fi
`)
	t.Setenv("PATH", dir)
	result, err := QueryOutdatedScopes(context.Background(), []string{PM_UV}, []Scope{{Kind: "global"}})
	if err == nil || len(result.Managers) != 1 || len(result.Managers[0].Operations) != 1 || result.Managers[0].Operations[0].Category != ErrorAuthentication {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestUVInventoryClassifiesParserFailure(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_UV), `#!/bin/sh
if [ "$1" = "--version" ]; then printf 'uv 0.12.19'; else printf 'changed output'; fi
`)
	t.Setenv("PATH", dir)
	result, err := QueryInstalledScopes(context.Background(), []string{PM_UV}, []Scope{{Kind: "global"}})
	var operationErr *OperationError
	if !errors.As(err, &operationErr) || operationErr.Category != ErrorParser || result.Managers[0].Operations[0].Category != ErrorParser {
		t.Fatalf("result = %#v, error = %#v", result, err)
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
