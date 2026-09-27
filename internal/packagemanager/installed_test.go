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
		{Manager: "manager", Name: PM_MISE, Version: "2026.9.6", Role: RolePackageManager, Scope: "global", OSVEcosystem: "crates.io", OSVName: "mise"},
		{Manager: "manager", Name: PM_BREW, Version: "7.0.1", Role: RolePackageManager, Scope: "global"},
		{Manager: "manager", Name: PM_NPM, Version: "11.19.1", Role: RolePackageManager, Scope: "global", OSVEcosystem: "npm", OSVName: "npm"},
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
		{Manager: PM_MISE, Name: "node", Version: "22.1.0", Role: RoleRuntime},
		{Manager: PM_BREW, Name: "app", Version: "3.0", Role: RoleApplication},
		{Manager: PM_BREW, Name: "wget", Version: "1.2", Role: RoleLibrary},
		{Manager: PM_NPM, Name: "left-pad", Version: "1.3.0", Role: RoleLibrary},
	}
	if !reflect.DeepEqual(result.Packages, want) {
		t.Fatalf("packages = %#v; want %#v", result.Packages, want)
	}
}

func TestQueryInstalledReportsUnsupportedProjectScope(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "brew"), "#!/bin/sh\nexit 99\n")
	t.Setenv("PATH", dir)

	result, err := QueryInstalledScopes(context.Background(), []string{PM_BREW}, []Scope{{Kind: "project", Path: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	status := result.Managers[0]
	if status.Status != StatusAvailable || len(status.Operations) != 1 || status.Operations[0].Status != StatusUnsupported {
		t.Fatalf("status = %#v", status)
	}
}

func TestQueryInstalledScopesUsesOnlyProjectMiseTools(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "mise.toml"), []byte("[tools]\nnode = '22'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
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
	want := []InstalledPackage{{Manager: PM_MISE, Name: "node", Version: "22.1.0", Role: RoleRuntime, Scope: "project", Path: project, Config: filepath.Join(project, "mise.toml")}}
	if !reflect.DeepEqual(result.Packages, want) {
		t.Fatalf("packages = %#v; want %#v", result.Packages, want)
	}
}

func TestQueryInstalledIncludesNPMTransitivePackagesWhenRequested(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{"devDependencies":{"parent":"1.0.0"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
printf '{"dependencies":{"parent":{"version":"1.0.0","dependencies":{"child":{"version":"2.0.0"}}}}}'
`)
	t.Setenv("PATH", dir)
	result, err := QueryInstalledScopesWithOptions(context.Background(), []string{PM_NPM}, []Scope{{Kind: "project", Path: project}}, QueryOptions{IncludeTransitive: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []InstalledPackage{
		{Manager: PM_NPM, Name: "child", Version: "2.0.0", Role: RoleLibrary, Scope: "project", Path: project, Manifest: filepath.Join(project, "package.json"), DependencyClass: "transitive", Transitive: true},
		{Manager: PM_NPM, Name: "parent", Version: "1.0.0", Role: RoleDevelopmentTool, Scope: "project", Path: project, Manifest: filepath.Join(project, "package.json"), DependencyClass: "development"},
	}
	if !reflect.DeepEqual(result.Packages, want) {
		t.Fatalf("packages = %#v; want %#v", result.Packages, want)
	}
}

func TestQueryInstalledTransitiveFlagOverridesConfiguredClassFilter(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{"dependencies":{"parent":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(dir, "npm"), "#!/bin/sh\nprintf '{\"dependencies\":{\"parent\":{\"version\":\"1\",\"dependencies\":{\"child\":{\"version\":\"2\"}}}}}'\n")
	t.Setenv("PATH", dir)
	result, err := QueryInstalledScopesWithOptions(context.Background(), []string{PM_NPM}, []Scope{{Kind: "project", Path: project}}, QueryOptions{IncludeTransitive: true, DependencyClasses: map[string]map[string]bool{project: {"runtime": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Packages) != 2 {
		t.Fatalf("packages = %#v", result.Packages)
	}
}

func TestQueryInstalledIncludesTransitiveNPMClassFromConfiguration(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{"dependencies":{"parent":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(dir, "npm"), "#!/bin/sh\nprintf '{\"dependencies\":{\"parent\":{\"version\":\"1\",\"dependencies\":{\"child\":{\"version\":\"2\"}}}}}'\n")
	t.Setenv("PATH", dir)
	result, err := QueryInstalledScopesWithOptions(context.Background(), []string{PM_NPM}, []Scope{{Kind: "project", Path: project}}, QueryOptions{DependencyClasses: map[string]map[string]bool{project: {"transitive": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Packages) != 1 || result.Packages[0].Name != "child" || !result.Packages[0].Transitive {
		t.Fatalf("packages = %#v", result.Packages)
	}
}

func TestPackageIdentityCanonicalIDIncludesManager(t *testing.T) {
	npm := PackageIdentity{Manager: PM_NPM, Name: "typescript"}
	brew := PackageIdentity{Manager: PM_BREW, Name: "typescript"}
	if npm.CanonicalID() != "npm:typescript" || npm == brew || npm.CanonicalID() == brew.CanonicalID() {
		t.Fatalf("npm = %#v, brew = %#v", npm, brew)
	}
}

func TestExposedCommandKeepsOwnerIdentity(t *testing.T) {
	command := ExposedCommand{Package: PackageIdentity{Manager: PM_NPM, Name: "typescript"}, Name: "tsc"}
	if command.Package.CanonicalID() != "npm:typescript" || command.Name != "tsc" {
		t.Fatalf("command = %#v", command)
	}
}

func TestCollisionCandidatesOnlyReportsCompetingGlobalOwners(t *testing.T) {
	commands := []ExposedCommand{
		{Package: PackageIdentity{Manager: PM_NPM, Name: "typescript"}, Name: "tsc", Scope: "global"},
		{Package: PackageIdentity{Manager: PM_BREW, Name: "typescript"}, Name: "tsc", Scope: "global"},
		{Package: PackageIdentity{Manager: PM_NPM, Name: "typescript"}, Name: "tsc", Scope: "project", Path: "/repo"},
		{Package: PackageIdentity{Manager: PM_NPM, Name: "typescript"}, Name: "tsserver", Scope: "global"},
	}
	want := []CollisionCandidate{{Command: "tsc", Owners: []PackageIdentity{{Manager: PM_BREW, Name: "typescript"}, {Manager: PM_NPM, Name: "typescript"}}}}
	if got := CollisionCandidates(commands); !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v; want %#v", got, want)
	}
}

func TestResolveCollisionCandidatesOnlyLooksUpCandidates(t *testing.T) {
	var lookedUp []string
	candidates := []CollisionCandidate{{Command: "node", Owners: []PackageIdentity{{Manager: PM_BREW, Name: "node"}, {Manager: PM_MISE, Name: "node"}}}}
	resolved := ResolveCollisionCandidates(candidates, func(command string) (string, error) {
		lookedUp = append(lookedUp, command)
		return "/usr/local/bin/" + command, nil
	})
	if !reflect.DeepEqual(lookedUp, []string{"node"}) || len(resolved) != 1 || resolved[0].Path != "/usr/local/bin/node" || candidates[0].Path != "" {
		t.Fatalf("lookedUp = %#v, resolved = %#v, candidates = %#v", lookedUp, resolved, candidates)
	}
}

func TestResolveCollisionCandidatesKeepsUnknownPath(t *testing.T) {
	candidates := []CollisionCandidate{{Command: "missing"}}
	resolved := ResolveCollisionCandidates(candidates, func(string) (string, error) { return "", os.ErrNotExist })
	if len(resolved) != 1 || resolved[0].Path != "" {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestCollisionCandidatesDoesNotTreatLocalAndGlobalCopiesAsDuplicates(t *testing.T) {
	owner := PackageIdentity{Manager: PM_NPM, Name: "typescript"}
	commands := []ExposedCommand{{Package: owner, Name: "tsc", Scope: "global"}, {Package: owner, Name: "tsc", Scope: "project", Path: "/repo"}}
	if got := CollisionCandidates(commands); len(got) != 0 {
		t.Fatalf("candidates = %#v", got)
	}
}

func TestQueryInstalledFiltersConfiguredNPMDependencyClasses(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{"dependencies":{"react":"1"},"devDependencies":{"typescript":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(dir, "npm"), "#!/bin/sh\nprintf '{\"dependencies\":{\"react\":{\"version\":\"1\"},\"typescript\":{\"version\":\"1\"}}}'\n")
	t.Setenv("PATH", dir)
	result, err := QueryInstalledScopesWithOptions(context.Background(), []string{PM_NPM}, []Scope{{Kind: "project", Path: project}}, QueryOptions{DependencyClasses: map[string]map[string]bool{project: {"development": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Packages) != 1 || result.Packages[0].Name != "typescript" {
		t.Fatalf("packages = %#v", result.Packages)
	}
}

func TestNPMDependencyClassesUseDeterministicPrecedence(t *testing.T) {
	project := t.TempDir()
	contents := `{"dependencies":{"runtime":"1","shared":"1"},"peerDependencies":{"peer":"1","shared":"1"},"optionalDependencies":{"optional":"1","shared":"1"},"devDependencies":{"development":"1","shared":"1"}}`
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	classes, err := npmDependencyClasses(Scope{Kind: "project", Path: project})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"runtime": "runtime", "peer": "peer", "optional": "optional", "development": "development", "shared": "development"}
	if !reflect.DeepEqual(classes, want) {
		t.Fatalf("classes = %#v; want %#v", classes, want)
	}
}

func TestQueryInstalledClassifiesUnlistedNPMDependencyAsExtraneous(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(dir, "npm"), "#!/bin/sh\nprintf '{\"dependencies\":{\"left-pad\":{\"version\":\"1.3.0\"}}}'\n")
	t.Setenv("PATH", dir)
	result, err := QueryInstalledScopes(context.Background(), []string{PM_NPM}, []Scope{{Kind: "project", Path: project}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Packages) != 1 || result.Packages[0].DependencyClass != "extraneous" {
		t.Fatalf("packages = %#v", result.Packages)
	}
}

func TestQueryInstalledClassifiesNPMDevelopmentDependencies(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{"dependencies":{"react":"^19.0.0"},"devDependencies":{"typescript":"^5.0.0"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
printf '{"dependencies":{"react":{"version":"19.0.0"},"typescript":{"version":"5.0.0"}}}'
`)
	t.Setenv("PATH", dir)
	result, err := QueryInstalledScopes(context.Background(), []string{PM_NPM}, []Scope{{Kind: "project", Path: project}})
	if err != nil {
		t.Fatal(err)
	}
	want := []InstalledPackage{{Manager: PM_NPM, Name: "react", Version: "19.0.0", Role: RoleLibrary, Scope: "project", Path: project, Manifest: filepath.Join(project, "package.json"), DependencyClass: "runtime"}, {Manager: PM_NPM, Name: "typescript", Version: "5.0.0", Role: RoleDevelopmentTool, Scope: "project", Path: project, Manifest: filepath.Join(project, "package.json"), DependencyClass: "development"}}
	if !reflect.DeepEqual(result.Packages, want) {
		t.Fatalf("packages = %#v; want %#v", result.Packages, want)
	}
}

func TestQueryInstalledPreservesWorkspaceScope(t *testing.T) {
	dir := t.TempDir()
	workspace := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
printf '{"dependencies":{"workspace-package":{"version":"1.0.0"}}}'
`)
	t.Setenv("PATH", dir)
	result, err := QueryInstalledScopes(context.Background(), []string{PM_NPM}, []Scope{{Kind: "workspace", Path: workspace}})
	if err != nil {
		t.Fatal(err)
	}
	want := []InstalledPackage{{Manager: PM_NPM, Name: "workspace-package", Version: "1.0.0", Role: RoleLibrary, Scope: "workspace", Path: workspace, Manifest: filepath.Join(workspace, "package.json"), DependencyClass: "extraneous"}}
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
	want := []InstalledPackage{{Manager: PM_NPM, Name: "project-package", Version: "2.0.0", Role: RoleLibrary, Scope: "project", Path: project, Manifest: filepath.Join(project, "package.json"), DependencyClass: "extraneous"}}
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
	want := []InstalledPackage{{Manager: PM_NPM, Name: "left-pad", Version: "1.3.0", Role: RoleLibrary, Scope: "project", Path: goodProject, Manifest: filepath.Join(goodProject, "package.json"), DependencyClass: "extraneous"}}
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
