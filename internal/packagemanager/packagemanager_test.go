package packagemanager

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/config"
)

func TestQueryOptionsFromProfile(t *testing.T) {
	disabled := false
	options := QueryOptionsFromProfile(config.Profile{Managers: map[string]config.ManagerSettings{
		"npm":  {Enabled: &disabled, Executable: "/tools/npm", ExpectedGlobalPrefix: "/prefix"},
		"brew": {IncludeGreedy: true},
	}})
	if !options.Disabled["npm"] || options.Executables["npm"] != "/tools/npm" {
		t.Fatalf("options = %#v", options)
	}
	if options.Managers["npm"].ExpectedGlobalPrefix != "/prefix" || !options.Managers["brew"].IncludeGreedy {
		t.Fatalf("manager options = %#v", options.Managers)
	}
}

func TestPlanUpdatesUsesExactNativeNamesAndExclusions(t *testing.T) {
	result := QueryResult{Packages: Packages{
		"node": {
			NewMiseTool(MiseOutdatedTool{Name: "node", Current: "20", Latest: "22"}, "/bin/mise"),
		},
		"typescript": {
			NewNPMTool("typescript", NPMOutdatedTool{Current: "5.0", Latest: "5.1"}, "/bin/npm"),
		},
		"wget": {
			NewHomebrewTool(HomebrewOutdatedTool{Name: "wget", InstalledVersions: []string{"1"}, CurrentVersion: "2"}, "/bin/brew"),
		},
	}}

	plans, err := PlanUpdates(result, []string{"mise:node", "typescript", "brew:wget"}, []string{"typescript"})
	if err != nil {
		t.Fatal(err)
	}

	want := []UpdatePlan{
		{
			Manager: PM_MISE, Executable: "/bin/mise",
			Scope:    "",
			Args:     []string{"upgrade", "--bump", "--yes", "node"},
			Packages: []Package{{Manager: PM_MISE, Name: "node", Current: "20", Latest: "22", Executable: "/bin/mise"}},
		},
		{
			Manager: PM_BREW, Executable: "/bin/brew",
			Scope:    "",
			Args:     []string{"upgrade", "--yes", "wget"},
			Packages: []Package{{Manager: PM_BREW, Name: "wget", Current: "1", Latest: "2", Executable: "/bin/brew"}},
		},
	}
	if !reflect.DeepEqual(plans, want) {
		t.Fatalf("plans = %#v; want %#v", plans, want)
	}
}

func TestPlanUpdatesRejectsAmbiguousBareName(t *testing.T) {
	result := QueryResult{Packages: Packages{
		"node": {
			NewMiseTool(MiseOutdatedTool{Name: "node", Current: "20", Latest: "22"}, "/bin/mise"),
			NewNPMTool("node", NPMOutdatedTool{Current: "20", Latest: "22"}, "/bin/npm"),
		},
	}}

	_, err := PlanUpdates(result, []string{"node"}, nil)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error = %v; want ambiguity", err)
	}
}

func TestNewHomebrewToolHandlesMissingInstalledVersion(t *testing.T) {
	tool := NewHomebrewTool(HomebrewOutdatedTool{Name: "broken"}, "/bin/brew")
	if tool.GetCurrentVersion() != "" {
		t.Fatalf("current version = %q; want empty", tool.GetCurrentVersion())
	}
}

func TestNPMPrefixWarningsReportMismatchAndMissingPath(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "npm"), "#!/bin/sh\nprintf '/node/version/prefix\\n'\n")
	t.Setenv("PATH", dir)
	warnings := npmPrefixWarnings(context.Background(), filepath.Join(dir, "npm"), filepath.Join(dir, "stable"))
	if len(warnings) != 2 || !strings.Contains(warnings[0], "expected") || !strings.Contains(warnings[1], "not on PATH") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestTypedManagerOptionsControlOutdatedFlags(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	writeExecutable(t, filepath.Join(dir, "mise"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$ARGS_LOG\"\nprintf '{}'\n")
	writeExecutable(t, filepath.Join(dir, "brew"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$ARGS_LOG\"\nprintf '{\"formulae\":[],\"casks\":[]}'\n")
	t.Setenv("PATH", dir)
	t.Setenv("ARGS_LOG", log)
	_, err := QueryOutdatedScopesWithOptions(context.Background(), []string{PM_MISE, PM_BREW}, []Scope{{Kind: "global"}}, QueryOptions{Managers: map[string]ManagerOptions{
		PM_MISE: {IncludeBumpCandidates: true},
		PM_BREW: {IncludeGreedy: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(contents); !strings.Contains(got, "--bump") || !strings.Contains(got, "--greedy") || strings.Contains(got, "--inactive") {
		t.Fatalf("args = %q", got)
	}
}

func TestProjectMiseOutdatedExcludesInactiveGlobalTools(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	log := filepath.Join(dir, "args")
	writeExecutable(t, filepath.Join(dir, "mise"), "#!/bin/sh\nprintf '%s|%s\\n' \"$PWD\" \"$*\" > \"$ARGS_LOG\"\nprintf '{}'\n")
	t.Setenv("PATH", dir)
	t.Setenv("ARGS_LOG", log)
	if _, err := QueryOutdatedScopes(context.Background(), []string{PM_MISE}, []Scope{{Kind: "project", Path: project}}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	got := string(contents)
	if !strings.Contains(got, project+"|outdated --json --local --bump") || strings.Contains(got, "--inactive") {
		t.Fatalf("invocation = %q", got)
	}
}

func TestQueryOutdatedContinuesPastUnavailableManager(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
printf '{"typescript":{"current":"5.0","wanted":"5.1","latest":"5.1"}}'
`)
	t.Setenv("PATH", dir)

	result, err := QueryOutdated(context.Background(), []string{PM_MISE, PM_NPM})
	if err != nil {
		t.Fatal(err)
	}
	if result.Managers[0].Status != StatusUnavailable || result.Managers[1].Status != StatusAvailable {
		t.Fatalf("statuses = %#v", result.Managers)
	}
	if got := Records(result.Packages); len(got) != 1 || got[0].Name != "typescript" {
		t.Fatalf("packages = %#v", got)
	}
}

func TestApplyUpdatesPreservesArgsAndContinuesFailures(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		line := strings.Join(os.Args[1:], "|") + "\n"
		if err := os.WriteFile(os.Getenv("ARGS_FILE"), appendFile(os.Getenv("ARGS_FILE"), []byte(line)), 0o600); err != nil {
			os.Exit(2)
		}
		if os.Args[len(os.Args)-1] == "fail" {
			os.Exit(3)
		}
		os.Exit(0)
	}

	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("ARGS_FILE", argsFile)
	plans := []UpdatePlan{
		{Manager: PM_MISE, Executable: os.Args[0], Args: []string{"-test.run=TestApplyUpdatesPreservesArgsAndContinuesFailures", "--", "two words", "fail"}},
		{Manager: PM_NPM, Executable: os.Args[0], Args: []string{"-test.run=TestApplyUpdatesPreservesArgsAndContinuesFailures", "--", "@scope/pkg"}},
	}

	err := ApplyUpdates(context.Background(), plans, bytes.NewBufferString("input"), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), PM_MISE) {
		t.Fatalf("error = %v", err)
	}
	got, readErr := os.ReadFile(argsFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(got), "two words|fail") || !strings.Contains(string(got), "@scope/pkg") {
		t.Fatalf("recorded args = %q", got)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

func appendFile(path string, content []byte) []byte {
	existing, _ := os.ReadFile(path)
	return append(existing, content...)
}
