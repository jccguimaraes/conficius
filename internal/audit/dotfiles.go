package audit

import (
	"fmt"

	"github.com/jccguimaraes/conficius/internal/dotfiles"
)

func LinkFindings(results []dotfiles.Result) []Finding {
	findings := make([]Finding, 0, len(results))
	for _, result := range results {
		finding := Finding{
			Code:           "dotfile_" + string(result.State),
			Message:        fmt.Sprintf("%s: %s", result.Name, result.Target),
			Recommendation: "No change needed.",
		}
		switch result.State {
		case dotfiles.StateHealthy:
			finding.Severity = SeverityOK
			finding.Message = fmt.Sprintf("%s link is healthy: %s", result.Name, result.Target)
		case dotfiles.StateMissing:
			finding.Severity = SeverityWarning
			finding.Message = fmt.Sprintf("%s link is missing: %s", result.Name, result.Target)
			finding.Recommendation = fmt.Sprintf("Link %s to %s", result.Target, result.Source)
		case dotfiles.StateWrongLink:
			finding.Severity = SeverityWarning
			finding.Message = fmt.Sprintf("%s link points elsewhere: %s", result.Name, result.Target)
			finding.Recommendation = fmt.Sprintf("Replace repository symlink with a link to %s", result.Source)
		case dotfiles.StateConflict:
			finding.Severity = SeverityError
			finding.Message = fmt.Sprintf("%s target is not a symlink: %s", result.Name, result.Target)
			finding.Recommendation = "Move or remove the existing target manually."
		}
		findings = append(findings, finding)
	}
	return findings
}
