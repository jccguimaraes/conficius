package packagemanager

import (
	"encoding/json"
	"fmt"
)

const PM_PNPM = "pnpm"

type pnpmInventoryProject struct {
	Dependencies         map[string]pnpmInventoryPackage `json:"dependencies"`
	DevDependencies      map[string]pnpmInventoryPackage `json:"devDependencies"`
	OptionalDependencies map[string]pnpmInventoryPackage `json:"optionalDependencies"`
}

type pnpmInventoryPackage struct {
	Version string `json:"version"`
}

func parsePNPMInstalled(output []byte) ([]InstalledPackage, error) {
	var projects []pnpmInventoryProject
	if err := json.Unmarshal(output, &projects); err != nil {
		return nil, parserError(fmt.Errorf("decode pnpm inventory: %w", err))
	}
	if projects == nil {
		return nil, parserError(fmt.Errorf("decode pnpm inventory: expected array"))
	}
	var packages []InstalledPackage
	for _, project := range projects {
		for class, dependencies := range map[string]map[string]pnpmInventoryPackage{
			"runtime": project.Dependencies, "development": project.DevDependencies, "optional": project.OptionalDependencies,
		} {
			for name, pkg := range dependencies {
				if name == "" || pkg.Version == "" {
					return nil, parserError(fmt.Errorf("decode pnpm inventory package %q: missing fields", name))
				}
				role := RoleLibrary
				if class == "development" {
					role = RoleDevelopmentTool
				}
				packages = append(packages, InstalledPackage{Manager: PM_PNPM, Name: name, Version: pkg.Version, Role: role, DependencyClass: class})
			}
		}
	}
	return packages, nil
}

type pnpmOutdatedPackage struct {
	Current        string `json:"current"`
	Wanted         string `json:"wanted"`
	Latest         string `json:"latest"`
	DependencyType string `json:"dependencyType"`
	Deprecated     bool   `json:"isDeprecated"`
}

func parsePNPMOutdated(output []byte, executable string) (Packages, error) {
	var decoded map[string]pnpmOutdatedPackage
	if err := json.Unmarshal(output, &decoded); err != nil {
		return nil, parserError(fmt.Errorf("decode pnpm outdated: %w", err))
	}
	if decoded == nil {
		return nil, parserError(fmt.Errorf("decode pnpm outdated: expected object"))
	}
	packages := make(Packages)
	for name, pkg := range decoded {
		if name == "" || pkg.Current == "" || pkg.Latest == "" || pkg.DependencyType == "" {
			return nil, parserError(fmt.Errorf("decode pnpm outdated package %q: missing fields", name))
		}
		packages[name] = append(packages[name], pnpmPackage{name: name, current: pkg.Current, requested: pkg.Wanted, latest: pkg.Latest, executable: executable})
	}
	return packages, nil
}

type pnpmPackage struct {
	name       string
	current    string
	requested  string
	latest     string
	executable string
}

func (pkg pnpmPackage) GetPackageManager() string   { return PM_PNPM }
func (pkg pnpmPackage) GetName() string             { return pkg.name }
func (pkg pnpmPackage) GetCurrentVersion() string   { return pkg.current }
func (pkg pnpmPackage) GetLatestVersion() string    { return pkg.latest }
func (pkg pnpmPackage) GetRequestedVersion() string { return pkg.requested }
func (pkg pnpmPackage) GetExecutable() string       { return pkg.executable }
