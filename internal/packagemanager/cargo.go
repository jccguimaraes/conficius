package packagemanager

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const PM_CARGO = "cargo"

type Cargo struct{}

func (Cargo) name() string                             { return PM_CARGO }
func (Cargo) testedVersions() VersionRange             { return VersionRange{MinimumMajor: 1, MaximumMajor: 1} }
func (Cargo) capabilities() []Capability               { return []Capability{CapabilityGlobalInventory} }
func (Cargo) updateImpact(Scope) MutationImpact        { return MutationImpact{} }
func (Cargo) recoveryArgs(Scope, []Package) [][]string { return nil }
func (Cargo) projectFiles() []string                   { return nil }
func (Cargo) updateArgs([]Package) []string            { return nil }
func (Cargo) listInstalled(ctx context.Context, executable string, _ Scope) ([]InstalledPackage, error) {
	output, err := runOutput(ctx, executable, "install", "--list")
	if err != nil {
		return nil, err
	}
	return parseCargoInstalled(output)
}
func (Cargo) listOutdatedScope(context.Context, string, Scope, ManagerOptions, Packages) error {
	return nil
}

var cargoInstallHeader = regexp.MustCompile(`^([^ ]+) v([^ ]+):$`)

func parseCargoInstalled(output []byte) ([]InstalledPackage, error) {
	var packages []InstalledPackage
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" || strings.HasPrefix(line, "    ") {
			continue
		}
		match := cargoInstallHeader.FindStringSubmatch(line)
		if match == nil {
			return nil, parserError(fmt.Errorf("decode cargo install list line %q", line))
		}
		packages = append(packages, InstalledPackage{Manager: PM_CARGO, Name: match[1], Version: match[2], Role: RoleApplication})
	}
	return packages, nil
}
