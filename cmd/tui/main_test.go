package main

import (
	"testing"

	"github.com/jccguimaraes/conficius/internal/config"
)

func TestSelectedScopesUsesActiveScope(t *testing.T) {
	scopes := []config.Scope{{Kind: "global"}, {Kind: "project", Path: "/repo"}}
	data := &model{scopes: scopes, active: &scopes[1]}
	selected := selectedScopes(data)
	if len(selected) != 1 || selected[0].Path != "/repo" {
		t.Fatalf("selected = %#v", selected)
	}
}
