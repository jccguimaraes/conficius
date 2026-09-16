package main

import (
	"testing"

	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

func TestCommandRowsKeepPlansSeparate(t *testing.T) {
	plans := []packagemanager.UpdatePlan{
		{Manager: "mise", Executable: "/opt/mise", Scope: "global", Args: []string{"upgrade", "node"}, Packages: []packagemanager.Package{{Name: "node"}}},
		{Manager: "brew", Executable: "/opt/brew", Scope: "global", Args: []string{"upgrade", "wget"}, Packages: []packagemanager.Package{{Name: "wget"}}},
	}
	rows := commandRows(plans)
	if len(rows) != 2 || rows[0].Manager != "mise" || rows[1].Manager != "brew" || rows[1].Command != "/opt/brew upgrade wget" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestConfirmViewRendersEveryCommand(t *testing.T) {
	data := &model{planSummary: "2 packages in 2 commands", commands: []commandRow{
		{Manager: "mise", Scope: "global", Packages: "1 package(s)", Command: "/opt/mise upgrade node"},
		{Manager: "brew", Scope: "global", Packages: "1 package(s)", Command: "/opt/brew upgrade wget"},
	}}
	component := confirmView(data)
	assertRenderedContains(t, component, "2 packages in 2 commands")
	assertRenderedContains(t, component, "/opt/mise upgrade node")
	assertRenderedContains(t, component, "/opt/brew upgrade wget")
}
