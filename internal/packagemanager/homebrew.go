package packagemanager

import (
	"context"
	"encoding/json"
)

type HomebrewTool struct {
	name       string
	current    string
	latest     string
	requested  string
	executable string
}

func NewHomebrewTool(tool HomebrewOutdatedTool, executable string) HomebrewTool {
	current := ""
	if len(tool.InstalledVersions) > 0 {
		current = tool.InstalledVersions[0]
	}

	return HomebrewTool{
		name:       tool.Name,
		current:    current,
		latest:     tool.CurrentVersion,
		requested:  tool.PinnedVersion,
		executable: executable,
	}
}

type HomebrewOutdatedTools struct {
	Formulae []HomebrewOutdatedTool `json:"formulae"`
	Casks    []HomebrewOutdatedTool `json:"casks"`
}

type HomebrewOutdatedTool struct {
	Name              string   `json:"name"`
	InstalledVersions []string `json:"installed_versions"`
	CurrentVersion    string   `json:"current_version"`
	Pinned            bool     `json:"pinned"`
	PinnedVersion     string   `json:"pinned_version"`
}

type Homebrew struct {
	Version string
}

type HomebrewUpdateOpts struct{}

func (Homebrew) name() string { return PM_BREW }

func (Homebrew) updateImpact(Scope) MutationImpact {
	return MutationImpact{MayRestartServices: true}
}

func (Homebrew) recoveryArgs(Scope, []Package) [][]string { return nil }

func (Homebrew) capabilities() []Capability {
	return []Capability{CapabilityGlobalInventory, CapabilityGlobalOutdated, CapabilityGlobalUpdate}
}

func (Homebrew) listInstalled(ctx context.Context, executable string, _ Scope) ([]InstalledPackage, error) {
	return installedHomebrew(ctx, executable)
}

func (p Homebrew) listOutdatedScope(ctx context.Context, executable string, scope Scope, options ManagerOptions, packages Packages) error {
	return p.listOutdated(ctx, scope.Path, executable, options, packages)
}

func (Homebrew) updateArgs(packages []Package) []string {
	args := []string{HomebrewCommandUpgrade, HomebrewFlagYes, HomebrewFlagGreedy}
	for _, pkg := range packages {
		args = append(args, pkg.Name)
	}
	return args
}

const (
	PM_BREW     string = "brew"
	PM_HOMEBREW string = "homebrew"

	HomebrewCommandExec string = "brew"

	HomebrewCommandOutdated string = "outdated"
	HomebrewCommandUpgrade  string = "upgrade"
	HomebrewFlagYes         string = "--yes"

	HomebrewFlagVersions  string = "--versions"
	HomebrewFlagGreedy    string = "--greedy"
	HomebrewFlagInstalled string = "--installed"
	HomebrewFlagJSON      string = "--json"
	HomebrewFlagJSONv2    string = "--json=v2"
)

func (t HomebrewTool) GetPackageManager() string {
	return PM_BREW
}

func (t HomebrewTool) GetName() string {
	return t.name
}

func (t HomebrewTool) GetCurrentVersion() string {
	return t.current
}

func (t HomebrewTool) GetLatestVersion() string {
	return t.latest
}

func (t HomebrewTool) GetRequestedVersion() string {
	return t.requested
}

func (t HomebrewTool) GetExecutable() string {
	return t.executable
}

func (p Homebrew) ListOutdated(ctx context.Context, executable string, outdatedPackages Packages) error {
	return p.listOutdated(ctx, "", executable, ManagerOptions{IncludeGreedy: true}, outdatedPackages)
}

func (p Homebrew) listOutdated(ctx context.Context, dir, executable string, options ManagerOptions, outdatedPackages Packages) error {
	args := []string{HomebrewCommandOutdated, HomebrewFlagJSONv2}
	args = append(args, HomebrewFlagGreedy)
	output, err := runOutputDir(ctx, dir, executable, args...)
	if err != nil {
		return err
	}

	var tools HomebrewOutdatedTools
	if err := json.Unmarshal(output, &tools); err != nil {
		return parserError(err)
	}

	for _, tool := range append(tools.Formulae, tools.Casks...) {
		if len(tool.InstalledVersions) == 0 {
			continue
		}
		outdatedPackages[tool.Name] = append(outdatedPackages[tool.Name], NewHomebrewTool(tool, executable))
	}

	return nil
}
