package packagemanager

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const PM_APT = "apt"

type Apt struct{}

func (Apt) name() string                 { return PM_APT }
func (Apt) testedVersions() VersionRange { return VersionRange{MinimumMajor: 2, MaximumMajor: 3} }
func (Apt) updateImpact(Scope) MutationImpact {
	return MutationImpact{RequiresAdmin: true, MayRestartServices: true, MayReboot: true}
}
func (Apt) recoveryArgs(Scope, []Package) [][]string { return nil }
func (Apt) projectFiles() []string                   { return nil }
func (Apt) updateArgs([]Package) []string            { return nil }
func (Apt) capabilities() []Capability {
	return []Capability{CapabilityGlobalInventory, CapabilityGlobalOutdated}
}

func aptCompanion(executable, name string) (string, error) {
	path := filepath.Join(filepath.Dir(executable), name)
	if isExecutable(path) {
		return path, nil
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", &OperationError{Category: ErrorUnavailable, Err: fmt.Errorf("find apt companion %s: %w", name, err)}
	}
	return path, nil
}

func (Apt) listInstalled(ctx context.Context, executable string, _ Scope) ([]InstalledPackage, error) {
	query, err := aptCompanion(executable, "dpkg-query")
	if err != nil {
		return nil, err
	}
	output, err := runOutput(ctx, query, "-W", "-f=${binary:Package}\t${Version}\t${db:Status-Status}\n")
	if err != nil {
		return nil, err
	}
	return parseAptInstalled(output)
}

var aptPackageName = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*(?::[a-z0-9][a-z0-9-]*)?$`)
var aptInstallLine = regexp.MustCompile(`^Inst ([a-z0-9][a-z0-9+.-]*(?::[a-z0-9][a-z0-9-]*)?) \[([^\s\[\]]+)\] \(([^\s()]+) ([^()\[\]]+?)(?: \[([a-z0-9][a-z0-9-]*)\])?\)$`)

func parseAptInstalled(output []byte) ([]InstalledPackage, error) {
	var packages []InstalledPackage
	for _, line := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 || !aptPackageName.MatchString(fields[0]) || fields[2] == "" {
			return nil, parserError(fmt.Errorf("invalid dpkg-query inventory line %q", line))
		}
		if fields[2] != "installed" {
			continue
		}
		if len(strings.Fields(fields[1])) != 1 {
			return nil, parserError(fmt.Errorf("invalid dpkg-query version in %q", line))
		}
		packages = append(packages, InstalledPackage{Manager: PM_APT, Name: fields[0], Version: fields[1], Role: RoleLibrary})
	}
	return packages, nil
}

func (apt Apt) listOutdatedScope(ctx context.Context, executable string, scope Scope, _ ManagerOptions, packages Packages) error {
	companion, err := aptCompanion(executable, "apt-get")
	if err != nil {
		return err
	}
	installed, err := apt.listInstalled(ctx, executable, scope)
	if err != nil {
		return err
	}
	output, err := runOutput(ctx, companion, "--simulate", "--no-remove", "-o", "APT::Get::Upgrade-Allow-New=false", "upgrade")
	if err != nil {
		return err
	}
	parsed, err := parseAptOutdated(output, executable, installed)
	if err != nil {
		return err
	}
	MergePackages(packages, parsed)
	return nil
}

func parseAptOutdated(output []byte, executable string, installed []InstalledPackage) (Packages, error) {
	versions := make(map[string]string, len(installed))
	for _, pkg := range installed {
		versions[pkg.Name] = pkg.Version
	}
	packages := make(Packages)
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "Remv" {
			return nil, parserError(fmt.Errorf("unexpected apt removal %q", line))
		}
		if fields[0] != "Inst" {
			continue
		}
		match := aptInstallLine.FindStringSubmatch(line)
		if match == nil {
			return nil, parserError(fmt.Errorf("invalid apt upgrade line %q", line))
		}
		name := match[1]
		base, arch, qualified := strings.Cut(name, ":")
		if qualified && match[5] != "" && arch != match[5] {
			return nil, parserError(fmt.Errorf("conflicting apt architecture in %q", line))
		}
		if !qualified && match[5] != "" && versions[base+":"+match[5]] != "" {
			name = base + ":" + match[5]
		}
		if versions[name] != match[2] {
			return nil, parserError(fmt.Errorf("apt upgrade does not match installed package in %q", line))
		}
		if len(packages[name]) != 0 {
			return nil, parserError(fmt.Errorf("duplicate apt upgrade for %q", name))
		}
		packages[name] = []Tool{aptTool{name: name, current: match[2], latest: match[3], executable: executable}}
	}
	return packages, nil
}

type aptTool struct {
	name, current, latest, executable string
}

func (tool aptTool) GetPackageManager() string   { return PM_APT }
func (tool aptTool) GetName() string             { return tool.name }
func (tool aptTool) GetCurrentVersion() string   { return tool.current }
func (tool aptTool) GetLatestVersion() string    { return tool.latest }
func (tool aptTool) GetRequestedVersion() string { return "" }
func (tool aptTool) GetExecutable() string       { return tool.executable }
