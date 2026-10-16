package main

import (
	"fmt"

	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

func reconcileUpgradeOutcomes(data *model, packages []packagemanager.Package) (upgraded, unchanged, failed int) {
	if len(data.attempted) == 0 {
		return 0, 0, 0
	}
	outdated := make(map[string]packagemanager.Package, len(packages))
	for _, pkg := range packages {
		outdated[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)] = pkg
	}
	for key, previous := range data.attempted {
		if message := data.failed[key]; message != "" {
			if _, remains := outdated[key]; remains {
				failed++
			} else {
				delete(data.failed, key)
				delete(data.selected, key)
				upgraded++
			}
			continue
		}
		pkg, remains := outdated[key]
		if !remains {
			delete(data.selected, key)
			upgraded++
			continue
		}
		message := "No version change after command completed"
		if pkg.Current != previous {
			message = fmt.Sprintf("Still outdated after %s → %s", previous, pkg.Current)
		}
		data.failed[key] = message
		data.selected[key] = true
		unchanged++
	}
	clear(data.attempted)
	return upgraded, unchanged, failed
}

func vulnerabilityStatus(summary string, inspectionErr, scanErr error) string {
	if inspectionErr != nil {
		summary += "; Package inspection failed. Run `conficius packages doctor`."
	}
	if scanErr != nil {
		summary += "; vulnerability scan failed: " + scanErr.Error()
	}
	return summary
}

func refreshStatus(outdated, upgraded, unchanged, failed int) string {
	if upgraded+unchanged+failed == 0 {
		return fmt.Sprintf("%d outdated; scanning vulnerabilities...", outdated)
	}
	return fmt.Sprintf("%d upgraded, %d unchanged, %d failed; %d outdated; scanning vulnerabilities...", upgraded, unchanged, failed, outdated)
}
