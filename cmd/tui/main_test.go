package main

import (
	"fmt"
	"strings"
	"testing"

	glyph "github.com/kungfusheep/glyph"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/jccguimaraes/conficius/internal/security"
)

func TestResultViewRendersPackageIdentity(t *testing.T) {
	packages := []packagemanager.Package{{Manager: "npm", Name: "typescript", Current: "5.0", Latest: "5.1", Scope: "global"}}
	data := &model{packages: packages, rows: outdatedRows(packages, nil, nil)}
	component, _ := resultView(data)
	assertRenderedContains(t, component, "Manager")
	assertRenderedContains(t, component, "[ ]")
	assertRenderedContains(t, component, "typescript")
}

func TestResultViewRendersMarkedPackage(t *testing.T) {
	pkg := packagemanager.Package{Manager: "npm", Name: "typescript", Scope: "global"}
	selected := map[string]bool{packageKey("global", "", "npm", "typescript"): true}
	data := &model{packages: []packagemanager.Package{pkg}, rows: outdatedRows([]packagemanager.Package{pkg}, selected, nil), selected: selected}
	component, _ := resultView(data)
	assertRenderedContains(t, component, "[x]")
}

func TestScopeViewRendersScopeIdentity(t *testing.T) {
	data := &model{scopes: []config.Scope{{Kind: "project", Name: "app", Path: "/repo"}}}
	component, _ := scopeView(data, glyph.NewApp())
	assertRenderedContains(t, component, "project:app")
}

func TestDriftViewRendersPackageIdentity(t *testing.T) {
	data := &model{drift: []config.PackageState{{Manager: "mise", Name: "node", Installed: "20", DesiredVersion: "22", Status: config.DriftBehind}}}
	component, _ := driftView(data)
	assertRenderedContains(t, component, "Desired")
	assertRenderedContains(t, component, "node")
}

func TestVulnerabilityViewRendersPackageIdentityAndAdvisory(t *testing.T) {
	data := &model{findings: vulnerabilityRows([]security.Evidence{{
		Source: security.SourceOSV, Manager: "npm", Package: "lodash", Version: "4.17.20", Status: security.StatusVulnerable,
		Advisory: "GHSA-29mw-wpgm-hmr9", UpgradeTo: "4.17.21",
	}})}
	component, _ := vulnerabilityView(data)
	assertRenderedContains(t, component, "Advisory")
	assertRenderedContains(t, component, "lodash")
	assertRenderedContains(t, component, "GHSA-29mw-wpgm-hmr9")
	assertRenderedContains(t, component, "4.17.21")
}

func TestVulnerabilityDetailViewRendersEvidence(t *testing.T) {
	data := &model{detail: vulnerabilityRow{Evidence: security.Evidence{
		Source: security.SourceEUVD, Advisory: "CVE-2026-1", SourceID: "EUVD-1", Score: 8.1,
		Rules: []string{">=20", "20.*"}, Aliases: []string{"CVE-2026-1", "GHSA-1"}, UpgradeTo: "22.22.4",
		Description: "test description", Reference: "https://example.test/advisory", CVELink: "https://www.cve.org/CVERecord?id=CVE-2026-1", UpgradeNote: "Latest version does not fix this CVE.",
	}}}
	component := vulnerabilityDetailView(data, glyph.NewApp())
	for _, want := range []string{"EUVD", "CVE-2026-1", "EUVD-1", "8.1", ">=20, 20.*", "GHSA-1", "22.22.4", "test description", "https://example.test/advisory", "Latest version does not fix this CVE."} {
		assertRenderedContains(t, component, want)
	}
}

func TestEvidenceRowsShowCurrentAndUpgradeVersion(t *testing.T) {
	rows := vulnerabilityRows([]security.Evidence{{Source: security.SourceEUVD, Manager: "mise", Package: "node", Version: "22.10.0", Advisory: "CVE-2026-1", UpgradeTo: "22.22.4"}})
	if len(rows) != 1 || rows[0].Current != "22.10.0" || rows[0].UpgradeTo != "22.22.4" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestVulnerabilitySummarySeparatesStatuses(t *testing.T) {
	got := vulnerabilitySummary(3, 2, security.Summary{Vulnerable: 1, Unmapped: 1, Unknown: 2, Errors: 1})
	want := "3 outdated, 2 drift; 1 vulnerable, 1 unmapped, 2 unknown, 1 errors"
	if got != want {
		t.Fatalf("summary = %q", got)
	}
}

func TestSeverityColorUsesCVSSBands(t *testing.T) {
	for _, test := range []struct {
		score float64
		want  glyph.Color
	}{{9.8, glyph.Red}, {8.0, glyph.RGB(255, 140, 0)}, {5.0, glyph.Yellow}, {2.0, glyph.Blue}} {
		if got := severityColor(test.score); !got.Equal(test.want) {
			t.Fatalf("severityColor(%v) = %#v", test.score, got)
		}
	}
}

func TestVulnerabilityRowsHideCleanAndAdvisorylessDiagnostics(t *testing.T) {
	rows := vulnerabilityRows([]security.Evidence{
		{Package: "clean", Status: security.StatusClean},
		{Package: "unmapped", Status: security.StatusUnmapped},
		{Package: "failed", Status: security.StatusError},
		{Package: "unknown-rule", Status: security.StatusUnknown, Advisory: "CVE-1"},
		{Package: "affected", Status: security.StatusVulnerable, Advisory: "CVE-2"},
	})
	if len(rows) != 2 || rows[0].Package != "unknown-rule" || rows[1].Package != "affected" {
		t.Fatalf("rows = %#v", rows)
	}
}

func assertRenderedContains(t *testing.T, component glyph.Component, want string) {
	t.Helper()
	buffer := glyph.NewBuffer(160, 20)
	glyph.Build(component).Execute(buffer, 160, 20)
	for row := range 20 {
		if strings.Contains(buffer.GetLine(row), want) {
			return
		}
	}
	var lines []string
	for row := range 20 {
		lines = append(lines, buffer.GetLine(row))
	}
	t.Fatalf("rendered output does not contain %q:\n%s", want, strings.Join(lines, "\n"))
}

func TestVulnerabilityViewKeepsFooterAtBottomWithManyRows(t *testing.T) {
	data := &model{status: "summary"}
	for i := range 40 {
		data.findings = append(data.findings, vulnerabilityRow{Evidence: security.Evidence{Source: "EUVD", Package: fmt.Sprintf("tool-%02d", i), Status: "vulnerable"}})
	}
	component, list := vulnerabilityView(data)
	buffer := glyph.NewBuffer(120, 12)
	glyph.Build(component).Execute(buffer, 120, 12)
	if !strings.Contains(buffer.GetLine(10), "move") {
		t.Fatalf("footer = %q", buffer.GetLine(10))
	}
	list.Down(nil)
	if list.Index() != 1 {
		t.Fatalf("selection = %d", list.Index())
	}
}

func TestDriftViewKeepsFooterAtBottomAndMovesSelection(t *testing.T) {
	data := &model{status: "summary"}
	for i := range 40 {
		data.drift = append(data.drift, config.PackageState{Manager: "mise", Name: fmt.Sprintf("tool-%02d", i), Status: config.DriftBehind})
	}
	component, list := driftView(data)
	buffer := glyph.NewBuffer(120, 12)
	glyph.Build(component).Execute(buffer, 120, 12)
	if !strings.Contains(buffer.GetLine(10), "move") {
		t.Fatalf("footer = %q", buffer.GetLine(10))
	}
	list.Down(nil)
	if list.Index() != 1 {
		t.Fatalf("selection = %d", list.Index())
	}
}

func TestViewsFillAvailableHeight(t *testing.T) {
	data := &model{}
	outdated, _ := resultView(data)
	drift, _ := driftView(data)
	vulnerabilities, _ := vulnerabilityView(data)
	for name, component := range map[string]glyph.Component{
		"outdated":        outdated,
		"drift":           drift,
		"vulnerabilities": vulnerabilities,
	} {
		buffer := glyph.NewBuffer(80, 20)
		glyph.Build(component).Execute(buffer, 80, 20)
		if line := buffer.GetLine(19); !strings.Contains(line, "╰") {
			t.Fatalf("%s bottom border = %q", name, line)
		}
	}
}

func TestLoadingContentShowsGlyphSpinner(t *testing.T) {
	loading := true
	component := loadingContent(&loading, glyph.Text("content"))
	assertRenderedContains(t, component, "Loading...")
	loading = false
	assertRenderedContains(t, component, "content")
}

func TestGenerationRejectsStaleRefresh(t *testing.T) {
	if isCurrentGeneration(2, 1) || !isCurrentGeneration(2, 2) {
		t.Fatal("generation comparison failed")
	}
}

func TestSecurityTargetsSurviveOutdatedDisplayFiltering(t *testing.T) {
	all := []packagemanager.Package{{Manager: "manager", Name: "npm", Scope: "global", Latest: "11.20.0"}}
	displayed := filterOutdated(append([]packagemanager.Package(nil), all...), nil)
	if len(displayed) != 0 || len(all) != 1 || all[0].Latest != "11.20.0" {
		t.Fatalf("displayed = %#v, security targets = %#v", displayed, all)
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

func TestIncludesGlobal(t *testing.T) {
	if !includesGlobal([]config.Scope{{Kind: "global"}}) || includesGlobal([]config.Scope{{Kind: "project", Path: "/repo"}}) {
		t.Fatal("global scope detection failed")
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
