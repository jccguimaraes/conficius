package audit

import (
	"fmt"
	"path/filepath"

	"github.com/jccguimaraes/conficius/internal/layout"
	shell "github.com/jccguimaraes/conficius/internal/shell"
)

func Zsh(zsh shell.Zsh, expected layout.Expected) []Finding {
	findings := []Finding{}

	expectedZDOTDIR := filepath.Join(expected.ConfigRoot, shell.SHELL_ZSH)

	if expectedZDOTDIR != zsh.ZDOTDIR {
		findings = append(findings, Finding{
			Severity:       SeverityWarning,
			Code:           "z_dot_dir_unset",
			Message:        "ZDOTDIR is not set.",
			Recommendation: fmt.Sprintf("Set ZDOTDIR to '%s'", expected.ConfigRoot),
		})
	}

	return findings
}
