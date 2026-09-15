package audit

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/layout"
	"github.com/jccguimaraes/conficius/internal/shell"
)

func TestZshRecommendsExpectedZDOTDIR(t *testing.T) {
	expected := layout.Expected{ConfigRoot: filepath.Join("home", ".config")}
	findings := Zsh(shell.Zsh{}, expected)
	want := filepath.Join(expected.ConfigRoot, shell.SHELL_ZSH)
	if len(findings) != 1 || !strings.Contains(findings[0].Recommendation, want) {
		t.Fatalf("findings = %#v; want recommendation containing %q", findings, want)
	}
}
