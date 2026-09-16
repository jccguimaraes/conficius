package main

import (
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

func TestToggleSelectedMarksCurrentPackage(t *testing.T) {
	data := &model{selected: map[string]bool{}}
	pkg := &packagemanager.Package{Manager: "mise", Name: "node", Scope: "global"}
	toggleSelected(data, pkg)
	if !data.selected[packageKey("global", "", "mise", "node")] {
		t.Fatal("package not selected")
	}
	toggleSelected(data, pkg)
	if len(data.selected) != 0 {
		t.Fatal("package not unselected")
	}
}

func TestFormatPlansShowsExactCommandAndDirectory(t *testing.T) {
	got := formatPlans([]packagemanager.UpdatePlan{{Executable: "/usr/bin/mise", Directory: "/repo", Args: []string{"upgrade", "node"}}})
	if !strings.Contains(got, "(cd /repo && /usr/bin/mise upgrade node)") {
		t.Fatalf("formatPlans = %q", got)
	}
}
