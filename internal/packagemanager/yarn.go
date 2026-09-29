package packagemanager

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const PM_YARN = "yarn"

type Yarn struct{}

func (Yarn) name() string                             { return PM_YARN }
func (Yarn) testedVersions() VersionRange             { return VersionRange{MinimumMajor: 4, MaximumMajor: 4} }
func (Yarn) capabilities() []Capability               { return []Capability{CapabilityProjectInventory} }
func (Yarn) updateImpact(Scope) MutationImpact        { return MutationImpact{} }
func (Yarn) recoveryArgs(Scope, []Package) [][]string { return nil }
func (Yarn) projectFiles() []string                   { return []string{"package.json", "yarn.lock"} }
func (Yarn) updateArgs([]Package) []string            { return nil }
func (Yarn) listInstalled(ctx context.Context, executable string, scope Scope) ([]InstalledPackage, error) {
	output, err := runOutputDir(ctx, scope.Path, executable, "info", "--json")
	if err != nil {
		return nil, err
	}
	return parseYarnInstalled(output)
}
func (Yarn) listOutdatedScope(context.Context, string, Scope, ManagerOptions, Packages) error {
	return nil
}

type yarnInfo struct {
	Value    string `json:"value"`
	Children struct {
		Version string `json:"Version"`
	} `json:"children"`
}

func parseYarnInstalled(output []byte) ([]InstalledPackage, error) {
	var packages []InstalledPackage
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		var info yarnInfo
		if err := json.Unmarshal(scanner.Bytes(), &info); err != nil {
			return nil, parserError(fmt.Errorf("decode yarn inventory: %w", err))
		}
		name, _, ok := strings.Cut(info.Value, "@npm:")
		if !ok || name == "" || info.Children.Version == "" {
			return nil, parserError(fmt.Errorf("decode yarn inventory: missing package fields"))
		}
		packages = append(packages, InstalledPackage{Manager: PM_YARN, Name: name, Version: info.Children.Version, Role: RoleLibrary})
	}
	if err := scanner.Err(); err != nil {
		return nil, parserError(fmt.Errorf("decode yarn inventory: %w", err))
	}
	return packages, nil
}
