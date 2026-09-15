package packagemanager

import (
	"context"
	"encoding/json"
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
		return err
	}

	for toolName, tool := range tools {
		outdatedPackages[toolName] = append(outdatedPackages[toolName], NewNPMTool(toolName, tool, executable))
	}

	return nil
}
