package packagemanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type PackageRole string

const (
	RoleRuntime         PackageRole = "runtime"
	RoleLibrary         PackageRole = "library"
	RoleDevelopmentTool PackageRole = "development-tool"
	RoleApplication     PackageRole = "application"
	RolePackageManager  PackageRole = "package-manager"
	RolePlugin          PackageRole = "plugin"
)

type InstalledPackage struct {
	Manager         string      `json:"manager"`
	Name            string      `json:"name"`
	Version         string      `json:"version"`
	Role            PackageRole `json:"role"`
	Scope           string      `json:"scope,omitempty"`
	Path            string      `json:"path,omitempty"`
	Workspace       string      `json:"workspace,omitempty"`
	Manifest        string      `json:"manifest,omitempty"`
	Config          string      `json:"config,omitempty"`
	DependencyClass string      `json:"dependency_class,omitempty"`
	OSVEcosystem    string      `json:"osv_ecosystem,omitempty"`
	OSVName         string      `json:"osv_name,omitempty"`
	Transitive      bool        `json:"transitive,omitempty"`
}

type Scope struct {
	Kind string
	Path string
}

type InstalledResult struct {
	Managers []ManagerStatus    `json:"managers"`
	Packages []InstalledPackage `json:"packages"`
}

func QueryInstalled(ctx context.Context, pms []string) (InstalledResult, error) {
	result, err := QueryInstalledScopes(ctx, pms, []Scope{{Kind: "global"}})
	for i := range result.Packages {
		result.Packages[i].Scope = ""
	}
	return result, err
}

func QueryInstalledScopes(ctx context.Context, pms []string, scopes []Scope) (InstalledResult, error) {
	return QueryInstalledScopesWithOptions(ctx, pms, scopes, QueryOptions{})
}

func QueryInstalledScopesWithOptions(ctx context.Context, pms []string, scopes []Scope, options QueryOptions) (InstalledResult, error) {
	var err error
	scopes, err = normalizeScopes(scopes)
	if err != nil {
		return InstalledResult{}, err
	}
	options, err = requireManifestManagers(options, scopes)
	if err != nil {
		return InstalledResult{}, err
	}
	statuses, err := inspectWithOptions(pms, options)
	if err != nil {
		return InstalledResult{}, err
	}
	result := InstalledResult{Managers: statuses}
	var operations []readOperation[[]InstalledPackage]
	for i := range result.Managers {
		status := &result.Managers[i]
		if status.Status != StatusAvailable {
			continue
		}
		adapter, adapterErr := adapterFor(status.Name)
		if adapterErr != nil {
			return InstalledResult{}, adapterErr
		}
		for _, executable := range status.Executables {
			for _, scope := range scopes {
				if !hasCapability(adapter, scopeCapability(scope, CapabilityGlobalInventory, CapabilityProjectInventory)) {
					status.Operations = append(status.Operations, unsupportedOperationStatus("inventory", executable, scope))
					continue
				}
				manager, executable, scope := status.Name, executable, scope
				operations = append(operations, readOperation[[]InstalledPackage]{managerIndex: i, manager: manager, executable: executable, scope: scope, run: func(ctx context.Context) ([]InstalledPackage, error) {
					return listInstalledScope(ctx, manager, executable, scope, options)
				}})
			}
		}
	}
	inventoryErrors := requiredManagerErrors(result.Managers)
	for _, operationResult := range runReadOperations(ctx, operations) {
		status := &result.Managers[operationResult.operation.managerIndex]
		status.Operations = append(status.Operations, newOperationStatus("inventory", operationResult.operation, operationResult.err))
		if operationResult.err != nil {
			status.Error = redactSensitive(operationResult.err.Error())
			inventoryErrors = append(inventoryErrors, fmt.Errorf("%s (%s, %s): %w", operationResult.operation.manager, operationResult.operation.executable, operationResult.operation.scope.Kind, operationResult.err))
			continue
		}
		for _, pkg := range operationResult.value {
			classes := options.DependencyClasses[pkg.Path]
			if pkg.Manager == PM_NPM && len(classes) > 0 && !classes[pkg.DependencyClass] {
				continue
			}
			result.Packages = append(result.Packages, pkg)
		}
	}
	finalizeManagerStatuses(result.Managers)
	result.Packages = dedupeInstalled(result.Packages)
	return result, errors.Join(inventoryErrors...)
}

func listInstalledScope(ctx context.Context, manager, executable string, scope Scope, options QueryOptions) ([]InstalledPackage, error) {
	adapter, err := adapterFor(manager)
	if err != nil {
		return nil, err
	}
	if manager == PM_NPM && options.IncludeTransitive {
		packages, err := installedNPMTransitive(ctx, executable, scope)
		for i := range packages {
			packages[i].Scope, packages[i].Path = scope.Kind, scope.Path
			if scope.Kind == "project" || scope.Kind == "workspace" {
				packages[i].Manifest = filepath.Join(scope.Path, "package.json")
			}
		}
		return packages, err
	}
	packages, err := adapter.listInstalled(ctx, executable, scope)
	for i := range packages {
		packages[i].Scope = scope.Kind
		packages[i].Path = scope.Path
		if manager == PM_NPM && (scope.Kind == "project" || scope.Kind == "workspace") {
			packages[i].Manifest = filepath.Join(scope.Path, "package.json")
		}
		if manager == PM_MISE && (scope.Kind == "project" || scope.Kind == "workspace") {
			packages[i].Config = miseConfigPath(scope.Path)
		}
	}
	return packages, err
}

func miseConfigPath(root string) string {
	for _, name := range []string{"mise.toml", ".mise.toml"} {
		path := filepath.Join(root, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func installedNPM(ctx context.Context, executable string, scope Scope) ([]InstalledPackage, error) {
	args := []string{"ls", "--depth=0", NPMFlagJSON}
	if scope.Kind == "global" {
		args = []string{"ls", NPMFlagGlobal, "--depth=0", NPMFlagJSON}
	}
	output, err := runOutputDir(ctx, scope.Path, executable, args...)
	if err != nil {
		return nil, err
	}
	var document struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(output, &document); err != nil {
		return nil, parserError(fmt.Errorf("decode npm inventory: %w", err))
	}
	classes, err := npmDependencyClasses(scope)
	if err != nil {
		return nil, err
	}
	packages := make([]InstalledPackage, 0, len(document.Dependencies))
	for name, dependency := range document.Dependencies {
		if dependency.Version != "" {
			role := RoleLibrary
			if classes[name] == "development" {
				role = RoleDevelopmentTool
			}
			packages = append(packages, InstalledPackage{Manager: PM_NPM, Name: name, Version: dependency.Version, Role: role, DependencyClass: classes[name]})
		}
	}
	return packages, nil
}

func npmDependencyClasses(scope Scope) (map[string]string, error) {
	result := make(map[string]string)
	if scope.Kind != "project" && scope.Kind != "workspace" {
		return result, nil
	}
	contents, err := os.ReadFile(filepath.Join(scope.Path, "package.json"))
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read npm manifest: %w", err)
	}
	var manifest struct {
		Dependencies         map[string]string `json:"dependencies"`
		DevDependencies      map[string]string `json:"devDependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
		PeerDependencies     map[string]string `json:"peerDependencies"`
	}
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return nil, parserError(fmt.Errorf("decode npm manifest: %w", err))
	}
	for name := range manifest.Dependencies {
		result[name] = "runtime"
	}
	for name := range manifest.PeerDependencies {
		result[name] = "peer"
	}
	for name := range manifest.OptionalDependencies {
		result[name] = "optional"
	}
	for name := range manifest.DevDependencies {
		result[name] = "development"
	}
	return result, nil
}

func installedNPMTransitive(ctx context.Context, executable string, scope Scope) ([]InstalledPackage, error) {
	args := []string{"ls", "--all", NPMFlagJSON}
	if scope.Kind == "global" {
		args = []string{"ls", NPMFlagGlobal, "--all", NPMFlagJSON}
	}
	output, err := runOutputDir(ctx, scope.Path, executable, args...)
	if err != nil {
		return nil, err
	}
	var document struct {
		Dependencies map[string]npmDependency `json:"dependencies"`
	}
	if err := json.Unmarshal(output, &document); err != nil {
		return nil, parserError(fmt.Errorf("decode npm transitive inventory: %w", err))
	}
	classes, err := npmDependencyClasses(scope)
	if err != nil {
		return nil, err
	}
	var packages []InstalledPackage
	appendNPMDependencies(&packages, document.Dependencies, classes, false)
	return packages, nil
}

type npmDependency struct {
	Version      string                   `json:"version"`
	Dependencies map[string]npmDependency `json:"dependencies"`
}

func appendNPMDependencies(packages *[]InstalledPackage, dependencies map[string]npmDependency, classes map[string]string, transitive bool) {
	for name, dependency := range dependencies {
		if dependency.Version != "" {
			role := RoleLibrary
			if !transitive && classes[name] == "development" {
				role = RoleDevelopmentTool
			}
			dependencyClass := "transitive"
			if !transitive {
				dependencyClass = classes[name]
			}
			*packages = append(*packages, InstalledPackage{Manager: PM_NPM, Name: name, Version: dependency.Version, Role: role, DependencyClass: dependencyClass, Transitive: transitive})
		}
		appendNPMDependencies(packages, dependency.Dependencies, classes, true)
	}
}

func installedMise(ctx context.Context, executable, dir string) ([]InstalledPackage, error) {
	args := []string{"ls", "--current", "--installed", "--json"}
	if dir != "" {
		args = append(args, "--local")
	}
	output, err := runOutputDir(ctx, dir, executable, args...)
	if err != nil {
		return nil, err
	}
	var document map[string][]struct {
		Version   string `json:"version"`
		Installed bool   `json:"installed"`
	}
	if err := json.Unmarshal(output, &document); err != nil {
		return nil, parserError(fmt.Errorf("decode mise inventory: %w", err))
	}
	var packages []InstalledPackage
	for name, versions := range document {
		for _, version := range versions {
			if version.Installed && version.Version != "" {
				packages = append(packages, InstalledPackage{Manager: PM_MISE, Name: name, Version: version.Version, Role: RoleRuntime})
			}
		}
	}
	return packages, nil
}

func installedHomebrew(ctx context.Context, executable string) ([]InstalledPackage, error) {
	output, err := runOutput(ctx, executable, "info", HomebrewFlagJSONv2, HomebrewFlagInstalled)
	if err != nil {
		return nil, err
	}
	var document struct {
		Formulae []struct {
			Name      string `json:"name"`
			Installed []struct {
				Version string `json:"version"`
			} `json:"installed"`
		} `json:"formulae"`
		Casks []struct {
			Token     string `json:"token"`
			Installed string `json:"installed"`
		} `json:"casks"`
	}
	if err := json.Unmarshal(output, &document); err != nil {
		return nil, parserError(fmt.Errorf("decode Homebrew inventory: %w", err))
	}
	var packages []InstalledPackage
	for _, formula := range document.Formulae {
		for _, installed := range formula.Installed {
			if installed.Version != "" {
				packages = append(packages, InstalledPackage{Manager: PM_BREW, Name: formula.Name, Version: installed.Version, Role: RoleLibrary})
			}
		}
	}
	for _, cask := range document.Casks {
		if cask.Installed != "" {
			packages = append(packages, InstalledPackage{Manager: PM_BREW, Name: cask.Token, Version: cask.Installed, Role: RoleApplication})
		}
	}
	return packages, nil
}

func ManagerPackages(ctx context.Context, statuses []ManagerStatus) ([]InstalledPackage, error) {
	var packages []InstalledPackage
	var versionErrors []error
	for _, status := range statuses {
		if status.Status != StatusAvailable || len(status.Executables) == 0 {
			continue
		}
		output, err := runOutput(ctx, status.Executables[0], "--version")
		if err != nil {
			versionErrors = append(versionErrors, fmt.Errorf("read %s version: %w", status.Name, err))
			continue
		}
		version := firstVersion(string(output))
		if version == "" {
			versionErrors = append(versionErrors, fmt.Errorf("read %s version: no version in output", status.Name))
			continue
		}
		pkg := InstalledPackage{Manager: "manager", Name: status.Name, Version: version, Role: RolePackageManager, Scope: "global"}
		switch status.Name {
		case PM_MISE:
			pkg.OSVEcosystem, pkg.OSVName = "crates.io", "mise"
		case PM_NPM:
			pkg.OSVEcosystem, pkg.OSVName = "npm", "npm"
		}
		packages = append(packages, pkg)
	}
	return packages, errors.Join(versionErrors...)
}

func ManagerUpdates(ctx context.Context, statuses []ManagerStatus, installed []InstalledPackage) (Packages, error) {
	updates := make(Packages)
	var queryErrors []error
	for _, pkg := range installed {
		if pkg.Manager != "manager" || pkg.Name != PM_NPM {
			continue
		}
		status, ok := managerStatus(statuses, PM_NPM)
		if !ok || len(status.Executables) == 0 || npmManagedByOtherTool(ctx, status.Executables[0], statuses) {
			continue
		}
		output, err := runOutput(ctx, status.Executables[0], "view", "npm", "version")
		if err != nil {
			queryErrors = append(queryErrors, fmt.Errorf("read latest npm version: %w", err))
			continue
		}
		latest := strings.TrimSpace(string(output))
		if latest == "" || latest == pkg.Version {
			continue
		}
		updates[PM_NPM] = append(updates[PM_NPM], scopedTool{Tool: NewNPMTool(PM_NPM, NPMOutdatedTool{Current: pkg.Version, Wanted: latest, Latest: latest}, status.Executables[0]), scope: Scope{Kind: "global"}})
	}
	return updates, errors.Join(queryErrors...)
}

func managerStatus(statuses []ManagerStatus, name string) (ManagerStatus, bool) {
	for _, status := range statuses {
		if status.Name == name && status.Status == StatusAvailable {
			return status, true
		}
	}
	return ManagerStatus{}, false
}

func npmManagedByOtherTool(ctx context.Context, npmExecutable string, statuses []ManagerStatus) bool {
	if status, ok := managerStatus(statuses, PM_MISE); ok && len(status.Executables) > 0 {
		if output, err := runOutput(ctx, status.Executables[0], "which", "npm"); err == nil && strings.TrimSpace(string(output)) == npmExecutable {
			return true
		}
	}
	if status, ok := managerStatus(statuses, PM_BREW); ok && len(status.Executables) > 0 {
		if output, err := runOutput(ctx, status.Executables[0], "which-formula", npmExecutable); err == nil && strings.TrimSpace(string(output)) != "" {
			return true
		}
	}
	return false
}

func versionInRange(version string, tested VersionRange) bool {
	majorText, _, _ := strings.Cut(version, ".")
	major, err := strconv.Atoi(majorText)
	return err == nil && major >= tested.MinimumMajor && major <= tested.MaximumMajor
}

func firstVersion(output string) string {
	for _, field := range strings.Fields(output) {
		candidate := strings.TrimPrefix(field, "v")
		if candidate != "" && candidate[0] >= '0' && candidate[0] <= '9' && strings.Contains(candidate, ".") {
			return strings.TrimRight(candidate, ",;")
		}
	}
	return ""
}

func dedupeInstalled(packages []InstalledPackage) []InstalledPackage {
	sort.Slice(packages, func(i, j int) bool {
		if packages[i].Scope != packages[j].Scope {
			return packages[i].Scope < packages[j].Scope
		}
		if packages[i].Path != packages[j].Path {
			return packages[i].Path < packages[j].Path
		}
		if packages[i].Manager != packages[j].Manager {
			return managerIndex(packages[i].Manager) < managerIndex(packages[j].Manager)
		}
		if packages[i].Name != packages[j].Name {
			return packages[i].Name < packages[j].Name
		}
		return packages[i].Version < packages[j].Version
	})
	result := packages[:0]
	for _, pkg := range packages {
		if len(result) == 0 || result[len(result)-1] != pkg {
			result = append(result, pkg)
		}
	}
	return result
}
