package packagemanager

import (
	"encoding/json"
	"fmt"
)

const PM_POETRY = "poetry"

type poetryPackage struct {
	Name            string `json:"name"`
	InstalledStatus string `json:"installed_status"`
	Version         string `json:"version"`
	LatestVersion   string `json:"latest_version"`
}

func parsePoetryPackages(output []byte, executable string) ([]InstalledPackage, Packages, error) {
	var decoded []poetryPackage
	if err := json.Unmarshal(output, &decoded); err != nil {
		return nil, nil, parserError(fmt.Errorf("decode poetry packages: %w", err))
	}
	installed := make([]InstalledPackage, 0, len(decoded))
	outdated := make(Packages)
	for _, pkg := range decoded {
		if pkg.Name == "" || pkg.Version == "" || pkg.InstalledStatus == "" {
			return nil, nil, parserError(fmt.Errorf("decode poetry packages: missing package fields"))
		}
		installed = append(installed, InstalledPackage{Manager: PM_POETRY, Name: pkg.Name, Version: pkg.Version, Role: RoleLibrary})
		if pkg.LatestVersion != "" && pkg.LatestVersion != pkg.Version {
			outdated[pkg.Name] = append(outdated[pkg.Name], poetryDependency{name: pkg.Name, current: pkg.Version, latest: pkg.LatestVersion, executable: executable})
		}
	}
	return installed, outdated, nil
}

type poetryDependency struct {
	name       string
	current    string
	latest     string
	executable string
}

func (pkg poetryDependency) GetPackageManager() string   { return PM_POETRY }
func (pkg poetryDependency) GetName() string             { return pkg.name }
func (pkg poetryDependency) GetCurrentVersion() string   { return pkg.current }
func (pkg poetryDependency) GetLatestVersion() string    { return pkg.latest }
func (pkg poetryDependency) GetRequestedVersion() string { return "" }
func (pkg poetryDependency) GetExecutable() string       { return pkg.executable }
