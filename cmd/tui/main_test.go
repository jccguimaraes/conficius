package main

import (
	"testing"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

func TestFilterOutdatedUsesResolvedProfileSelection(t *testing.T) {
	packages := []packagemanager.Package{
		{Manager: "npm", Name: "keep", Scope: "global"},
		{Manager: "npm", Name: "drop", Scope: "global"},
	}
	selected := []config.InstalledVersion{{Manager: "npm", Name: "keep", Scope: "global"}}
	filtered := filterOutdated(packages, selected)
	if len(filtered) != 1 || filtered[0].Name != "keep" {
		t.Fatalf("filtered = %#v", filtered)
	}
}

func TestSelectedScopesUsesAllScopesWithoutSelection(t *testing.T) {
	scopes := []config.Scope{{Kind: "global"}, {Kind: "project", Path: "/repo"}}
	selected := selectedScopes(&model{scopes: scopes})
	if len(selected) != 2 {
		t.Fatalf("selected = %#v", selected)
	}
}

func TestSelectedScopesUsesActiveScope(t *testing.T) {
	scopes := []config.Scope{{Kind: "global"}, {Kind: "project", Path: "/repo"}}
	data := &model{scopes: scopes, active: &scopes[1]}
	selected := selectedScopes(data)
	if len(selected) != 1 || selected[0].Path != "/repo" {
		t.Fatalf("selected = %#v", selected)
	}
}
