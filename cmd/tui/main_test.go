package main

import (
	"errors"
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

func TestResultViewShowsFullUpdateHint(t *testing.T) {
	pkg := packagemanager.Package{Manager: "brew", Name: "notion", Scope: "global"}
	message := "brew (/opt/homebrew/bin/brew): Homebrew app conflict.\nInspect both app copies before repair.\nConficius did not attempt automatic repair."
	data := &model{rows: outdatedRows([]packagemanager.Package{pkg}, nil, map[string]string{packageKey("global", "", "brew", "notion"): message}), updateErrorLines: strings.Split(message, "\n")}
	component, _ := updateErrorView(data)
	assertRenderedContains(t, component, "Inspect both app copies before repair.")
	assertRenderedContains(t, component, "Conficius did not attempt automatic repair.")
}

func TestResultViewRendersMarkedPackage(t *testing.T) {
	pkg := packagemanager.Package{Manager: "npm", Name: "typescript", Scope: "global"}
	selected := map[string]bool{packageKey("global", "", "npm", "typescript"): true}
	data := &model{packages: []packagemanager.Package{pkg}, rows: outdatedRows([]packagemanager.Package{pkg}, selected, nil), selected: selected}
	component, _ := resultView(data)
	assertRenderedContains(t, component, "[x]")
}

func TestScopeViewRendersScopeIdentity(t *testing.T) {
	data := &model{scopes: []config.Scope{{Kind: "global"}, {Kind: "project", Name: "app", Path: "/repo"}, {Kind: "project", Path: "/other"}}}
	component, _ := scopeView(data, glyph.NewApp())
	buffer := glyph.NewBuffer(80, 24)
	glyph.Build(component).Execute(buffer, 80, 24)
	output := homeRenderOutput(buffer, 24)
	for _, want := range []string{"global", "app  /repo", "project  /other"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing compact scope %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "project:") {
		t.Fatalf("redundant scope prefix:\n%s", output)
	}
}

func TestVulnerabilityViewRendersPackageIdentityAndAdvisory(t *testing.T) {
	data := &model{findings: vulnerabilityRows([]security.Evidence{{
		Source: security.SourceOSV, Manager: "npm", Package: "lodash", Version: "4.17.20", Status: security.StatusVulnerable,
		Advisory: "GHSA-29mw-wpgm-hmr9", UpgradeTo: "4.17.21",
	}})}
	component, _ := vulnerabilityView(data)
	assertRenderedContains(t, component, "Advisory")
	assertRenderedContains(t, component, "lodash")
	assertRenderedContains(t, component, "GHSA-")
	assertRenderedContains(t, component, "4.17.21")
}

func TestVulnerabilityDetailViewRendersEvidence(t *testing.T) {
	data := &model{detail: vulnerabilityRow{Evidence: security.Evidence{
		Source: security.SourceEUVD, Advisory: "CVE-2026-1", SourceID: "EUVD-1", Score: 8.1,
		Rules: []string{">=20", "20.*"}, Aliases: []string{"CVE-2026-1", "GHSA-1"}, UpgradeTo: "22.22.4",
		Description: "test description", Reference: "https://example.test/advisory", CVELink: "https://www.cve.org/CVERecord?id=CVE-2026-1", UpgradeNote: "Latest version does not fix this CVE.",
	}}}
	component := vulnerabilityDetailView(data, glyph.NewApp())
	assertRenderedContains(t, component, "CVE-2026-1")
	content, viewer := vulnerabilityDetailContent(&data.detail)
	template := glyph.Build(content)
	for _, size := range [][2]int{{40, 12}, {80, 24}, {120, 30}} {
		buffer := glyph.NewBuffer(size[0], size[1])
		template.Execute(buffer, int16(size[0]), int16(size[1]))
		viewer.Layer().ScrollToTop()
		var output strings.Builder
		for offset := 0; offset <= viewer.Layer().MaxScroll(); offset++ {
			viewer.Layer().ScrollTo(offset)
			template.Execute(buffer, int16(size[0]), int16(size[1]))
			output.WriteString(homeRenderOutput(buffer, size[1]))
			output.WriteByte('\n')
		}
		for _, want := range []string{"EUVD", "CVE-2026-1", "EUVD-1", "8.1", ">=20, 20.*", "GHSA-1", "22.22.4", "test description", "https://example.test/advisory", "Latest version does not fix this CVE."} {
			if !strings.Contains(output.String(), want) {
				t.Fatalf("%dx%d scroll missing %q (max scroll %d)", size[0], size[1], want, viewer.Layer().MaxScroll())
			}
		}
	}

}

func TestEvidenceRowsShowCurrentAndUpgradeVersion(t *testing.T) {
	rows := vulnerabilityRows([]security.Evidence{{Source: security.SourceEUVD, Manager: "mise", Package: "node", Version: "22.10.0", Advisory: "CVE-2026-1", UpgradeTo: "22.22.4"}})
	if len(rows) != 1 || rows[0].Current != "22.10.0" || rows[0].UpgradeTo != "22.22.4" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestVulnerabilitySummaryCountsActionableUnknownFindings(t *testing.T) {
	evidence := []security.Evidence{
		{Status: security.StatusUnknown, Advisory: "CVE-1"},
		{Status: security.StatusUnknown, Advisory: "CVE-2", Rules: []string{">=20"}},
	}
	got := vulnerabilitySummary(3, security.Summary{Vulnerable: 1, Unmapped: 1, Unknown: 2, Errors: 1}, evidence)
	want := "3 outdated; 1 vulnerable, 1 unknown, 1 security errors; 0 tool, 0 graph-only (g toggles)"
	if got != want {
		t.Fatalf("summary = %q", got)
	}
}

func TestAppendCollisionStatusReportsCompetingGlobalCommands(t *testing.T) {
	commands := []packagemanager.ExposedCommand{
		{Package: packagemanager.PackageIdentity{Manager: "brew", Name: "node"}, Name: "node", Scope: "global"},
		{Package: packagemanager.PackageIdentity{Manager: "mise", Name: "node"}, Name: "node", Scope: "global"},
		{Package: packagemanager.PackageIdentity{Manager: "npm", Name: "typescript"}, Name: "tsc", Scope: "project", Path: "/repo"},
	}
	if got, want := appendCollisionStatus("0 outdated", commands), "0 outdated; 1 command collisions: node"; got != want {
		t.Fatalf("summary = %q; want %q", got, want)
	}
}

func TestAppendManagerStatusReportsConfiguredAndRequiredManagersOnly(t *testing.T) {
	statuses := []packagemanager.ManagerStatus{
		{Name: "uv", Status: packagemanager.StatusUnavailable},
		{Name: "npm", Status: packagemanager.StatusDisabled, Evidence: packagemanager.ManagerEvidence{Executable: true}},
		{Name: "mise", Status: packagemanager.StatusError, Evidence: packagemanager.ManagerEvidence{GlobalConfig: true}},
	}
	if got, want := appendManagerStatus("0 outdated", statuses), "0 outdated; managers: npm disabled, mise error"; got != want {
		t.Fatalf("summary = %q; want %q", got, want)
	}
}

func TestQueryStatusHidesRawMultilineManagerErrors(t *testing.T) {
	if got, want := queryStatus(errors.New("mise: Set a global default version\n mise use -g go@1.27.1")), "Package inspection failed. Run `conficius packages doctor`."; got != want {
		t.Fatalf("status = %q; want %q", got, want)
	}
}

func severityColor(score float64) glyph.Color {
	row := &vulnerabilityRow{}
	row.Score = score
	buffer := glyph.NewBuffer(1, 1)
	glyph.Build(glyph.Text("x").FG(vulnerabilityColumns(row)[2].color)).Execute(buffer, 1, 1)
	return buffer.Get(0, 0).Style.FG
}

func TestSeverityColorUsesCVSSBands(t *testing.T) {
	for _, test := range []struct {
		score float64
		want  glyph.Color
	}{{10, glyph.Red}, {9, glyph.Red}, {8.99, glyph.Yellow}, {7, glyph.Yellow}, {6.99, glyph.Yellow}, {4, glyph.Yellow}, {3.99, glyph.Blue}, {0.1, glyph.Blue}, {0, glyph.Color{}}, {-1, glyph.Color{}}} {
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
		{Package: "unknown-without-rule", Status: security.StatusUnknown, Advisory: "CVE-1"},
		{Package: "unknown-rule", Status: security.StatusUnknown, Advisory: "CVE-2", Rules: []string{">=20"}},
		{Package: "affected", Status: security.StatusVulnerable, Advisory: "CVE-3"},
	})
	if len(rows) != 2 || rows[0].Package != "unknown-rule" || rows[1].Package != "affected" {
		t.Fatalf("rows = %#v", rows)
	}
}

func assertRenderedContains(t *testing.T, component glyph.Component, want string) {
	t.Helper()
	updateScreenSize(160, 20)
	defer updateScreenSize(80, 24)
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
	updateScreenSize(120, 12)
	defer updateScreenSize(80, 24)
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
	vulnerabilities, _ := vulnerabilityView(data)
	for name, component := range map[string]glyph.Component{
		"outdated":        outdated,
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
	assertRenderedContains(t, component, "Refreshing…")
	loading = false
	assertRenderedContains(t, component, "content")
}

func TestGenerationRejectsStaleRefresh(t *testing.T) {
	if isCurrentGeneration(2, 1) || !isCurrentGeneration(2, 2) {
		t.Fatal("generation comparison failed")
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

func TestGraphOnlyToggleRetainsEvidence(t *testing.T) {
	data := &model{allEvidence: []security.Evidence{
		{Package: "app", DependencyClass: "application", Status: security.StatusVulnerable, Advisory: "GO-1"},
		{Package: "tool", DependencyClass: "tool", Status: security.StatusVulnerable, Advisory: "GO-2"},
		{Package: "grpc", DependencyClass: "graph-only", Status: security.StatusVulnerable, Advisory: "GO-3"},
	}}
	data.findings = vulnerabilityRows(data.allEvidence)
	if len(data.findings) != 2 {
		t.Fatal(data.findings)
	}
	toggleGraphOnly(data)
	if len(data.findings) != 3 || !data.includeGraphOnly {
		t.Fatal(data.findings)
	}
	toggleGraphOnly(data)
	if len(data.findings) != 2 || len(data.allEvidence) != 3 {
		t.Fatal(data.findings)
	}
}
