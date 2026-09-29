package packagemanager

import (
	"context"
	"encoding/json"
	"fmt"
)

const PM_POETRY = "poetry"

type Poetry struct{}

func (Poetry) name() string                 { return PM_POETRY }
func (Poetry) testedVersions() VersionRange { return VersionRange{MinimumMajor: 2, MaximumMajor: 2} }
func (Poetry) capabilities() []Capability {
	return []Capability{CapabilityProjectInventory, CapabilityProjectOutdated}
}
func (Poetry) updateImpact(Scope) MutationImpact        { return MutationImpact{} }
func (Poetry) recoveryArgs(Scope, []Package) [][]string { return nil }
func (Poetry) projectFiles() []string                   { return []string{"pyproject.toml", "poetry.lock"} }
func (Poetry) updateArgs([]Package) []string            { return nil }
func (Poetry) listInstalled(ctx context.Context, executable string, scope Scope) ([]InstalledPackage, error) {
	output, err := runOutputDir(ctx, scope.Path, executable, "show", "--format", "json")
	if err != nil {
		return nil, err
	}
	installed, _, err := parsePoetryPackages(output, executable)
	return installed, err
}
func (Poetry) listOutdatedScope(ctx context.Context, executable string, scope Scope, _ ManagerOptions, packages Packages) error {
	output, err := runOutputDir(ctx, scope.Path, executable, "show", "--outdated", "--format", "json")
	if err != nil {
		return err
	}
	_, outdated, err := parsePoetryPackages(output, executable)
	if err != nil {
		return err
	}
	MergePackages(packages, outdated)
	return nil
}

type poetryPackage struct {
	Name            string `json:"name"`
	InstalledStatus string `json:"installed_status"`
	Version         string `json:"version"`
	LatestVersion   string `json:"latest_version"`
}

func parsePoetryPackages(output []byte, executable string) ([]InstalledPackage, Packages, error) {
	var decoded []poetryPackage
	if err := json.Unmarshal(output, &decoded); err != nil {
		return nil, nil, parserError(fmt.Errorf("decode poetry packages: %w", err))
	}
	installed := make([]InstalledPackage, 0, len(decoded))
	outdated := make(Packages)
	for _, pkg := range decoded {
		if pkg.Name == "" || pkg.Version == "" || pkg.InstalledStatus == "" {
			return nil, nil, parserError(fmt.Errorf("decode poetry packages: missing package fields"))
		}
		installed = append(installed, InstalledPackage{Manager: PM_POETRY, Name: pkg.Name, Version: pkg.Version, Role: RoleLibrary})
		if pkg.LatestVersion != "" && pkg.LatestVersion != pkg.Version {
			outdated[pkg.Name] = append(outdated[pkg.Name], poetryDependency{name: pkg.Name, current: pkg.Version, latest: pkg.LatestVersion, executable: executable})
		}
	}
	return installed, outdated, nil
}

type poetryDependency struct {
	name       string
	current    string
	latest     string
	executable string
}

func (pkg poetryDependency) GetPackageManager() string   { return PM_POETRY }
func (pkg poetryDependency) GetName() string             { return pkg.name }
func (pkg poetryDependency) GetCurrentVersion() string   { return pkg.current }
func (pkg poetryDependency) GetLatestVersion() string    { return pkg.latest }
func (pkg poetryDependency) GetRequestedVersion() string { return "" }
func (pkg poetryDependency) GetExecutable() string       { return pkg.executable }
