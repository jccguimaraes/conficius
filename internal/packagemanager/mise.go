package packagemanager

import (
	"context"
	"encoding/json"
	"strings"
)

type MiseOutdatedTools map[string]MiseOutdatedTool

type MiseOutdatedTool struct {
	Name      string `json:"name"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Requested string `json:"requested"`
}

type MiseTool struct {
	name       string
	current    string
	latest     string
	requested  string
	executable string
}

func NewMiseTool(tool MiseOutdatedTool, executable string) MiseTool {
	return MiseTool{
		name:       tool.Name,
		current:    tool.Current,
		latest:     tool.Latest,
		requested:  tool.Requested,
		executable: executable,
	}
}

type Mise struct {
	Version string
}

type MiseOptsUpdate struct {
	Bump      bool
	OnlyLocal bool
	Inactive  bool
	Excluded  []string
}

type MiseOpts struct {
	Local bool
}

func (Mise) name() string { return PM_MISE }

func (Mise) capabilities() []Capability {
	return []Capability{
		CapabilityGlobalInventory, CapabilityProjectInventory,
		CapabilityGlobalOutdated, CapabilityProjectOutdated,
		CapabilityGlobalUpdate, CapabilityProjectUpdate,
	}
}

func (Mise) listInstalled(ctx context.Context, executable string, scope Scope) ([]InstalledPackage, error) {
	return installedMise(ctx, executable, scope.Path)
}

func (p Mise) listOutdatedScope(ctx context.Context, executable string, scope Scope, options ManagerOptions, packages Packages) error {
	return p.listOutdated(ctx, scope.Path, executable, options, packages)
}

func (Mise) updateArgs(packages []Package) []string {
	args := []string{MiseCommandUpgrade, MiseFlagBump, MiseFlagYes}
	for _, pkg := range packages {
		args = append(args, pkg.Name)
	}
	return args
}

const (
	PM_MISE string = "mise"

	MiseCommandExec string = "mise"

	MiseCommandOutdated string = "outdated"
	MiseCommandUpgrade  string = "upgrade"

	MiseFlagJSON         string = "--json"
	MiseFlagBump         string = "--bump"
	MiseFlagExcludes     string = "--exclude"
	MiseFlagLocal        string = "--local"
	MiseFlagOnlyInactive string = "--inactive"
	MiseFlagYes          string = "--yes"
)

func (t MiseTool) GetPackageManager() string {
	return PM_MISE
}

func (t MiseTool) GetName() string {
	return t.name
}

func (t MiseTool) GetCurrentVersion() string {
	return t.current
}

func (t MiseTool) GetLatestVersion() string {
	return t.latest
}

func (t MiseTool) GetRequestedVersion() string {
	return t.requested
}

func (t MiseTool) GetExecutable() string {
	return t.executable
}

func equivalentMiseVersions(current, latest string) bool {
	return strings.TrimPrefix(current, "v") == strings.TrimPrefix(latest, "v")
}

func (p Mise) ListOutdated(ctx context.Context, executable string, outdatedPackages Packages) error {
	return p.listOutdated(ctx, "", executable, ManagerOptions{IncludeBumpCandidates: true}, outdatedPackages)
}

func (p Mise) listOutdated(ctx context.Context, dir, executable string, options ManagerOptions, outdatedPackages Packages) error {
	args := []string{MiseCommandOutdated, MiseFlagJSON}
	if dir != "" {
		args = append(args, MiseFlagLocal)
		options.IncludeBumpCandidates = true
	}
	if options.IncludeBumpCandidates {
		args = append(args, MiseFlagBump)
	}
	output, err := runOutputDir(ctx, dir, executable, args...)
	if err != nil {
		return err
	}

	var tools MiseOutdatedTools
	if err := json.Unmarshal(output, &tools); err != nil {
		return parserError(err)
	}

	for _, tool := range tools {
		if equivalentMiseVersions(tool.Current, tool.Latest) {
			continue
		}
		outdatedPackages[tool.Name] = append(outdatedPackages[tool.Name], NewMiseTool(tool, executable))
	}

	return nil
}
