package packagemanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
)

type InstalledPackage struct {
	Manager string `json:"manager"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InstalledResult struct {
	Managers []ManagerStatus    `json:"managers"`
	Packages []InstalledPackage `json:"packages"`
}

func QueryInstalled(ctx context.Context, pms []string) (InstalledResult, error) {
	statuses, err := Inspect(pms)
	if err != nil {
		return InstalledResult{}, err
	}
	result := InstalledResult{Managers: statuses}
	var inventoryErrors []error
	for i := range result.Managers {
		status := &result.Managers[i]
		if status.Status != StatusAvailable {
			continue
		}
		for _, executable := range status.Executables {
			packages, err := listInstalled(ctx, status.Name, executable)
			if err != nil {
				status.Status = StatusError
				status.Error = err.Error()
				inventoryErrors = append(inventoryErrors, fmt.Errorf("%s (%s): %w", status.Name, executable, err))
				continue
			}
			result.Packages = append(result.Packages, packages...)
		}
	}
	result.Packages = dedupeInstalled(result.Packages)
	return result, errors.Join(inventoryErrors...)
}

func listInstalled(ctx context.Context, manager, executable string) ([]InstalledPackage, error) {
	switch manager {
	case PM_NPM:
		return installedNPM(ctx, executable)
	case PM_MISE:
		return installedMise(ctx, executable)
	case PM_BREW:
		return installedHomebrew(ctx, executable)
	default:
		return nil, exec.ErrNotFound
	}
}

func installedNPM(ctx context.Context, executable string) ([]InstalledPackage, error) {
	output, err := runOutput(ctx, executable, "ls", NPMFlagGlobal, "--depth=0", NPMFlagJSON)
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

func installedMise(ctx context.Context, executable string) ([]InstalledPackage, error) {
	output, err := runOutput(ctx, executable, "ls", "--installed", "--json")
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

func dedupeInstalled(packages []InstalledPackage) []InstalledPackage {
	sort.Slice(packages, func(i, j int) bool {
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
