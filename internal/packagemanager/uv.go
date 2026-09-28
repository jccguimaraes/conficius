package packagemanager

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const PM_UV = "uv"

type UV struct{}

func (UV) name() string                             { return PM_UV }
func (UV) testedVersions() VersionRange             { return VersionRange{MinimumMajor: 0, MaximumMajor: 0} }
func (UV) updateImpact(Scope) MutationImpact        { return MutationImpact{} }
func (UV) recoveryArgs(Scope, []Package) [][]string { return nil }
func (UV) projectFiles() []string                   { return nil }
func (UV) updateArgs([]Package) []string            { return nil }
func (UV) capabilities() []Capability {
	return []Capability{CapabilityGlobalInventory, CapabilityGlobalOutdated}
}
func (UV) listInstalled(ctx context.Context, executable string, _ Scope) ([]InstalledPackage, error) {
	output, err := runOutput(ctx, executable, "tool", "list")
	if err != nil {
		return nil, err
	}
	installed, _, err := parseUVTools(output, executable)
	return installed, err
}
func (UV) listOutdatedScope(ctx context.Context, executable string, _ Scope, _ ManagerOptions, packages Packages) error {
	output, err := runOutput(ctx, executable, "tool", "list", "--outdated")
	if err != nil {
		return err
	}
	_, outdated, err := parseUVTools(output, executable)
	if err != nil {
		return err
	}
	MergePackages(packages, outdated)
	return nil
}

var uvToolLine = regexp.MustCompile(`^([^ ]+) v([^ ]+)(?: \[latest: ([^]]+)\])?$`)

func parseUVTools(output []byte, executable string) ([]InstalledPackage, Packages, error) {
	var installed []InstalledPackage
	outdated := make(Packages)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" || strings.HasPrefix(line, "- ") {
			continue
		}
		match := uvToolLine.FindStringSubmatch(line)
		if match == nil {
			return nil, nil, parserError(fmt.Errorf("decode uv tool list line %q", line))
		}
		name, current, latest := match[1], match[2], match[3]
		installed = append(installed, InstalledPackage{Manager: PM_UV, Name: name, Version: current, Role: RoleApplication})
		if latest != "" && latest != current {
			outdated[name] = append(outdated[name], uvTool{name: name, current: current, latest: latest, executable: executable})
		}
	}
	return installed, outdated, nil
}

type uvTool struct {
	name       string
	current    string
	latest     string
	executable string
}

func (tool uvTool) GetPackageManager() string   { return PM_UV }
func (tool uvTool) GetName() string             { return tool.name }
func (tool uvTool) GetCurrentVersion() string   { return tool.current }
func (tool uvTool) GetLatestVersion() string    { return tool.latest }
func (tool uvTool) GetRequestedVersion() string { return "" }
func (tool uvTool) GetExecutable() string       { return tool.executable }
