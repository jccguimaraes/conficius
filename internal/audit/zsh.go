package audit

import (
	"fmt"
	"path/filepath"

	"github.com/jccguimaraes/conficius/internal/layout"
	shell "github.com/jccguimaraes/conficius/internal/shell"
)

func Zsh(zsh shell.Zsh, expected layout.Expected) []Finding {
	expectedZDOTDIR := filepath.Join(expected.ConfigRoot, shell.SHELL_ZSH)
	if expectedZDOTDIR == zsh.ZDOTDIR {
		return []Finding{{
			Severity:       SeverityOK,
			Code:           "zdotdir_match",
			Message:        "ZDOTDIR matches the selected layout.",
			Recommendation: fmt.Sprintf("Keep ZDOTDIR set to '%s'", expectedZDOTDIR),
		}}
	}

	message := "ZDOTDIR does not match the selected layout."
	code := "zdotdir_mismatch"
	if zsh.ZDOTDIR == "" {
		message = "ZDOTDIR is not set."
		code = "zdotdir_unset"
	}
	return []Finding{{
		Severity:       SeverityWarning,
		Code:           code,
		Message:        message,
		Recommendation: fmt.Sprintf("Set ZDOTDIR to '%s'", expectedZDOTDIR),
	}}
}
