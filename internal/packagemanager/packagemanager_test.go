package packagemanager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jccguimaraes/conficius/internal/config"
)

func TestApplyUpdatesReportsHomebrewAppConflict(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "brew")
	message := "Error: notion: It seems there is already an App at '/opt/homebrew/Caskroom/notion/7.36.1/Notion.app'."
	writeExecutable(t, executable, "#!/bin/sh\nprintf '%s\\n' \"$CONFLICT\"\nexit 1\n")
	t.Setenv("CONFLICT", message)
	var output bytes.Buffer
	outcomes, err := ApplyUpdatesWithOutcomes(context.Background(), []UpdatePlan{{Manager: PM_HOMEBREW, Executable: executable, Args: []string{"upgrade", "notion"}}}, false, nil, &output, nil)
	if err == nil || len(outcomes) != 1 || outcomes[0].Status != "failed" {
		t.Fatalf("outcomes = %#v, error = %v", outcomes, err)
	}
	for _, text := range []string{message, "Inspect both app copies before repair.", "Conficius did not attempt automatic repair.", "Homebrew may have changed files before the failure."} {
		if !strings.Contains(err.Error(), text) || !strings.Contains(outcomes[0].Error, text) {
			t.Fatalf("missing %q: outcomes = %#v, error = %v", text, outcomes, err)
		}
	}
	if !strings.Contains(output.String(), message) {
		t.Fatalf("original output missing: %q", output.String())
	}
}

func TestInspectWithOptionsReportsDisabledManagerAndExecutableEvidence(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, PM_NPM)
	writeExecutable(t, executable, "#!/bin/sh\nprintf '11.0.0'\n")
	t.Setenv("PATH", dir)
	statuses, err := inspectWithOptions([]string{PM_NPM}, QueryOptions{Disabled: map[string]bool{PM_NPM: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Status != StatusDisabled || !statuses[0].Evidence.Executable || !reflect.DeepEqual(statuses[0].Executables, []string{executable}) {
		t.Fatalf("statuses = %#v", statuses)
	}
}

func TestCheckManagerIntegritySeparatesHealthFromFreshness(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_NPM), "#!/bin/sh\nprintf '11.0.0'\n")
	t.Setenv("PATH", dir)
	checks, err := CheckManagerIntegrity([]string{PM_NPM, PM_MISE}, QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []ManagerIntegrity{
		{Manager: PM_NPM, Status: "healthy", Message: "executable and manager version available"},
		{Manager: PM_MISE, Status: StatusUnavailable, Message: "executable not found"},
	}
	if !reflect.DeepEqual(checks, want) {
		t.Fatalf("checks = %#v; want %#v", checks, want)
	}
}

func TestCheckManagerIntegrityWarnsForUntestedVersion(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_NPM), "#!/bin/sh\nprintf '12.0.0'\n")
	t.Setenv("PATH", dir)
	checks, err := CheckManagerIntegrity([]string{PM_NPM}, QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 1 || checks[0].Status != "warning" || !strings.Contains(checks[0].Message, "outside tested range") {
		t.Fatalf("checks = %#v", checks)
	}
}

func TestNormalizeScopesPrefersWorkspaceForSamePhysicalRoot(t *testing.T) {
	root := t.TempDir()
	scopes, err := normalizeScopes([]Scope{{Kind: "project", Path: root}, {Kind: "workspace", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Scope{{Kind: "workspace", Path: root}}
	if !reflect.DeepEqual(scopes, want) {
		t.Fatalf("scopes = %#v; want %#v", scopes, want)
	}
}

func TestNormalizeScopesResolvesSymlinksAndRemovesDuplicates(t *testing.T) {
	project := t.TempDir()
	link := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(project, link); err != nil {
		t.Fatal(err)
	}
	scopes, err := normalizeScopes([]Scope{{Kind: "project", Path: project}, {Kind: "project", Path: link}})
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 1 || scopes[0].Path != project {
		t.Fatalf("scopes = %#v", scopes)
	}
}

func TestNormalizeScopesUsesAbsolutePathsAndRemovesDuplicates(t *testing.T) {
	project := t.TempDir()
	scopes, err := normalizeScopes([]Scope{{Kind: "project", Path: project}, {Kind: "project", Path: filepath.Join(project, ".")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 1 || scopes[0].Path != project {
		t.Fatalf("scopes = %#v", scopes)
	}
}

func TestQueriesRejectInvalidScopes(t *testing.T) {
	tests := []struct {
		scope Scope
		want  string
	}{{Scope{Kind: "project"}, "project scope requires a path"}, {Scope{Kind: "global", Path: "/tmp"}, "global scope must not have a path"}, {Scope{Kind: "mystery"}, `unsupported scope "mystery"`}}
	for _, test := range tests {
		_, err := QueryOutdatedScopesWithOptions(context.Background(), nil, []Scope{test.scope}, QueryOptions{})
		if err == nil || err.Error() != test.want {
			t.Errorf("scope %#v: err = %v; want %q", test.scope, err, test.want)
		}
	}
}

func TestRustProjectDoesNotRunNPMWithoutPackageManifest(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "Cargo.toml"), []byte("[package]\nname = \"app\"\nversion = \"0.1.0\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(dir, PM_NPM), "#!/bin/sh\nprintf '11.0.0\\n'\n")
	t.Setenv("PATH", dir)

	inventory, err := QueryInstalledScopesWithOptions(context.Background(), []string{PM_NPM}, []Scope{{Kind: "project", Path: project}}, QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Packages) != 0 || len(inventory.Managers) != 1 || len(inventory.Managers[0].Operations) != 0 {
		t.Fatalf("inventory = %#v", inventory)
	}
	outdated, err := QueryOutdatedScopesWithOptions(context.Background(), []string{PM_NPM}, []Scope{{Kind: "project", Path: project}}, QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(outdated.Packages) != 0 || len(outdated.Managers) != 1 || len(outdated.Managers[0].Operations) != 0 {
		t.Fatalf("outdated = %#v", outdated)
	}
}

func TestManagerFilterCannotExcludeManifestManager(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := QueryOutdatedScopesWithOptions(context.Background(), []string{PM_MISE}, []Scope{{Kind: "project", Path: project}}, QueryOptions{})
	if err == nil || err.Error() != "required manager npm is excluded by manager filter" {
		t.Fatalf("err = %v", err)
	}
}

func TestManifestConflictsAreDeduplicatedAcrossEquivalentScopes(t *testing.T) {
	project := t.TempDir()
	manifest := filepath.Join(project, "package.json")
	if err := os.WriteFile(manifest, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := requireManifestManagers(QueryOptions{Disabled: map[string]bool{PM_NPM: true}}, []Scope{{Kind: "project", Path: project}, {Kind: "workspace", Path: filepath.Join(project, ".")}})
	if err == nil || strings.Count(err.Error(), "manager npm is disabled") != 1 {
		t.Fatalf("err = %v", err)
	}
}

func TestConcurrentManifestChecksDoNotMutateSharedOptions(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	options := QueryOptions{Required: map[string]bool{}}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := requireManifestManagers(options, []Scope{{Kind: "project", Path: project}})
			errs <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if len(options.Required) != 0 {
		t.Fatalf("shared required managers = %#v", options.Required)
	}
}

func TestManifestInspectionErrorsAreReturned(t *testing.T) {
	project := t.TempDir()
	manifest := filepath.Join(project, "package.json")
	if err := os.Symlink(filepath.Join(project, "missing"), manifest); err != nil {
		t.Fatal(err)
	}
	_, err := requireManifestManagers(QueryOptions{}, []Scope{{Kind: "project", Path: project}})
	if err == nil || !strings.Contains(err.Error(), "inspect manager manifest "+manifest) {
		t.Fatalf("err = %v", err)
	}
}

func TestDisabledManifestManagersReturnAllConfigurationConflicts(t *testing.T) {
	project := t.TempDir()
	for _, manifest := range []string{"Brewfile", "package.json"} {
		if err := os.WriteFile(filepath.Join(project, manifest), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, err := QueryOutdatedScopesWithOptions(context.Background(), nil, []Scope{{Kind: "project", Path: project}}, QueryOptions{Disabled: map[string]bool{PM_BREW: true, PM_NPM: true}})
	if err == nil || !strings.Contains(err.Error(), "manager brew is disabled") || !strings.Contains(err.Error(), "manager npm is disabled") {
		t.Fatalf("err = %v", err)
	}
}

func TestDisabledManifestManagerReturnsConfigurationConflict(t *testing.T) {
	project := t.TempDir()
	manifest := filepath.Join(project, "package.json")
	if err := os.WriteFile(manifest, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := QueryOutdatedScopesWithOptions(context.Background(), nil, []Scope{{Kind: "project", Path: project}}, QueryOptions{Disabled: map[string]bool{PM_NPM: true}})
	if err == nil || !strings.Contains(err.Error(), "manager npm is disabled but required by "+manifest) {
		t.Fatalf("err = %v", err)
	}
}

func TestProjectManifestRequiresItsManager(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	result, err := QueryOutdatedScopesWithOptions(context.Background(), nil, []Scope{{Kind: "project", Path: project}}, QueryOptions{})
	if err == nil || !strings.Contains(err.Error(), "npm: required manager is unavailable") {
		t.Fatalf("err = %v", err)
	}
	for _, status := range result.Managers {
		if status.Name == PM_NPM && status.Status == StatusError {
			return
		}
	}
	t.Fatalf("managers = %#v", result.Managers)
}

func TestRequiredManagerUnavailableReturnsInspectionError(t *testing.T) {
	t.Setenv("PATH", "")
	result, err := QueryOutdatedScopesWithOptions(context.Background(), []string{PM_NPM}, []Scope{{Kind: "global"}}, QueryOptions{Required: map[string]bool{PM_NPM: true}})
	if err == nil || !strings.Contains(err.Error(), "npm: required manager is unavailable") {
		t.Fatalf("err = %v", err)
	}
	if len(result.Managers) != 1 || result.Managers[0].Status != StatusError {
		t.Fatalf("managers = %#v", result.Managers)
	}
}

func TestInspectWithOptionsSeparatesGlobalConfigFromExecutableEvidence(t *testing.T) {
	t.Setenv("PATH", "")
	statuses, err := inspectWithOptions([]string{PM_NPM}, QueryOptions{GlobalConfig: map[string]bool{PM_NPM: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Status != StatusUnavailable || statuses[0].Evidence.Executable || !statuses[0].Evidence.GlobalConfig || statuses[0].Error != "executable not found; global config detected" {
		t.Fatalf("statuses = %#v", statuses)
	}
}

func TestInspectWithOptionsUsesConfiguredExecutableVersion(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "custom-npm")
	writeExecutable(t, executable, "#!/bin/sh\nprintf '11.4.0'\n")
	t.Setenv("PATH", "")
	statuses, err := inspectWithOptions([]string{PM_NPM}, QueryOptions{Executables: map[string]string{PM_NPM: executable}})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Status != StatusAvailable || statuses[0].ManagerVersion != "11.4.0" || !reflect.DeepEqual(statuses[0].Executables, []string{executable}) {
		t.Fatalf("statuses = %#v", statuses)
	}
}

func TestInspectWithOptionsRejectsMissingConfiguredExecutable(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "missing-npm")
	statuses, err := inspectWithOptions([]string{PM_NPM}, QueryOptions{Executables: map[string]string{PM_NPM: executable}})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Status != StatusUnavailable || statuses[0].Error != "configured executable is unavailable" {
		t.Fatalf("statuses = %#v", statuses)
	}
}

func TestQueryOptionsFromProfile(t *testing.T) {
	disabled, enabled := false, true
	options := QueryOptionsFromProfile(config.Profile{
		Managers: map[string]config.ManagerSettings{
			"npm":  {Enabled: &disabled},
			"mise": {Enabled: &enabled},
			"brew": {},
		},
		Projects: []config.ProfileProject{{Name: "app", Path: "/app", ResolvedPath: "/app", AllowManifestChanges: true}},
	})
	want := QueryOptions{
		GlobalConfig: map[string]bool{"npm": true, "mise": true, "brew": true},
		Disabled:     map[string]bool{"npm": true},
		Required:     map[string]bool{"mise": true},
	}
	if !reflect.DeepEqual(options, want) {
		t.Fatalf("options = %#v; want %#v", options, want)
	}
}

func TestProfileQueryPreservesPATHAmbiguityAndDefaultVersionLookup(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	for _, dir := range []string{first, second} {
		writeExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
case "$1" in
  --version) printf '11.4.0' ;;
  outdated) printf '{"typescript":{"current":"5.0","wanted":"5.1","latest":"5.2"}}' ;;
esac
`)
	}
	t.Setenv("PATH", strings.Join([]string{first, second, first}, string(os.PathListSeparator)))
	options := QueryOptionsFromProfile(config.Profile{Managers: map[string]config.ManagerSettings{PM_NPM: {}}})
	result, err := QueryOutdatedScopesWithOptions(context.Background(), []string{PM_NPM}, []Scope{{Kind: "global"}}, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Managers) != 1 {
		t.Fatalf("managers = %#v", result.Managers)
	}
	status := result.Managers[0]
	wantPaths := []string{filepath.Join(first, "npm"), filepath.Join(second, "npm")}
	if !reflect.DeepEqual(status.Executables, wantPaths) || len(status.Warnings) != 1 || !strings.Contains(status.Warnings[0], "multiple executables found on PATH") {
		t.Fatalf("status = %#v", status)
	}
	records := Records(result.Packages)
	if len(records) != 2 {
		t.Fatalf("records = %#v", records)
	}
	for _, record := range records {
		if record.Current != "5.0" || record.Latest != "5.2" || record.Requested != "5.1" {
			t.Fatalf("record = %#v", record)
		}
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
			Scope:      "",
			Confidence: PlanConfidenceEstimated,
			Args:       []string{"upgrade", "--bump", "--yes", "node"},
			Packages:   []Package{{Manager: PM_MISE, Name: "node", Current: "20", Latest: "22", Role: RoleRuntime, Executable: "/bin/mise"}},
		},
		{
			Manager: PM_BREW, Executable: "/bin/brew",
			Scope:      "",
			Confidence: PlanConfidenceEstimated,
			Impact:     MutationImpact{MayRestartServices: true},
			Args:       []string{"upgrade", "--yes", "--greedy", "wget"},
			Packages:   []Package{{Manager: PM_BREW, Name: "wget", Current: "1", Latest: "2", Role: RoleLibrary, Executable: "/bin/brew"}},
		},
	}
	want[0].Recovery = []RecoveryCommand{{Executable: "/bin/mise", Args: []string{"use", "node@20"}}}
	for i := range want {
		want[i].StateHash = plans[i].StateHash
		if want[i].StateHash == "" {
			t.Fatal("plan state hash is empty")
		}
	}
	if !reflect.DeepEqual(plans, want) {
		t.Fatalf("plans = %#v; want %#v", plans, want)
	}
}

func TestNPMRecoveryArgsRespectScope(t *testing.T) {
	adapter := NPM{}
	packages := []Package{{Name: "typescript", Current: "5.0"}}
	if got := adapter.recoveryArgs(Scope{Kind: "global"}, packages); !reflect.DeepEqual(got, [][]string{{"install", "--global", "typescript@5.0"}}) {
		t.Fatalf("global recovery = %#v", got)
	}
	if got := adapter.recoveryArgs(Scope{Kind: "project"}, packages); !reflect.DeepEqual(got, [][]string{{"install", "typescript@5.0"}}) {
		t.Fatalf("project recovery = %#v", got)
	}
}

func TestUpdatePlanHashChangesWithPackageState(t *testing.T) {
	plan := UpdatePlan{Manager: PM_NPM, Executable: "/bin/npm", Scope: "global", Confidence: PlanConfidenceEstimated, Args: []string{"update", "typescript"}, Packages: []Package{{Manager: PM_NPM, Name: "typescript", Current: "5.0", Latest: "5.1"}}}
	first := updatePlanHash(plan)
	if first != updatePlanHash(plan) {
		t.Fatal("same plan produced unstable hash")
	}
	plan.Packages[0].Latest = "6.0"
	if first == updatePlanHash(plan) {
		t.Fatal("changed package state produced same hash")
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

func TestHomebrewRefreshesBeforeOutdated(t *testing.T) {
	for _, failUpdate := range []bool{false, true} {
		t.Run(fmt.Sprintf("updateFailure=%t", failUpdate), func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "args")
			writeExecutable(t, filepath.Join(dir, "brew"), `#!/bin/sh
printf '%s\n' "$*" >> "$ARGS_LOG"
case "$1" in
  --version) printf 'Homebrew 5.0.0\n' ;;
  update)
    if [ "$FAIL_UPDATE" = "true" ]; then
      printf 'metadata refresh failed\n' >&2
      exit 1
    fi
    printf 'Updated Homebrew!\n'
    ;;
  outdated) printf '{"formulae":[{"name":"wget","installed_versions":["1"],"current_version":"2"}],"casks":[]}' ;;
esac
`)
			t.Setenv("PATH", dir)
			t.Setenv("ARGS_LOG", log)
			t.Setenv("FAIL_UPDATE", fmt.Sprint(failUpdate))
			result, err := QueryOutdatedScopesWithOptions(context.Background(), []string{PM_BREW}, []Scope{{Kind: "global"}}, QueryOptions{})
			contents, readErr := os.ReadFile(log)
			if readErr != nil {
				t.Fatal(readErr)
			}
			calls := string(contents)
			if failUpdate {
				if err == nil || !strings.Contains(err.Error(), "metadata refresh failed") || strings.Contains(calls, "outdated") || len(result.Packages) != 0 {
					t.Fatalf("failed refresh: err=%v packages=%v calls=%q", err, result.Packages, calls)
				}
			} else if err != nil || len(result.Packages["wget"]) != 1 || !strings.Contains(calls, "update\noutdated --json=v2 --greedy\n") || strings.Count(calls, "update\n") != 1 {
				t.Fatalf("refresh order: err=%v packages=%v calls=%q", err, result.Packages, calls)
			}
		})
	}
}

func TestHomebrewOutdatedIsAlwaysGreedy(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	writeExecutable(t, filepath.Join(dir, "brew"), "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$ARGS_LOG\"\nprintf '{\"formulae\":[],\"casks\":[]}'\n")
	t.Setenv("PATH", dir)
	t.Setenv("ARGS_LOG", log)
	if _, err := QueryOutdatedScopesWithOptions(context.Background(), []string{PM_BREW}, []Scope{{Kind: "global"}}, QueryOptions{}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "--greedy") {
		t.Fatalf("args = %q", contents)
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

func TestQueryOutdatedReportsPartialManagerAndPreservesSuccessfulScope(t *testing.T) {
	dir := t.TempDir()
	goodProject := t.TempDir()
	badProject := t.TempDir()
	for _, project := range []string{goodProject, badProject} {
		if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
if [ "$PWD" = "$BAD_PROJECT" ]; then
  printf 'network connection refused' >&2
  exit 1
fi
printf '{"typescript":{"current":"5.0","wanted":"5.1","latest":"5.1"}}'
`)
	t.Setenv("PATH", dir)
	t.Setenv("BAD_PROJECT", badProject)

	result, err := QueryOutdatedScopes(context.Background(), []string{PM_NPM}, []Scope{{Kind: "project", Path: goodProject}, {Kind: "project", Path: badProject}})
	if err == nil {
		t.Fatal("query succeeded; want partial error")
	}
	status := result.Managers[0]
	if status.Status != StatusPartial || len(status.Operations) != 2 {
		t.Fatalf("status = %#v", status)
	}
	if status.Operations[1].Category != ErrorNetwork {
		t.Fatalf("failed operation = %#v", status.Operations[1])
	}
	records := Records(result.Packages)
	if len(records) != 1 || records[0].Path != goodProject || records[0].Manifest != filepath.Join(goodProject, "package.json") {
		t.Fatalf("packages = %#v", records)
	}
}

func TestQueryOutdatedClassifiesAndFiltersExtraneousNPMDependencies(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(dir, "npm"), "#!/bin/sh\nprintf '{\"left-pad\":{\"current\":\"1\",\"wanted\":\"2\",\"latest\":\"2\"}}'\n")
	t.Setenv("PATH", dir)
	result, err := QueryOutdatedScopesWithOptions(context.Background(), []string{PM_NPM}, []Scope{{Kind: "project", Path: project}}, QueryOptions{DependencyClasses: map[string]map[string]bool{project: {"extraneous": true}}})
	if err != nil {
		t.Fatal(err)
	}
	records := Records(result.Packages)
	if len(records) != 1 || records[0].Name != "left-pad" || records[0].DependencyClass != "extraneous" {
		t.Fatalf("records = %#v", records)
	}
}

func TestQueryOutdatedFiltersConfiguredNPMDependencyClasses(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{"dependencies":{"react":"1"},"devDependencies":{"typescript":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(dir, "npm"), "#!/bin/sh\nprintf '{\"react\":{\"current\":\"1\",\"wanted\":\"2\",\"latest\":\"2\"},\"typescript\":{\"current\":\"1\",\"wanted\":\"2\",\"latest\":\"2\"}}'\n")
	t.Setenv("PATH", dir)
	result, err := QueryOutdatedScopesWithOptions(context.Background(), []string{PM_NPM}, []Scope{{Kind: "project", Path: project}}, QueryOptions{DependencyClasses: map[string]map[string]bool{project: {"development": true}}})
	if err != nil {
		t.Fatal(err)
	}
	records := Records(result.Packages)
	if len(records) != 1 || records[0].Name != "typescript" || records[0].DependencyClass != "development" || records[0].Role != RoleDevelopmentTool {
		t.Fatalf("records = %#v", records)
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

func TestPlanUpdatesInstallsNPMManagerVersionGlobally(t *testing.T) {
	result := QueryResult{Packages: Packages{"npm": []Tool{NewNPMTool("npm", NPMOutdatedTool{Current: "11.19.1", Latest: "11.20.0", Wanted: "11.20.0"}, "/bin/npm")}}}
	plans, err := PlanUpdates(result, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || !reflect.DeepEqual(plans[0].Args, []string{"install", "--global", "npm@11.20.0"}) {
		t.Fatalf("plans = %#v", plans)
	}
}

func TestVerifyUpdateOutcomesMarksPackageStillOutdated(t *testing.T) {
	dir := t.TempDir()
	npm := filepath.Join(dir, "npm")
	writeExecutable(t, npm, "#!/bin/sh\nprintf '{\"typescript\":{\"current\":\"5.0\",\"wanted\":\"5.1\",\"latest\":\"5.1\"}}'\n")
	plans := []UpdatePlan{{Manager: PM_NPM, Executable: npm, Scope: "global", StateHash: "plan", Packages: []Package{{Manager: PM_NPM, Name: "typescript"}}}}
	outcomes, err := VerifyUpdateOutcomes(context.Background(), plans, []UpdateOutcome{{StateHash: "plan", Status: "completed"}}, QueryOptions{})
	if err == nil || len(outcomes) != 1 || outcomes[0].Status != "verification-failed" || !strings.Contains(outcomes[0].Error, "remains outdated") {
		t.Fatalf("outcomes = %#v; error = %v", outcomes, err)
	}
}

func TestVerifyUpdateOutcomesKeepsCompletedWhenTargetCurrent(t *testing.T) {
	dir := t.TempDir()
	npm := filepath.Join(dir, "npm")
	writeExecutable(t, npm, "#!/bin/sh\nprintf '{}'\n")
	plans := []UpdatePlan{{Manager: PM_NPM, Executable: npm, Scope: "global", StateHash: "plan", Packages: []Package{{Manager: PM_NPM, Name: "typescript"}}}}
	outcomes, err := VerifyUpdateOutcomes(context.Background(), plans, []UpdateOutcome{{StateHash: "plan", Status: "completed"}}, QueryOptions{})
	if err != nil || outcomes[0].Status != "completed" {
		t.Fatalf("outcomes = %#v; error = %v", outcomes, err)
	}
}

func TestManagerEnvironmentOverridesLocaleAndColorWithoutDroppingInheritedValues(t *testing.T) {
	t.Setenv("NO_COLOR", "0")
	t.Setenv("CLICOLOR", "1")
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("CONFICIUS_TEST_INHERITED", "kept")

	environment := managerEnvironment("")
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		name, value, _ := strings.Cut(entry, "=")
		values[name] = value
	}
	if values["NO_COLOR"] != "1" || values["CLICOLOR"] != "0" || values["LC_ALL"] != "C" {
		t.Fatalf("deterministic environment = %#v", values)
	}
	if values["CONFICIUS_TEST_INHERITED"] != "kept" {
		t.Fatalf("inherited environment dropped: %#v", values)
	}
}

func TestApplyUpdatesRecordsChangedProjectFiles(t *testing.T) {
	if os.Getenv("GO_WANT_CHANGED_FILES_HELPER") == "1" {
		if err := os.WriteFile(filepath.Join(os.Getenv("PROJECT_DIR"), "package-lock.json"), []byte("after"), 0o600); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package-lock.json"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_WANT_CHANGED_FILES_HELPER", "1")
	t.Setenv("PROJECT_DIR", project)
	plans := []UpdatePlan{{Manager: PM_NPM, Scope: "project", Directory: project, StateHash: "plan", Executable: os.Args[0], Args: []string{"-test.run=TestApplyUpdatesRecordsChangedProjectFiles"}}}
	outcomes, err := ApplyUpdatesWithOutcomes(context.Background(), plans, false, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil || len(outcomes) != 1 || !reflect.DeepEqual(outcomes[0].ChangedFiles, []string{"package-lock.json"}) {
		t.Fatalf("outcomes = %#v; error = %v", outcomes, err)
	}
}

func TestApplyUpdatesTimesOutPlanAndSkipsLaterWork(t *testing.T) {
	if os.Getenv("GO_WANT_TIMEOUT_HELPER") == "1" {
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	originalTimeout := mutationTimeout
	mutationTimeout = 20 * time.Millisecond
	t.Cleanup(func() { mutationTimeout = originalTimeout })
	t.Setenv("GO_WANT_TIMEOUT_HELPER", "1")
	plans := []UpdatePlan{
		{Manager: PM_NPM, StateHash: "timed-out", Executable: os.Args[0], Args: []string{"-test.run=TestApplyUpdatesTimesOutPlanAndSkipsLaterWork"}},
		{Manager: PM_BREW, StateHash: "skipped", Executable: "/must/not/run"},
	}
	outcomes, err := ApplyUpdatesWithOutcomes(context.Background(), plans, false, nil, &bytes.Buffer{}, &bytes.Buffer{})
	var operationErr *OperationError
	if !errors.As(err, &operationErr) || operationErr.Category != ErrorTimeout || len(outcomes) != 2 || outcomes[1].Status != "skipped" {
		t.Fatalf("outcomes = %#v; error = %#v", outcomes, err)
	}
}

func TestApplyUpdatesStopsBeforeLaunchingCancelledWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plans := []UpdatePlan{{Manager: PM_NPM, Executable: "/must/not/run", Args: []string{"update"}}}
	err := ApplyUpdates(ctx, plans, nil, &bytes.Buffer{}, &bytes.Buffer{})
	var operationErr *OperationError
	if !errors.As(err, &operationErr) || operationErr.Category != ErrorCancelled {
		t.Fatalf("error = %#v", err)
	}
}

func TestApplyUpdatesWithOutcomesRecordsCompletedAndFailedPlans(t *testing.T) {
	if os.Getenv("GO_WANT_OUTCOME_HELPER") == "1" {
		if os.Args[len(os.Args)-1] == "fail" {
			os.Exit(3)
		}
		os.Exit(0)
	}
	t.Setenv("GO_WANT_OUTCOME_HELPER", "1")
	plans := []UpdatePlan{
		{Manager: PM_NPM, StateHash: "failed", Executable: os.Args[0], Args: []string{"-test.run=TestApplyUpdatesWithOutcomesRecordsCompletedAndFailedPlans", "--", "fail"}},
		{Manager: PM_BREW, StateHash: "completed", Executable: os.Args[0], Args: []string{"-test.run=TestApplyUpdatesWithOutcomesRecordsCompletedAndFailedPlans", "--", "ok"}},
	}
	outcomes, err := ApplyUpdatesWithOutcomes(context.Background(), plans, true, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("updates succeeded; want first plan failure")
	}
	want := []UpdateOutcome{{StateHash: "failed", Status: "failed", Error: outcomes[0].Error}, {StateHash: "completed", Status: "completed"}}
	if outcomes[0].Error == "" || !reflect.DeepEqual(outcomes, want) {
		t.Fatalf("outcomes = %#v; want %#v", outcomes, want)
	}
}

func TestApplyUpdatesWithOutcomesBestEffortStopsFailedManagerGroup(t *testing.T) {
	if os.Getenv("GO_WANT_GROUP_HELPER") == "1" {
		if os.Args[len(os.Args)-1] == "fail" {
			os.Exit(3)
		}
		os.Exit(0)
	}
	t.Setenv("GO_WANT_GROUP_HELPER", "1")
	plans := []UpdatePlan{
		{Manager: PM_NPM, StateHash: "npm-failed", Executable: os.Args[0], Args: []string{"-test.run=TestApplyUpdatesWithOutcomesBestEffortStopsFailedManagerGroup", "--", "fail"}},
		{Manager: PM_NPM, StateHash: "npm-skipped", Executable: "/must/not/run"},
		{Manager: PM_BREW, StateHash: "brew-completed", Executable: os.Args[0], Args: []string{"-test.run=TestApplyUpdatesWithOutcomesBestEffortStopsFailedManagerGroup", "--", "ok"}},
	}
	outcomes, err := ApplyUpdatesWithOutcomes(context.Background(), plans, true, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || len(outcomes) != 3 || outcomes[0].Status != "failed" || outcomes[1].Status != "skipped" || outcomes[2].Status != "completed" {
		t.Fatalf("outcomes = %#v; error = %v", outcomes, err)
	}
}

func TestApplyUpdatesWithOutcomesStopsAfterFailureByDefault(t *testing.T) {
	if os.Getenv("GO_WANT_FAIL_FAST_HELPER") == "1" {
		os.Exit(3)
	}
	t.Setenv("GO_WANT_FAIL_FAST_HELPER", "1")
	plans := []UpdatePlan{
		{Manager: PM_NPM, StateHash: "failed", Executable: os.Args[0], Args: []string{"-test.run=TestApplyUpdatesWithOutcomesStopsAfterFailureByDefault"}},
		{Manager: PM_NPM, StateHash: "skipped", Executable: "/must/not/run"},
	}
	outcomes, err := ApplyUpdatesWithOutcomes(context.Background(), plans, false, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || len(outcomes) != 2 || outcomes[0].Status != "failed" || outcomes[1].Status != "skipped" {
		t.Fatalf("outcomes = %#v; error = %v", outcomes, err)
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

	_, err := ApplyUpdatesWithOutcomes(context.Background(), plans, true, bytes.NewBufferString("input"), &bytes.Buffer{}, &bytes.Buffer{})
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

func TestValidateProjectMutationsKeepsSafetyGuards(t *testing.T) {
	project := t.TempDir()
	plans := []UpdatePlan{{Scope: "project", Directory: project}}
	for _, test := range []struct {
		name                         string
		manifest, dirty, unversioned bool
		git                          string
		want                         string
	}{
		{name: "manifest denied", unversioned: true, want: "does not allow manifest changes"},
		{name: "unversioned denied", manifest: true, git: "exit 1", want: "not in a Git repository"},
		{name: "unversioned allowed", manifest: true, unversioned: true, git: "exit 1"},
		{name: "dirty denied", manifest: true, git: "printf 'changed'", want: "has uncommitted changes"},
		{name: "dirty allowed", manifest: true, dirty: true, git: "printf 'changed'"},
		{name: "clean allowed", manifest: true, git: "exit 0"},
		{name: "status failure denied", manifest: true, dirty: true, unversioned: true, git: "if [ \"$3\" = status ]; then exit 1; fi", want: "inspect Git status"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeExecutable(t, filepath.Join(dir, "git"), "#!/bin/sh\n"+test.git+"\n")
			t.Setenv("PATH", dir)
			profile := config.Profile{Projects: []config.ProfileProject{{Name: "app", Path: project, ResolvedPath: project, AllowManifestChanges: test.manifest}}}
			err := ValidateProjectMutations(plans, profile, test.dirty, test.unversioned)
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v; want %q", err, test.want)
			}
		})
	}
}
