package packagemanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

const PM_MAS = "mas"

type MAS struct{}

func (MAS) name() string                             { return PM_MAS }
func (MAS) testedVersions() VersionRange             { return VersionRange{MinimumMajor: 7, MaximumMajor: 7} }
func (MAS) updateImpact(Scope) MutationImpact        { return MutationImpact{} }
func (MAS) recoveryArgs(Scope, []Package) [][]string { return nil }
func (MAS) projectFiles() []string                   { return nil }
func (MAS) updateArgs([]Package) []string            { return nil }
func (MAS) capabilities() []Capability {
	return []Capability{CapabilityGlobalInventory, CapabilityGlobalOutdated}
}
func (MAS) listInstalled(ctx context.Context, executable string, _ Scope) ([]InstalledPackage, error) {
	output, err := runOutput(ctx, executable, "list", "--json")
	if err != nil {
		return nil, err
	}
	return parseMASInstalled(output)
}
func (MAS) listOutdatedScope(ctx context.Context, executable string, _ Scope, _ ManagerOptions, packages Packages) error {
	output, err := runOutput(ctx, executable, "outdated", "--json", "--inaccurate", "--no-check-min-os")
	if err != nil {
		return err
	}
	parsed, err := parseMASOutdated(output, executable)
	if err != nil {
		return err
	}
	MergePackages(packages, parsed)
	return nil
}

type masInstalledApp struct {
	AdamID  int64  `json:"adamID"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

func parseMASInstalled(output []byte) ([]InstalledPackage, error) {
	apps, err := decodeMASJSON[masInstalledApp](output)
	if err != nil {
		return nil, parserError(fmt.Errorf("decode mas inventory: %w", err))
	}
	packages := make([]InstalledPackage, 0, len(apps))
	for _, app := range apps {
		if app.AdamID == 0 || app.Name == "" || app.Version == "" {
			continue
		}
		packages = append(packages, InstalledPackage{Manager: PM_MAS, Name: fmt.Sprint(app.AdamID), Version: app.Version, Role: RoleApplication})
	}
	return packages, nil
}

type masOutdatedApp struct {
	AdamID     int64  `json:"adamID"`
	Version    string `json:"version"`
	NewVersion string `json:"newVersion"`
}

func parseMASOutdated(output []byte, executable string) (Packages, error) {
	apps, err := decodeMASJSON[masOutdatedApp](output)
	if err != nil {
		return nil, parserError(fmt.Errorf("decode mas outdated: %w", err))
	}
	packages := make(Packages)
	for _, app := range apps {
		if app.AdamID == 0 || app.Version == "" || app.NewVersion == "" {
			continue
		}
		name := fmt.Sprint(app.AdamID)
		packages[name] = append(packages[name], masTool{name: name, current: app.Version, latest: app.NewVersion, executable: executable})
	}
	return packages, nil
}

func decodeMASJSON[T any](output []byte) ([]T, error) {
	var items []T
	if err := json.Unmarshal(output, &items); err == nil {
		return items, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var item T
		err := decoder.Decode(&item)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if len(items) == 0 && len(bytes.TrimSpace(output)) > 0 {
		return nil, fmt.Errorf("no JSON objects")
	}
	return items, nil
}

type masTool struct {
	name       string
	current    string
	latest     string
	executable string
}

func (tool masTool) GetPackageManager() string   { return PM_MAS }
func (tool masTool) GetName() string             { return tool.name }
func (tool masTool) GetCurrentVersion() string   { return tool.current }
func (tool masTool) GetLatestVersion() string    { return tool.latest }
func (tool masTool) GetRequestedVersion() string { return "" }
func (tool masTool) GetExecutable() string       { return tool.executable }
