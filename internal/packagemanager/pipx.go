package packagemanager

import (
	"encoding/json"
	"fmt"
	"sort"
)

const PM_PIPX = "pipx"

type pipxList struct {
	SpecVersion string `json:"pipx_spec_version"`
	Venvs       map[string]struct {
		Metadata struct {
			MainPackage struct {
				Package        string   `json:"package"`
				PackageVersion string   `json:"package_version"`
				Apps           []string `json:"apps"`
			} `json:"main_package"`
		} `json:"metadata"`
	} `json:"venvs"`
}

type pipxOutdated struct {
	ResultVersion string `json:"pipx_result_version"`
	Status        string `json:"status"`
	Data          struct {
		Packages []struct {
			Package       string `json:"package"`
			Version       string `json:"version"`
			LatestVersion string `json:"latest_version"`
		} `json:"packages"`
	} `json:"data"`
}

func parsePipxOutdated(output []byte, executable string) (Packages, error) {
	var result pipxOutdated
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, parserError(fmt.Errorf("decode pipx outdated: %w", err))
	}
	if result.ResultVersion == "" || result.Status == "" || result.Data.Packages == nil {
		return nil, parserError(fmt.Errorf("decode pipx outdated: missing schema fields"))
	}
	packages := make(Packages)
	for _, pkg := range result.Data.Packages {
		if pkg.Package == "" || pkg.Version == "" || pkg.LatestVersion == "" {
			return nil, parserError(fmt.Errorf("decode pipx outdated: missing package fields"))
		}
		packages[pkg.Package] = append(packages[pkg.Package], pipxPackage{name: pkg.Package, current: pkg.Version, latest: pkg.LatestVersion, executable: executable})
	}
	return packages, nil
}

type pipxPackage struct {
	name       string
	current    string
	latest     string
	executable string
}

func (pkg pipxPackage) GetPackageManager() string   { return PM_PIPX }
func (pkg pipxPackage) GetName() string             { return pkg.name }
func (pkg pipxPackage) GetCurrentVersion() string   { return pkg.current }
func (pkg pipxPackage) GetLatestVersion() string    { return pkg.latest }
func (pkg pipxPackage) GetRequestedVersion() string { return "" }
func (pkg pipxPackage) GetExecutable() string       { return pkg.executable }

func parsePipxInstalled(output []byte) ([]InstalledPackage, error) {
	var list pipxList
	if err := json.Unmarshal(output, &list); err != nil {
		return nil, parserError(fmt.Errorf("decode pipx inventory: %w", err))
	}
	if list.SpecVersion == "" || list.Venvs == nil {
		return nil, parserError(fmt.Errorf("decode pipx inventory: missing schema fields"))
	}
	names := make([]string, 0, len(list.Venvs))
	for name := range list.Venvs {
		names = append(names, name)
	}
	sort.Strings(names)
	packages := make([]InstalledPackage, 0, len(names))
	for _, environment := range names {
		pkg := list.Venvs[environment].Metadata.MainPackage
		if pkg.Package == "" || pkg.PackageVersion == "" {
			return nil, parserError(fmt.Errorf("decode pipx inventory environment %q: missing package fields", environment))
		}
		packages = append(packages, InstalledPackage{Manager: PM_PIPX, Name: pkg.Package, Version: pkg.PackageVersion, Role: RoleApplication})
	}
	return packages, nil
}
