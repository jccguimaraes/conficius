package packagemanager

import (
	"encoding/json"
	"fmt"
)

const PM_MAS = "mas"

type masInstalledApp struct {
	AdamID  int64  `json:"adamID"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

func parseMASInstalled(output []byte) ([]InstalledPackage, error) {
	var apps []masInstalledApp
	if err := json.Unmarshal(output, &apps); err != nil {
		var app masInstalledApp
		if lineErr := json.Unmarshal(output, &app); lineErr != nil {
			return nil, parserError(fmt.Errorf("decode mas inventory: %w", err))
		}
		apps = []masInstalledApp{app}
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
