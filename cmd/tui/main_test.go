package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jccguimaraes/conficius/internal/vulnerability"
	"strings"
	"testing"
	"time"

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

func TestScopeTogglesRescanSelectedInventory(t *testing.T) {
	data := &model{inventory: []packagemanager.InstalledPackage{
		{Manager: "go", Name: "direct", DependencyClass: "application"},
		{Manager: "go", Name: "indirect", DependencyClass: "test", Transitive: true},
		{Manager: "go", Name: "graph", DependencyClass: "graph-only", Transitive: true},
	}}
	calls := 0
	rescan := func() {
		calls++
		selected := security.SelectInventory(data.inventory, security.Options{IncludeIndirect: data.includeIndirect, IncludeGraphOnly: data.includeGraphOnly})
		want := 1
		if data.includeIndirect {
			want++
		}
		if data.includeGraphOnly {
			want++
		}
		if len(selected) != want {
			t.Fatalf("selected = %#v", selected)
		}
	}
	toggleIndirect(data, rescan)
	toggleGraphOnly(data, rescan)
	toggleIndirect(data, rescan)
	toggleGraphOnly(data, rescan)
	if calls != 4 || data.includeIndirect || data.includeGraphOnly {
		t.Fatal(data, calls)
	}
}

type scopeQueryClient struct{ requests chan string }

func (c scopeQueryClient) Query(_ context.Context, pkg vulnerability.Package) ([]vulnerability.Vulnerability, error) {
	c.requests <- pkg.Name
	return nil, nil
}

func TestSecurityToggleStartsRealExpandedScan(t *testing.T) {
	requests := make(chan string, 8)
	data := &model{localChecked: true, outdatedChecked: true, installedCount: 3,
		osvClient: scopeQueryClient{requests: requests},
		inventory: []packagemanager.InstalledPackage{
			{Manager: "go", Name: "example.com/direct", Version: "v1.0.0", OSVEcosystem: "Go", OSVName: "example.com/direct", DependencyClass: "application"},
			{Manager: "go", Name: "example.com/indirect", Version: "v1.0.0", OSVEcosystem: "Go", OSVName: "example.com/indirect", DependencyClass: "test", Transitive: true},
			{Manager: "go", Name: "example.com/graph", Version: "v1.0.0", OSVEcosystem: "Go", OSVName: "example.com/graph", DependencyClass: "graph-only", Transitive: true},
		},
	}
	app := glyph.NewApp()
	for _, graph := range []bool{false, true} {
		if graph {
			toggleGraphOnly(data, func() { refreshSecurity(data, app) })
		} else {
			toggleIndirect(data, func() { refreshSecurity(data, app) })
		}
		want := 2
		if graph {
			want = 3
		}
		seen := map[string]bool{}
		for range want {
			select {
			case name := <-requests:
				seen[name] = true
			case <-time.After(time.Second):
				t.Fatal("toggle did not start selected scan")
			}
		}
		if !seen["example.com/indirect"] || seen["example.com/graph"] != graph {
			t.Fatal(seen)
		}
		if !data.loading || !data.securityScanning || !strings.Contains(data.status, "Busy: checking security") {
			t.Fatal(data.status)
		}
	}
	data.securityCancel()
	if data.securityGeneration != 2 {
		t.Fatal(data.securityGeneration)
	}
}

// Requests stay blocked until cancellation or explicit release, including clients
// that return successful results after cancellation.
type securityRequest struct {
	ctx      context.Context
	release  chan struct{}
	returned chan struct{}
}

type blockingSecurityClient struct {
	requests chan securityRequest
}

func (c blockingSecurityClient) Query(ctx context.Context, _ vulnerability.Package) ([]vulnerability.Vulnerability, error) {
	request := securityRequest{ctx, make(chan struct{}), make(chan struct{})}
	c.requests <- request
	<-request.release
	close(request.returned)
	return nil, nil
}

func waitSecurityApply(t *testing.T, app *glyph.App, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.RenderNow()
		if ready() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("security callback not applied")
}

func TestCancelSecurityAndRestartRejectsLateResponse(t *testing.T) {
	client := blockingSecurityClient{make(chan securityRequest, 4)}
	parent, stop := context.WithCancel(context.Background())
	defer stop()
	data := &model{localChecked: true, outdatedChecked: true, cancel: stop,
		osvClient: client, generation: 7,
		inventory:   []packagemanager.InstalledPackage{{Manager: "go", Name: "example.com/direct", Version: "v1.0.0", OSVEcosystem: "Go", OSVName: "example.com/direct"}},
		allEvidence: []security.Evidence{{Package: "previous", Status: security.StatusVulnerable}},
		findings:    []vulnerabilityRow{{Evidence: security.Evidence{Package: "previous"}}},
	}
	app := glyph.NewApp()
	refreshSecurity(data, app)
	var first securityRequest
	select {
	case first = <-client.requests:
	case <-time.After(time.Second):
		t.Fatal("scan did not start")
	}
	cancelSecurity(data, app)
	select {
	case <-first.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("request context not canceled")
	}
	if parent.Err() != nil || data.generation != 7 || data.securityChecked || data.securityScanning || data.loading || !errors.Is(data.securityErr, context.Canceled) {
		t.Fatalf("cancellation state: %+v", data)
	}
	if len(data.allEvidence) != 1 || len(data.findings) != 1 || data.status != "Vulnerability scan canceled; results incomplete" {
		t.Fatal("cancellation discarded results or hid incomplete state")
	}
	summary, color := packageHomeSummary(data)
	if !strings.Contains(summary, "Security: canceled; results incomplete") || color == glyph.Red {
		t.Fatalf("home: %s, %v", summary, color)
	}
	cancelSecurity(data, app)
	if data.status != "Vulnerability scan canceled; results incomplete" {
		t.Fatal(data.status)
	}
	refreshSecurity(data, app)
	var second securityRequest
	select {
	case second = <-client.requests:
	case <-time.After(time.Second):
		t.Fatal("restart did not start")
	}
	close(first.release)
	<-first.returned
	// Drain late old completion before releasing replacement scan.
	for range 20 {
		app.RenderNow()
		time.Sleep(time.Millisecond)
	}
	if data.securityChecked || !data.securityScanning || len(data.allEvidence) != 1 {
		t.Fatal("late response overwrote replacement scan")
	}
	close(second.release)
	<-second.returned
	waitSecurityApply(t, app, func() bool { return data.securityChecked })
	if data.securityScanning || data.securityErr != nil || data.securityCancel != nil {
		t.Fatal("replacement scan failed to complete")
	}
}

func TestCancelSecurityWithoutActiveScanPreservesPackageWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	data := &model{cancel: cancel, loading: true, localLoading: true, networkPending: true}
	cancelSecurity(data, glyph.NewApp())
	if ctx.Err() != nil || !data.loading || !data.localLoading || !data.networkPending || data.status != "No vulnerability scan running" {
		t.Fatalf("inactive cancellation changed package work: %+v", data)
	}
}

func TestFullRefreshSecurityCancelCannotResurrect(t *testing.T) {
	t.Setenv(config.ProfileEnvironment, "")
	root := t.TempDir()
	// Native commands are local fixtures; no inventory or network dependency.
	npm := `#!/bin/sh
case "$1" in
  --version) printf '10.0.0' ;;
  ls) printf '{"dependencies":{"example":{"version":"1.0.0"}}}' ;;
  outdated) printf '{"example":{"current":"1.0.0","wanted":"2.0.0","latest":"2.0.0"}}' ;;
esac
`
	for name, content := range map[string]string{
		"npm":               npm,
		"package.json":      `{"dependencies":{"example":"1.0.0"}}`,
		"package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{"example":"1.0.0"}},"node_modules/example":{"version":"1.0.0"}}}`,
		"packages.toml":     fmt.Sprintf("version = 2\n[[profiles.default.projects]]\nname = \"test\"\npath = %q\n", root),
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", root)
	data, err := startupModel("", filepath.Join(root, "packages.toml"))
	if err != nil || data.packageConfigErr != nil {
		t.Fatalf("startup: %v, %v", err, data.packageConfigErr)
	}
	data.active = &data.scopes[1]
	client := blockingSecurityClient{make(chan securityRequest, 4)}
	data.osvClient = client
	app := glyph.NewApp()
	refresh(data, app)
	var request securityRequest
	select {
	case request = <-client.requests:
	case <-time.After(2 * time.Second):
		t.Fatal("full refresh did not start security request")
	}
	waitSecurityApply(t, app, func() bool { return data.securityScanning })
	if !data.localChecked || !data.outdatedChecked || len(data.packages) != 1 {
		t.Fatal("package results missing before cancellation")
	}
	cancelSecurity(data, app)
	select {
	case <-request.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("full refresh security request not canceled")
	}
	close(request.release)
	<-request.returned
	for range 20 {
		app.RenderNow()
		time.Sleep(time.Millisecond)
	}
	if data.securityChecked || data.securityScanning || data.loading || !errors.Is(data.securityErr, context.Canceled) || len(data.packages) != 1 || !data.outdatedChecked {
		t.Fatalf("full refresh resurrected scan or lost outdated results: %+v", data)
	}
}

func TestSecurityGenerationRejectsQueuedCompletion(t *testing.T) {
	client := blockingSecurityClient{make(chan securityRequest, 1)}
	data := &model{localChecked: true, outdatedChecked: true, osvClient: client,
		inventory: []packagemanager.InstalledPackage{{Manager: "go", Name: "example.com/direct", Version: "v1.0.0", OSVEcosystem: "Go", OSVName: "example.com/direct"}},
	}
	app := glyph.NewApp()
	refreshSecurity(data, app)
	var request securityRequest
	select {
	case request = <-client.requests:
	case <-time.After(time.Second):
		t.Fatal("scan did not start")
	}
	stop := data.securityCancel
	defer stop()
	// Leave context live: generation alone must reject stale Apply callbacks.
	data.securityCancel = func() {}
	cancelSecurity(data, app)
	close(request.release)
	<-request.returned
	for range 20 {
		app.RenderNow()
		time.Sleep(time.Millisecond)
	}
	if data.securityChecked || data.securityScanning || !errors.Is(data.securityErr, context.Canceled) {
		t.Fatal("stale generation marked canceled scan complete")
	}
}

func TestSecurityCancelFooterTracksActiveScan(t *testing.T) {
	data := &model{securityScanning: true}
	results, _ := resultView(data)
	vulnerabilities, _ := vulnerabilityView(data)
	assertRenderedContains(t, results, "x cancel scan")
	assertRenderedContains(t, vulnerabilities, "x cancel scan")
	data.securityScanning = false
	assertRenderedContains(t, results, "Space mark")
	assertRenderedContains(t, vulnerabilities, "i indirect")
}
