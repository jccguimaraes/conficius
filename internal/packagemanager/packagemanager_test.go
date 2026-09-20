package packagemanager

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jccguimaraes/conficius/internal/config"
)

func TestInspectWithOptionsReportsDisabledManagerAndExecutableEvidence(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, PM_NPM)
	writeExecutable(t, executable, "#!/bin/sh\nprintf '11.0.0'\n")
	t.Setenv("PATH", dir)
	statuses, err := inspectWithOptions([]string{PM_NPM}, QueryOptions{Disabled: map[string]bool{PM_NPM: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Status != StatusDisabled || !reflect.DeepEqual(statuses[0].Executables, []string{executable}) {
		t.Fatalf("statuses = %#v", statuses)
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
	options := QueryOptionsFromProfile(config.Profile{Managers: map[string]config.ManagerSettings{
		"npm":  {Enabled: &disabled, Executable: "/tools/npm", ExpectedGlobalPrefix: "/prefix"},
		"mise": {Enabled: &enabled},
		"brew": {IncludeGreedy: true},
	}})
	if !options.Disabled["npm"] || !options.Required["mise"] || options.Executables["npm"] != "/tools/npm" {
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
	if len(records) != 1 || records[0].Path != goodProject {
		t.Fatalf("packages = %#v", records)
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
