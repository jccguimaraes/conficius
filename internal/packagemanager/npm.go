package packagemanager

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type NPMOutdatedTools map[string]NPMOutdatedTool

type NPMOutdatedTool struct {
	Current   string `json:"current"`
	Wanted    string `json:"wanted"`
	Latest    string `json:"latest"`
	Dependent string `json:"dependent"`
	Location  string `json:"location"`
}

type NPMTool struct {
	name       string
	current    string
	latest     string
	requested  string
	executable string
}

func NewNPMTool(toolName string, tool NPMOutdatedTool, executable string) NPMTool {
	return NPMTool{
		name:       toolName,
		current:    tool.Current,
		latest:     tool.Latest,
		requested:  tool.Wanted,
		executable: executable,
	}
}

type NPM struct {
	Version string
}

type NPMOptsUpdate any

func (NPM) name() string { return PM_NPM }

func (NPM) updateImpact(Scope) MutationImpact { return MutationImpact{} }

func (NPM) recoveryArgs(scope Scope, packages []Package) [][]string {
	commands := make([][]string, 0, len(packages))
	for _, pkg := range packages {
		if pkg.Current == "" {
			continue
		}
		args := []string{"install"}
		if scope.Kind != "project" && scope.Kind != "workspace" {
			args = append(args, NPMFlagGlobal)
		}
		commands = append(commands, append(args, pkg.Name+"@"+pkg.Current))
	}
	return commands
}

func (NPM) capabilities() []Capability {
	return []Capability{
		CapabilityGlobalInventory, CapabilityProjectInventory,
		CapabilityGlobalOutdated, CapabilityProjectOutdated,
		CapabilityGlobalUpdate, CapabilityProjectUpdate,
	}
}

func (NPM) listInstalled(ctx context.Context, executable string, scope Scope) ([]InstalledPackage, error) {
	return installedNPM(ctx, executable, scope)
}

func (p NPM) listOutdatedScope(ctx context.Context, executable string, scope Scope, _ ManagerOptions, packages Packages) error {
	return p.listOutdated(ctx, scope, executable, packages)
}

func (NPM) updateArgs(packages []Package) []string {
	if len(packages) == 1 && packages[0].Name == PM_NPM && packages[0].Scope != "project" {
		return []string{"install", NPMFlagGlobal, PM_NPM + "@" + packages[0].Latest}
	}
	args := []string{NPMCommandUpdate}
	if len(packages) > 0 && packages[0].Scope != "project" {
		args = append(args, NPMFlagGlobal)
	}
	for _, pkg := range packages {
		args = append(args, pkg.Name)
	}
	return args
}

const (
	PM_NPM string = "npm"

	NPMCommandExec     string = "npm"
	NPMCommandOutdated string = "outdated"
	NPMCommandUpdate   string = "update"

	NPMFlagJSON   string = "--json"
	NPMFlagGlobal string = "--global"
)

func (t NPMTool) GetPackageManager() string {
	return PM_NPM
}

func (t NPMTool) GetName() string {
	return t.name
}

func (t NPMTool) GetCurrentVersion() string {
	return t.current
}

func (t NPMTool) GetLatestVersion() string {
	return t.latest
}

func (t NPMTool) GetRequestedVersion() string {
	return t.requested
}

func (t NPMTool) GetExecutable() string {
	return t.executable
}

func (p NPM) ListOutdated(ctx context.Context, executable string, outdatedPackages Packages) error {
	return p.listOutdated(ctx, Scope{Kind: "global"}, executable, outdatedPackages)
}

func npmPrefixWarnings(ctx context.Context, executable, expected string) []string {
	output, err := runOutput(ctx, executable, "config", "get", "prefix")
	if err != nil {
		return []string{fmt.Sprintf("read npm global prefix: %v", err)}
	}
	actual := filepath.Clean(strings.TrimSpace(string(output)))
	expected = filepath.Clean(expected)
	var warnings []string
	if actual != expected {
		warnings = append(warnings, fmt.Sprintf("npm global prefix is %q; expected %q", actual, expected))
	}
	bin := filepath.Join(expected, "bin")
	found := false
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if cleaned, err := filepath.Abs(entry); err == nil && cleaned == bin {
			found = true
			break
		}
	}
	if !found {
		warnings = append(warnings, fmt.Sprintf("npm global binary directory %q is not on PATH", bin))
	}
	return warnings
}

func (p NPM) listOutdated(ctx context.Context, scope Scope, executable string, outdatedPackages Packages) error {
	args := []string{NPMCommandOutdated, NPMFlagJSON}
	if scope.Kind == "global" {
		args = []string{NPMCommandOutdated, NPMFlagGlobal, NPMFlagJSON}
	}
	output, err := runOutputDir(ctx, scope.Path, executable, args...)
	if err != nil {
		if len(output) == 0 {
			return err
		}
	}

	var tools NPMOutdatedTools
	if err := json.Unmarshal(output, &tools); err != nil {
		return parserError(err)
	}

	for toolName, tool := range tools {
		outdatedPackages[toolName] = append(outdatedPackages[toolName], NewNPMTool(toolName, tool, executable))
	}

	return nil
}
