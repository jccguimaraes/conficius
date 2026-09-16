package main

import (
	"strings"
	"testing"

	glyph "github.com/kungfusheep/glyph"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/jccguimaraes/conficius/internal/vulnerability"
)

func TestResultViewRendersPackageIdentity(t *testing.T) {
	data := &model{packages: []packagemanager.Package{{Manager: "npm", Name: "typescript", Current: "5.0", Latest: "5.1", Scope: "global"}}}
	component := resultView(data)
	assertRenderedContains(t, component, "Manager")
	assertRenderedContains(t, component, "typescript")
}

func TestScopeViewRendersScopeIdentity(t *testing.T) {
	data := &model{scopes: []config.Scope{{Kind: "project", Name: "app", Path: "/repo"}}}
	component, _ := scopeView(data, glyph.NewApp())
	assertRenderedContains(t, component, "project:app")
}

func TestDriftViewRendersPackageIdentity(t *testing.T) {
	data := &model{drift: []config.PackageState{{Manager: "mise", Name: "node", Installed: "20", DesiredVersion: "22", Status: config.DriftBehind}}}
	component := driftView(data)
	assertRenderedContains(t, component, "Desired")
	assertRenderedContains(t, component, "node")
}

func TestVulnerabilityViewRendersPackageIdentityAndAdvisory(t *testing.T) {
	data := &model{findings: vulnerabilityRows([]vulnerability.Finding{{
		Manager: "npm", Package: "lodash", Version: "4.17.20", Status: vulnerability.StatusVulnerable,
		Vulnerabilities: []vulnerability.Vulnerability{{ID: "GHSA-29mw-wpgm-hmr9"}},
	}})}
	component := vulnerabilityView(data)
	assertRenderedContains(t, component, "Advisories")
	assertRenderedContains(t, component, "lodash")
	assertRenderedContains(t, component, "GHSA-29mw-wpgm-hmr9")
}

func assertRenderedContains(t *testing.T, component glyph.Component, want string) {
	t.Helper()
	buffer := glyph.NewBuffer(160, 8)
	glyph.Build(component).Execute(buffer, 160, 8)
	for row := range 8 {
		if strings.Contains(buffer.GetLine(row), want) {
			return
		}
	}
	t.Fatalf("rendered output does not contain %q", want)
}

func TestGenerationRejectsStaleRefresh(t *testing.T) {
	if isCurrentGeneration(2, 1) || !isCurrentGeneration(2, 2) {
		t.Fatal("generation comparison failed")
	}
}

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

func TestSelectingScopeActivatesOnlyOnExplicitSelection(t *testing.T) {
	scopes := []config.Scope{{Kind: "global"}, {Kind: "project", Path: "/repo"}}
	data := &model{scopes: scopes}
	selectScope(data, &scopes[1])
	selected := selectedScopes(data)
	if len(selected) != 1 || selected[0].Path != "/repo" {
		t.Fatalf("selected = %#v", selected)
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
