package packagemanager

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const PM_BUN = "bun"

type Bun struct{}

func (Bun) name() string                 { return PM_BUN }
func (Bun) testedVersions() VersionRange { return VersionRange{MinimumMajor: 1, MaximumMajor: 1} }
func (Bun) capabilities() []Capability {
	return []Capability{CapabilityProjectInventory, CapabilityProjectOutdated, CapabilityProjectUpdate}
}
func (Bun) updateImpact(Scope) MutationImpact        { return MutationImpact{MayChangeManifests: true} }
func (Bun) recoveryArgs(Scope, []Package) [][]string { return nil }
func (Bun) projectFiles() []string                   { return []string{"package.json", "bun.lock", "bun.lockb"} }
func (Bun) updateArgs(packages []Package) []string {
	args := []string{"update"}
	for _, pkg := range packages {
		args = append(args, pkg.Name)
	}
	return args
}
func (Bun) listInstalled(ctx context.Context, executable string, scope Scope) ([]InstalledPackage, error) {
	output, err := runOutputDir(ctx, scope.Path, executable, "pm", "ls")
	if err != nil {
		return nil, err
	}
	return parseBunInstalled(output)
}
func (Bun) listOutdatedScope(ctx context.Context, executable string, scope Scope, _ ManagerOptions, packages Packages) error {
	output, err := runOutputDir(ctx, scope.Path, executable, "outdated")
	if err != nil {
		return err
	}
	outdated, err := parseBunOutdated(output, executable)
	if err != nil {
		return err
	}
	MergePackages(packages, outdated)
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

var bunOutdatedANSI = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func parseBunOutdated(output []byte, executable string) (Packages, error) {
	packages := make(Packages)
	columns := 0
	for _, line := range strings.Split(bunOutdatedANSI.ReplaceAllString(string(output), ""), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if columns == 0 && strings.HasPrefix(line, "bun outdated v") {
			continue
		}
		if strings.Trim(line, " ┌┬┐├┼┤└┴┘─+|-") == "" {
			continue
		}
		if columns != 0 && line == "Note: The * indicates that version isn't true latest due to minimum release age" {
			continue
		}
		cells := strings.Split(strings.ReplaceAll(line, "│", "|"), "|")
		if len(cells) < 6 || cells[0] != "" || cells[len(cells)-1] != "" {
			return nil, parserError(fmt.Errorf("decode bun outdated line %q", line))
		}
		cells = cells[1 : len(cells)-1]
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if columns == 0 {
			if (len(cells) != 4 && len(cells) != 5) || cells[0] != "Package" || cells[1] != "Current" || cells[2] != "Update" || cells[3] != "Latest" || (len(cells) == 5 && cells[4] != "Workspace") {
				return nil, parserError(fmt.Errorf("decode bun outdated: unexpected header %q", line))
			}
			columns = len(cells)
			continue
		}
		if len(cells) != columns {
			return nil, parserError(fmt.Errorf("decode bun outdated: unexpected columns %q", line))
		}
		name := cells[0]
		for _, suffix := range []string{" (dev)", " (peer)", " (optional)"} {
			name = strings.TrimSuffix(name, suffix)
		}
		name, _, _ = strings.Cut(name, "@npm:")
		if name == "" || strings.ContainsAny(name, " \t\r\n") || strings.HasPrefix(name, "-") {
			return nil, parserError(fmt.Errorf("decode bun outdated: invalid package %q", cells[0]))
		}
		for i := 1; i <= 3; i++ {
			cells[i] = strings.TrimSuffix(cells[i], " *")
			if cells[i] == "" || strings.ContainsAny(cells[i], " \t\r\n") {
				return nil, parserError(fmt.Errorf("decode bun outdated: invalid version %q", cells[i]))
			}
		}
		if cells[1] == cells[2] {
			continue
		}
		packages[name] = append(packages[name], bunPackage{name: name, current: cells[1], requested: cells[2], latest: cells[3], executable: executable})
	}
	return packages, nil
}

type bunPackage struct {
	name       string
	current    string
	requested  string
	latest     string
	executable string
}

func (pkg bunPackage) GetPackageManager() string   { return PM_BUN }
func (pkg bunPackage) GetName() string             { return pkg.name }
func (pkg bunPackage) GetCurrentVersion() string   { return pkg.current }
func (pkg bunPackage) GetLatestVersion() string    { return pkg.latest }
func (pkg bunPackage) GetRequestedVersion() string { return pkg.requested }
func (pkg bunPackage) GetExecutable() string       { return pkg.executable }
