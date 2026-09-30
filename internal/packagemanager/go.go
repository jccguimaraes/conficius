package packagemanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/semver"
)

type Go struct{}

func (Go) name() string                 { return PM_GO }
func (Go) testedVersions() VersionRange { return VersionRange{MinimumMajor: 1, MaximumMajor: 1} }
func (Go) capabilities() []Capability {
	return []Capability{CapabilityGlobalInventory, CapabilityGlobalOutdated}
}
func (Go) updateImpact(Scope) MutationImpact        { return MutationImpact{} }
func (Go) recoveryArgs(Scope, []Package) [][]string { return nil }
func (Go) projectFiles() []string                   { return nil }
func (Go) updateArgs([]Package) []string            { return nil }
func (Go) listOutdatedScope(ctx context.Context, executable string, _ Scope, _ ManagerOptions, packages Packages) error {
	path, err := GoToolJournalPath()
	if err != nil {
		return err
	}
	journal, err := ReadGoToolJournal(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return parserError(fmt.Errorf("read Go tool journal: %w", err))
	}
	for _, imported := range journal.Imports {
		output, err := runOutput(ctx, executable, "list", "-m", "-json", imported.Module+"@latest")
		if err != nil {
			return err
		}
		var module struct {
			Path    string
			Version string
		}
		if err := json.Unmarshal(output, &module); err != nil {
			return parserError(fmt.Errorf("decode Go module freshness: %w", err))
		}
		if module.Path != imported.Module || !semver.IsValid(module.Version) {
			return parserError(fmt.Errorf("decode Go module freshness for %q", imported.Module))
		}
		if module.Version != imported.Version {
			packages[imported.Module] = append(packages[imported.Module], goTool{module: imported.Module, current: imported.Version, latest: module.Version, executable: executable})
		}
	}
	return nil
}
func (Go) listInstalled(context.Context, string, Scope) ([]InstalledPackage, error) {
	path, err := GoToolJournalPath()
	if err != nil {
		return nil, err
	}
	journal, err := ReadGoToolJournal(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, parserError(fmt.Errorf("read Go tool journal: %w", err))
	}
	packages := make([]InstalledPackage, 0, len(journal.Imports))
	for _, imported := range journal.Imports {
		packages = append(packages, InstalledPackage{Manager: PM_GO, Name: imported.Module, Version: imported.Version, Role: RoleDevelopmentTool})
	}
	return packages, nil
}

type goTool struct {
	module     string
	current    string
	latest     string
	executable string
}

func (tool goTool) GetPackageManager() string   { return PM_GO }
func (tool goTool) GetName() string             { return tool.module }
func (tool goTool) GetCurrentVersion() string   { return tool.current }
func (tool goTool) GetLatestVersion() string    { return tool.latest }
func (tool goTool) GetRequestedVersion() string { return "" }
func (tool goTool) GetExecutable() string       { return tool.executable }

func GoToolJournalPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory for Go tool journal: %w", err)
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "conficius", "go-tools.json"), nil
}
