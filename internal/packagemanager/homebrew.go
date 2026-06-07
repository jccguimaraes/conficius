package packagemanager

import (
	"encoding/json"

	"github.com/jccguimaraes/conficius/internal/config"
)

type HomebrewTool struct {
	name       string
	current    string
	latest     string
	requested  string
	executable string
}

func NewHomebrewTool(tool HomebrewOutdatedTool, executable string) HomebrewTool {
	return HomebrewTool{
		name:       tool.Name,
		current:    tool.InstalledVersions[0],
		latest:     tool.CurrentVersion,
		requested:  tool.PinnedVersion,
		executable: executable,
	}
}

type HomebrewOutdatedTools struct {
	Formulae []HomebrewOutdatedTool `json:"formulae"`
	Casks    []HomebrewOutdatedTool `json:"casks"`
}

type HomebrewOutdatedTool struct {
	Name              string   `json:"name"`
	InstalledVersions []string `json:"installed_versions"`
	CurrentVersion    string   `json:"current_version"`
	Pinned            bool     `json:"pinned"`
	PinnedVersion     string   `json:"pinned_version"`
}

type Homebrew struct {
	Version string
}

type HomebrewUpdateOpts struct{}

const (
	PM_BREW     string = "brew"
	PM_HOMEBREW string = "homebrew"

	HomebrewCommandExec string = "brew"

	HomebrewCommandOutdated string = "outdated"

	HomebrewFlagVersions  string = "--versions"
	HomebrewFlagGreedy    string = "--greedy"
	HomebrewFlagInstalled string = "--installed"
	HomebrewFlagJSON      string = "--json"
	HomebrewFlagJSONv2    string = "--json=v2"
)

func (t HomebrewTool) GetName() string {
	return t.name
}

func (t HomebrewTool) GetCurrentVersion() string {
	return t.current
}

func (t HomebrewTool) GetLatestVersion() string {
	return t.latest
}

func (t HomebrewTool) GetRequestedVersion() string {
	return t.requested
}

func (t HomebrewTool) GetExecutable() string {
	return t.executable
}

func (p Homebrew) ListOutdated(snapshot *config.Snapshot, executable string, outdatedPackages Packages) error {
	output, err := RunShellCommand(
		snapshot.Shell,
		executable,
		HomebrewCommandOutdated,
		HomebrewFlagGreedy,
		HomebrewFlagJSON,
	)
	if err != nil {
		return err
	}

	var tools HomebrewOutdatedTools
	if err := json.Unmarshal(output, &tools); err != nil {
		return err
	}

	for _, tool := range tools.Formulae {
		outdatedPackages[tool.Name] = append(outdatedPackages[tool.Name], NewHomebrewTool(tool, executable))
	}

	for _, tool := range tools.Casks {
		outdatedPackages[tool.Name] = append(outdatedPackages[tool.Name], NewHomebrewTool(tool, executable))
	}

	return nil
}
