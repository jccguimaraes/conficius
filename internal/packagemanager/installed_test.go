package packagemanager

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestManagerPackagesReadsManagerVersionsAndOSVIdentities(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "mise"), "#!/bin/sh\nprintf '2026.9.6 macos-arm64\n'\n")
	writeExecutable(t, filepath.Join(dir, "brew"), "#!/bin/sh\nprintf 'Homebrew 7.0.1\n'\n")
	writeExecutable(t, filepath.Join(dir, "npm"), "#!/bin/sh\nprintf '11.19.1\n'\n")
	statuses := []ManagerStatus{
		{Name: PM_MISE, Status: StatusAvailable, Executables: []string{filepath.Join(dir, "mise")}},
		{Name: PM_BREW, Status: StatusAvailable, Executables: []string{filepath.Join(dir, "brew")}},
		{Name: PM_NPM, Status: StatusAvailable, Executables: []string{filepath.Join(dir, "npm")}},
	}
	packages, err := ManagerPackages(context.Background(), statuses)
	if err != nil {
		t.Fatal(err)
	}
	want := []InstalledPackage{
		{Manager: "manager", Name: PM_MISE, Version: "2026.9.6", Scope: "global", OSVEcosystem: "crates.io", OSVName: "mise"},
		{Manager: "manager", Name: PM_BREW, Version: "7.0.1", Scope: "global"},
		{Manager: "manager", Name: PM_NPM, Version: "11.19.1", Scope: "global", OSVEcosystem: "npm", OSVName: "npm"},
	}
	if !reflect.DeepEqual(packages, want) {
		t.Fatalf("packages = %#v; want %#v", packages, want)
	}
}

func TestNPMManagerUpdateQueriesRegistryWhenUnmanaged(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	writeExecutable(t, filepath.Join(dir, "npm"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >\"$ARGS_FILE\"\nprintf '11.20.0\\n'\n")
	t.Setenv("ARGS_FILE", argsFile)
	status := ManagerStatus{Name: PM_NPM, Status: StatusAvailable, Executables: []string{filepath.Join(dir, "npm")}}
	updates, err := ManagerUpdates(context.Background(), []ManagerStatus{status}, []InstalledPackage{{Manager: "manager", Name: PM_NPM, Version: "11.19.1", Scope: "global"}})
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(args)); got != "view npm version" {
		t.Fatalf("args = %q", got)
	}
	records := Records(updates)
	if len(records) != 1 || records[0].Latest != "11.20.0" {
		t.Fatalf("updates = %#v", records)
	}
}

func TestNPMManagerUpdateSkipsMiseOwnedExecutable(t *testing.T) {
	dir := t.TempDir()
	npm := filepath.Join(dir, "npm")
	writeExecutable(t, npm, "#!/bin/sh\nprintf '11.20.0\\n'\n")
	writeExecutable(t, filepath.Join(dir, "mise"), "#!/bin/sh\nprintf '%s\\n' \"$NPM_PATH\"\n")
	t.Setenv("NPM_PATH", npm)
	statuses := []ManagerStatus{
		{Name: PM_MISE, Status: StatusAvailable, Executables: []string{filepath.Join(dir, "mise")}},
		{Name: PM_NPM, Status: StatusAvailable, Executables: []string{npm}},
	}
	updates, err := ManagerUpdates(context.Background(), statuses, []InstalledPackage{{Manager: "manager", Name: PM_NPM, Version: "11.19.1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(Records(updates)) != 0 {
		t.Fatalf("updates = %#v", Records(updates))
	}
}

func TestQueryInstalledReadsSupportedManagerInventories(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
printf '{"dependencies":{"left-pad":{"version":"1.3.0"},"missing":{}}}'
`)
	writeExecutable(t, filepath.Join(dir, "mise"), `#!/bin/sh
[ "$*" = "ls --current --installed --json" ] || { printf 'unexpected args: %s' "$*" >&2; exit 1; }
printf '{"node":[{"version":"22.1.0","installed":true},{"version":"20","installed":false}]}'
`)
	writeExecutable(t, filepath.Join(dir, "brew"), `#!/bin/sh
printf '{"formulae":[{"name":"wget","installed":[{"version":"1.2"}]}],"casks":[{"token":"app","installed":"3.0"}]}'
`)
	t.Setenv("PATH", dir)

	result, err := QueryInstalled(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []InstalledPackage{
		{Manager: PM_MISE, Name: "node", Version: "22.1.0"},
		{Manager: PM_BREW, Name: "app", Version: "3.0"},
		{Manager: PM_BREW, Name: "wget", Version: "1.2"},
		{Manager: PM_NPM, Name: "left-pad", Version: "1.3.0"},
	}
	if !reflect.DeepEqual(result.Packages, want) {
		t.Fatalf("packages = %#v; want %#v", result.Packages, want)
	}
}

func TestQueryInstalledScopesUsesOnlyProjectMiseTools(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "mise"), `#!/bin/sh
[ "$PWD" = "$PROJECT" ] || { printf 'wrong directory: %s' "$PWD" >&2; exit 1; }
[ "$*" = "ls --current --installed --json --local" ] || { printf 'unexpected args: %s' "$*" >&2; exit 1; }
printf '{"node":[{"version":"22.1.0","installed":true}]}'
`)
	t.Setenv("PATH", dir)
	t.Setenv("PROJECT", project)

	result, err := QueryInstalledScopes(context.Background(), []string{PM_MISE}, []Scope{{Kind: "project", Path: project}})
	if err != nil {
		t.Fatal(err)
	}
	want := []InstalledPackage{{Manager: PM_MISE, Name: "node", Version: "22.1.0", Scope: "project", Path: project}}
	if !reflect.DeepEqual(result.Packages, want) {
		t.Fatalf("packages = %#v; want %#v", result.Packages, want)
	}
}

func TestQueryInstalledScopesRunsProjectNPMInProjectDirectory(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
printf '{"dependencies":{"project-package":{"version":"2.0.0"}}}'
`)
	t.Setenv("PATH", dir)

	result, err := QueryInstalledScopes(context.Background(), []string{PM_NPM}, []Scope{{Kind: "project", Path: project}})
	if err != nil {
		t.Fatal(err)
	}
	want := []InstalledPackage{{Manager: PM_NPM, Name: "project-package", Version: "2.0.0", Scope: "project", Path: project}}
	if !reflect.DeepEqual(result.Packages, want) {
		t.Fatalf("packages = %#v; want %#v", result.Packages, want)
	}
}

func TestQueryInstalledContinuesPastUnavailableManager(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "npm"), "#!/bin/sh\nprintf '{\"dependencies\":{}}'\n")
	t.Setenv("PATH", dir)

	result, err := QueryInstalled(context.Background(), []string{PM_MISE, PM_NPM})
	if err != nil {
		t.Fatal(err)
	}
	if result.Managers[0].Status != StatusUnavailable || result.Managers[1].Status != StatusAvailable {
		t.Fatalf("statuses = %#v", result.Managers)
	}
}

func TestQueryInstalledReportsPartialManagerAndPreservesSuccessfulScope(t *testing.T) {
	dir := t.TempDir()
	goodProject := t.TempDir()
	badProject := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
if [ "$PWD" = "$BAD_PROJECT" ]; then
  printf 'registry unavailable' >&2
  exit 1
fi
printf '{"dependencies":{"left-pad":{"version":"1.3.0"}}}'
`)
	t.Setenv("PATH", dir)
	t.Setenv("BAD_PROJECT", badProject)

	result, err := QueryInstalledScopes(context.Background(), []string{PM_NPM}, []Scope{{Kind: "project", Path: goodProject}, {Kind: "project", Path: badProject}})
	if err == nil {
		t.Fatal("query succeeded; want partial error")
	}
	status := result.Managers[0]
	if status.Status != StatusPartial || len(status.Operations) != 2 || status.Operations[1].Category != ErrorRegistry {
		t.Fatalf("status = %#v", status)
	}
	want := []InstalledPackage{{Manager: PM_NPM, Name: "left-pad", Version: "1.3.0", Scope: "project", Path: goodProject}}
	if !reflect.DeepEqual(result.Packages, want) {
		t.Fatalf("packages = %#v; want %#v", result.Packages, want)
	}
}

func TestQueryInstalledReportsMalformedInventory(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "npm"), "#!/bin/sh\nprintf broken\n")
	t.Setenv("PATH", dir)

	result, err := QueryInstalled(context.Background(), []string{PM_NPM})
	if err == nil {
		t.Fatal("query succeeded; want decode error")
	}
	if result.Managers[0].Status != StatusError {
		t.Fatalf("status = %#v", result.Managers[0])
	}
	if got := result.Managers[0].Operations[0].Category; got != ErrorParser {
		t.Fatalf("category = %q; want %q", got, ErrorParser)
	}
}
