package packagemanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

type InstalledPackage struct {
	Manager      string `json:"manager"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	Scope        string `json:"scope,omitempty"`
	Path         string `json:"path,omitempty"`
	Workspace    string `json:"workspace,omitempty"`
	OSVEcosystem string `json:"osv_ecosystem,omitempty"`
	OSVName      string `json:"osv_name,omitempty"`
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
	statuses, err := inspectWithOptions(pms, options)
	if err != nil {
		return InstalledResult{}, err
	}
	result := InstalledResult{Managers: statuses}
	var operations []readOperation[[]InstalledPackage]
	for i, status := range result.Managers {
		if status.Status != StatusAvailable {
			continue
		}
		for _, executable := range status.Executables {
			for _, scope := range scopes {
				if status.Name == PM_BREW && scope.Kind == "project" {
					continue
				}
				manager, executable, scope := status.Name, executable, scope
				operations = append(operations, readOperation[[]InstalledPackage]{managerIndex: i, manager: manager, executable: executable, scope: scope, run: func(ctx context.Context) ([]InstalledPackage, error) {
					return listInstalledScope(ctx, manager, executable, scope)
				}})
			}
		}
	}
	var inventoryErrors []error
	for _, operationResult := range runReadOperations(ctx, operations) {
		if operationResult.err != nil {
			status := &result.Managers[operationResult.operation.managerIndex]
			status.Status = StatusError
			status.Error = operationResult.err.Error()
			inventoryErrors = append(inventoryErrors, fmt.Errorf("%s (%s, %s): %w", operationResult.operation.manager, operationResult.operation.executable, operationResult.operation.scope.Kind, operationResult.err))
			continue
		}
		result.Packages = append(result.Packages, operationResult.value...)
	}
	result.Packages = dedupeInstalled(result.Packages)
	return result, errors.Join(inventoryErrors...)
}

func listInstalledScope(ctx context.Context, manager, executable string, scope Scope) ([]InstalledPackage, error) {
	var packages []InstalledPackage
	var err error
	switch manager {
	case PM_NPM:
		packages, err = installedNPM(ctx, executable, scope)
	case PM_MISE:
		packages, err = installedMise(ctx, executable, scope.Path)
	case PM_BREW:
		packages, err = installedHomebrew(ctx, executable)
	default:
		return nil, exec.ErrNotFound
	}
	for i := range packages {
		packages[i].Scope = scope.Kind
		packages[i].Path = scope.Path
	}
	return packages, err
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
		return nil, fmt.Errorf("decode npm inventory: %w", err)
	}
	packages := make([]InstalledPackage, 0, len(document.Dependencies))
	for name, dependency := range document.Dependencies {
		if dependency.Version != "" {
			packages = append(packages, InstalledPackage{Manager: PM_NPM, Name: name, Version: dependency.Version})
		}
	}
	return packages, nil
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
		return nil, fmt.Errorf("decode mise inventory: %w", err)
	}
	var packages []InstalledPackage
	for name, versions := range document {
		for _, version := range versions {
			if version.Installed && version.Version != "" {
				packages = append(packages, InstalledPackage{Manager: PM_MISE, Name: name, Version: version.Version})
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
		return nil, fmt.Errorf("decode Homebrew inventory: %w", err)
	}
	var packages []InstalledPackage
	for _, formula := range document.Formulae {
		for _, installed := range formula.Installed {
			if installed.Version != "" {
				packages = append(packages, InstalledPackage{Manager: PM_BREW, Name: formula.Name, Version: installed.Version})
			}
		}
	}
	for _, cask := range document.Casks {
		if cask.Installed != "" {
			packages = append(packages, InstalledPackage{Manager: PM_BREW, Name: cask.Token, Version: cask.Installed})
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
		pkg := InstalledPackage{Manager: "manager", Name: status.Name, Version: version, Scope: "global"}
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
