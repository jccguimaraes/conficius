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
