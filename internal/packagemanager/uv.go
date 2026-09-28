package packagemanager

import (
	"fmt"
	"regexp"
	"strings"
)

const PM_UV = "uv"

var uvToolLine = regexp.MustCompile(`^([^ ]+) v([^ ]+)(?: \[latest: ([^]]+)\])?$`)

func parseUVTools(output []byte, executable string) ([]InstalledPackage, Packages, error) {
	var installed []InstalledPackage
	outdated := make(Packages)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" || strings.HasPrefix(line, "- ") {
			continue
		}
		match := uvToolLine.FindStringSubmatch(line)
		if match == nil {
			return nil, nil, parserError(fmt.Errorf("decode uv tool list line %q", line))
		}
		name, current, latest := match[1], match[2], match[3]
		installed = append(installed, InstalledPackage{Manager: PM_UV, Name: name, Version: current, Role: RoleApplication})
		if latest != "" && latest != current {
			outdated[name] = append(outdated[name], uvTool{name: name, current: current, latest: latest, executable: executable})
		}
	}
	return installed, outdated, nil
}

type uvTool struct {
	name       string
	current    string
	latest     string
	executable string
}

func (tool uvTool) GetPackageManager() string   { return PM_UV }
func (tool uvTool) GetName() string             { return tool.name }
func (tool uvTool) GetCurrentVersion() string   { return tool.current }
func (tool uvTool) GetLatestVersion() string    { return tool.latest }
func (tool uvTool) GetRequestedVersion() string { return "" }
func (tool uvTool) GetExecutable() string       { return tool.executable }
