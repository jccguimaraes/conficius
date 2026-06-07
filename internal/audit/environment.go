package audit

import (
	"fmt"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/layout"
)

func Environment(snapshot *config.Snapshot, expected layout.Expected) Report {
	report := Report{
		Snapshot: snapshot,
		Expected: expected,
	}

	if snapshot.UserEnv.XdgConfigHome == "" {
		report.Findings = append(report.Findings, Finding{
			Severity:       SeverityWarning,
			Code:           "xdg_config_home_unset",
			Message:        "XDG_CONFIG_HOME is not set.",
			Recommendation: fmt.Sprintf("Set XDG_CONFIG_HOME to '%s'", expected.ConfigRoot),
		})
	}

	if snapshot.UserEnv.XdgConfigHome != "" && snapshot.UserEnv.XdgConfigHome != expected.ConfigRoot {
		report.Findings = append(report.Findings, Finding{
			Severity:       SeverityWarning,
			Code:           "xdg_config_home_mismatch",
			Message:        "XDG_CONFIG_HOME does not match the selected layout.",
			Recommendation: fmt.Sprintf("Update XDG_CONFIG_HOME to '%s'", expected.ConfigRoot),
		})
	}

	if snapshot.UserEnv.XdgConfigHome == expected.ConfigRoot {
		report.Findings = append(report.Findings, Finding{
			Severity:       SeverityOK,
			Code:           "xdg_config_home_match",
			Message:        "XDG_CONFIG_HOME matches the selected layout.",
			Recommendation: fmt.Sprintf("XDG_CONFIG_HOME matches the selected layout '%s'", expected.ConfigRoot),
		})
	}

	return report
}
