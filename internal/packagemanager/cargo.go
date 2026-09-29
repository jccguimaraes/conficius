package packagemanager

import (
	"fmt"
	"regexp"
	"strings"
)

const PM_CARGO = "cargo"

var cargoInstallHeader = regexp.MustCompile(`^([^ ]+) v([^ ]+):$`)

func parseCargoInstalled(output []byte) ([]InstalledPackage, error) {
	var packages []InstalledPackage
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" || strings.HasPrefix(line, "    ") {
			continue
		}
		match := cargoInstallHeader.FindStringSubmatch(line)
		if match == nil {
			return nil, parserError(fmt.Errorf("decode cargo install list line %q", line))
		}
		packages = append(packages, InstalledPackage{Manager: PM_CARGO, Name: match[1], Version: match[2], Role: RoleApplication})
	}
	return packages, nil
}
