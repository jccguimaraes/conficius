package packagemanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Go struct{}

func (Go) name() string                             { return PM_GO }
func (Go) testedVersions() VersionRange             { return VersionRange{MinimumMajor: 1, MaximumMajor: 1} }
func (Go) capabilities() []Capability               { return []Capability{CapabilityGlobalInventory} }
func (Go) updateImpact(Scope) MutationImpact        { return MutationImpact{} }
func (Go) recoveryArgs(Scope, []Package) [][]string { return nil }
func (Go) projectFiles() []string                   { return nil }
func (Go) updateArgs([]Package) []string            { return nil }
func (Go) listOutdatedScope(context.Context, string, Scope, ManagerOptions, Packages) error {
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
