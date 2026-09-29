package packagemanager

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const PM_BUN = "bun"

type Bun struct{}

func (Bun) name() string                             { return PM_BUN }
func (Bun) testedVersions() VersionRange             { return VersionRange{MinimumMajor: 1, MaximumMajor: 1} }
func (Bun) capabilities() []Capability               { return []Capability{CapabilityProjectInventory} }
func (Bun) updateImpact(Scope) MutationImpact        { return MutationImpact{} }
func (Bun) recoveryArgs(Scope, []Package) [][]string { return nil }
func (Bun) projectFiles() []string                   { return []string{"package.json", "bun.lock", "bun.lockb"} }
func (Bun) updateArgs([]Package) []string            { return nil }
func (Bun) listInstalled(ctx context.Context, executable string, scope Scope) ([]InstalledPackage, error) {
	output, err := runOutputDir(ctx, scope.Path, executable, "pm", "ls")
	if err != nil {
		return nil, err
	}
	return parseBunInstalled(output)
}
func (Bun) listOutdatedScope(context.Context, string, Scope, ManagerOptions, Packages) error {
	return nil
}

var bunListLine = regexp.MustCompile(`^[├└]── (.+)@([^@]+)$`)

func parseBunInstalled(output []byte) ([]InstalledPackage, error) {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || !strings.Contains(lines[0], " node_modules (") {
		return nil, parserError(fmt.Errorf("decode bun inventory: missing header"))
	}
	packages := make([]InstalledPackage, 0, len(lines)-1)
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		match := bunListLine.FindStringSubmatch(line)
		if match == nil {
			return nil, parserError(fmt.Errorf("decode bun inventory line %q", line))
		}
		packages = append(packages, InstalledPackage{Manager: PM_BUN, Name: match[1], Version: match[2], Role: RoleLibrary})
	}
	return packages, nil
}
