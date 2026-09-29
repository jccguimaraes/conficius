package packagemanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
)

const PM_PNPM = "pnpm"

type PNPM struct{}

func (PNPM) name() string                 { return PM_PNPM }
func (PNPM) testedVersions() VersionRange { return VersionRange{MinimumMajor: 12, MaximumMajor: 12} }
func (PNPM) capabilities() []Capability {
	return []Capability{CapabilityProjectInventory, CapabilityProjectOutdated}
}
func (PNPM) updateImpact(Scope) MutationImpact        { return MutationImpact{} }
func (PNPM) recoveryArgs(Scope, []Package) [][]string { return nil }
func (PNPM) projectFiles() []string                   { return []string{"package.json", "pnpm-lock.yaml"} }
func (PNPM) updateArgs([]Package) []string            { return nil }
func (PNPM) listInstalled(ctx context.Context, executable string, scope Scope) ([]InstalledPackage, error) {
	output, err := runOutputDir(ctx, scope.Path, executable, "list", "--depth", "0", "--json")
	if err != nil {
		return nil, err
	}
	return parsePNPMInstalled(output)
}
func (PNPM) listOutdatedScope(ctx context.Context, executable string, scope Scope, _ ManagerOptions, packages Packages) error {
	cmd := commandContext(ctx, executable, "outdated", "--format", "json")
	cmd.Dir = scope.Path
	cmd.Env = managerEnvironment(scope.Path)
	output, err := cmd.Output()
	var exitErr *exec.ExitError
	if err != nil && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || len(output) == 0) {
		stderr := ""
		if exitErr != nil {
			stderr = string(exitErr.Stderr)
		}
		return classifyOperationError(ctx, err, stderr)
	}
	outdated, err := parsePNPMOutdated(output, executable)
	if err != nil {
		return err
	}
	MergePackages(packages, outdated)
	return nil
}

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
