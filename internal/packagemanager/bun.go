package packagemanager

import (
	"fmt"
	"regexp"
	"strings"
)

const PM_BUN = "bun"

var bunListLine = regexp.MustCompile(`^[├└]── (.+)@([^@]+)$`)

func parseBunInstalled(output []byte) ([]InstalledPackage, error) {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || !strings.Contains(lines[0], " node_modules (") {
		return nil, parserError(fmt.Errorf("decode bun inventory: missing header"))
	}
	packages := make([]InstalledPackage, 0, len(lines)-1)
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		match := bunListLine.FindStringSubmatch(line)
		if match == nil {
			return nil, parserError(fmt.Errorf("decode bun inventory line %q", line))
		}
		packages = append(packages, InstalledPackage{Manager: PM_BUN, Name: match[1], Version: match[2], Role: RoleLibrary})
	}
	return packages, nil
}
