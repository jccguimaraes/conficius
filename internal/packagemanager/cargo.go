package packagemanager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

const PM_CARGO = "cargo"

type Cargo struct{}

func (Cargo) name() string                 { return PM_CARGO }
func (Cargo) testedVersions() VersionRange { return VersionRange{MinimumMajor: 1, MaximumMajor: 1} }
func (Cargo) capabilities() []Capability {
	return []Capability{CapabilityGlobalInventory, CapabilityProjectInventory}
}
func (Cargo) updateImpact(Scope) MutationImpact        { return MutationImpact{} }
func (Cargo) recoveryArgs(Scope, []Package) [][]string { return nil }
func (Cargo) projectFiles() []string                   { return []string{"Cargo.lock"} }
func (Cargo) updateArgs([]Package) []string            { return nil }
func (Cargo) listInstalled(ctx context.Context, executable string, scope Scope) ([]InstalledPackage, error) {
	if scope.Kind == "project" || scope.Kind == "workspace" {
		return cargoLockfileInventory(filepath.Join(scope.Path, "Cargo.lock"))
	}
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

func cargoLockfileInventory(path string) ([]InstalledPackage, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Cargo.lock: %w", err)
	}
	var lock struct {
		Packages []struct {
			Name    string `toml:"name"`
			Version string `toml:"version"`
			Source  string `toml:"source"`
		} `toml:"package"`
	}
	if err := toml.Unmarshal(contents, &lock); err != nil {
		return nil, parserError(fmt.Errorf("decode Cargo.lock: %w", err))
	}
	packages := make([]InstalledPackage, 0, len(lock.Packages))
	for _, pkg := range lock.Packages {
		if pkg.Name == "" || pkg.Version == "" || pkg.Source == "" {
			continue
		}
		packages = append(packages, InstalledPackage{Manager: PM_CARGO, Name: pkg.Name, Version: pkg.Version, Role: RoleLibrary, Manifest: path, OSVEcosystem: "crates.io", OSVName: pkg.Name})
	}
	return packages, nil
}

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
