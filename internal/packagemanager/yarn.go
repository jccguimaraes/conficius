package packagemanager

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const PM_YARN = "yarn"

type yarnInfo struct {
	Value    string `json:"value"`
	Children struct {
		Version string `json:"Version"`
	} `json:"children"`
}

func parseYarnInstalled(output []byte) ([]InstalledPackage, error) {
	var packages []InstalledPackage
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		var info yarnInfo
		if err := json.Unmarshal(scanner.Bytes(), &info); err != nil {
			return nil, parserError(fmt.Errorf("decode yarn inventory: %w", err))
		}
		name, _, ok := strings.Cut(info.Value, "@npm:")
		if !ok || name == "" || info.Children.Version == "" {
			return nil, parserError(fmt.Errorf("decode yarn inventory: missing package fields"))
		}
		packages = append(packages, InstalledPackage{Manager: PM_YARN, Name: name, Version: info.Children.Version, Role: RoleLibrary})
	}
	if err := scanner.Err(); err != nil {
		return nil, parserError(fmt.Errorf("decode yarn inventory: %w", err))
	}
	return packages, nil
}
