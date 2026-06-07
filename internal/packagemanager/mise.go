package packagemanager

import (
	"encoding/json"

	"github.com/jccguimaraes/conficius/internal/config"
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
)

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

func (p Mise) ListOutdated(snapshot *config.Snapshot, executable string, outdatedPackages Packages) error {
	output, err := RunShellCommand(
		snapshot.Shell,
		executable,
		MiseCommandOutdated,
		MiseFlagJSON,
		MiseFlagBump,
		MiseFlagOnlyInactive,
	)
	if err != nil {
		return err
	}

	var tools MiseOutdatedTools
	if err := json.Unmarshal(output, &tools); err != nil {
		return err
	}

	for _, tool := range tools {
		outdatedPackages[tool.Name] = append(outdatedPackages[tool.Name], NewMiseTool(tool, executable))
	}

	return nil
}
