package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jccguimaraes/conficius/internal/audit"
	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/jccguimaraes/conficius/internal/security"
	glyph "github.com/kungfusheep/glyph"
)

func TestStartupFlags(t *testing.T) {
	for _, test := range []struct {
		args    []string
		path    string
		wantErr bool
	}{
		{nil, "", false},
		{[]string{"--config", "missing.toml"}, "missing.toml", false},
		{[]string{"--config=custom.toml"}, "custom.toml", false},
		{[]string{"--config"}, "", true},
		{[]string{"--unknown"}, "", true},
		{[]string{"extra"}, "", true},
	} {
		var output bytes.Buffer
		path, err := parseStartupFlags(test.args, &output)
		if path != test.path || (err != nil) != test.wantErr {
			t.Errorf("args %v: path %q, err %v", test.args, path, err)
		}
	}
	var output bytes.Buffer
	if _, err := parseStartupFlags([]string{"--help"}, &output); !errors.Is(err, flag.ErrHelp) || !strings.Contains(output.String(), "config") {
		t.Fatalf("help: %v, %q", err, output.String())
	}
}

func TestPackageHomeSummaryStates(t *testing.T) {
	data := &model{localLoading: true, networkPending: true}
	text, _ := packageHomeSummary(data)
	for _, want := range []string{"loading", "drift unknown", "Outdated: unknown", "Security: unknown", "network pending"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	data.localLoading, data.localChecked, data.installedCount = false, true, 3
	text, _ = packageHomeSummary(data)
	if !strings.Contains(text, "3 installed; 0 drift") || strings.Contains(text, "Outdated: 0") || strings.Contains(text, "0 vulnerable") {
		t.Fatal(text)
	}
	data.outdatedChecked, data.securityScanning = true, true
	text, _ = packageHomeSummary(data)
	if !strings.Contains(text, "Outdated: 0") || !strings.Contains(text, "unknown (scanning)") {
		t.Fatal(text)
	}
	data.securityScanning, data.securityChecked, data.securityCount = false, true, 3
	data.securitySummary = security.Summary{Vulnerable: 1, Unknown: 2}
	text, color := packageHomeSummary(data)
	if !strings.Contains(text, "3 packages checked; 1 vulnerable; 2 unknown") || color != glyph.Red {
		t.Fatal(text)
	}
	data.localErr, data.outdatedErr, data.securityErr = errors.New("local"), errors.New("outdated"), errors.New("security")
	text, color = packageHomeSummary(data)
	if !strings.Contains(text, "inspection failed; partial counts") || !strings.Contains(text, "Outdated: unknown (inspection failed)") || !strings.Contains(text, "scan failed; partial counts") || color != glyph.Red {
		t.Fatal(text)
	}
}

func TestDotfileHomeSummarySeverityAndPath(t *testing.T) {
	state := &dotfileState{project: &config.Project{}, configPath: "/tmp/custom.toml", environment: "Environment: xdg", links: "Links: 1 healthy"}
	text, color := dotfileHomeSummary(state)
	if !strings.Contains(text, state.configPath) || !strings.Contains(text, state.environment) || !strings.Contains(text, state.links) || color != glyph.Green {
		t.Fatal(text)
	}
	state.findings = []audit.Finding{{Severity: audit.SeverityWarning}}
	if _, color := dotfileHomeSummary(state); color != glyph.Yellow {
		t.Fatal("warning must be yellow")
	}
	state.rows = []dotfileRow{{group: 1}}
	if _, color := dotfileHomeSummary(state); color != glyph.Red {
		t.Fatal("blocked must be red")
	}
	state.configErr = errors.New("invalid configuration")
	text, color = dotfileHomeSummary(state)
	if !strings.Contains(text, "invalid configuration") || color != glyph.Red {
		t.Fatal(text)
	}
}

func TestEvaluateInventorySharesProfileSelection(t *testing.T) {
	profile := config.Profile{Mode: "allowlist", Packages: []config.PackageRule{{Manager: "npm", Name: "tool", Policy: "include", DesiredVersion: "2.0.0"}}}
	selected, drift := evaluateInventory(profile, []packagemanager.InstalledPackage{{Manager: "npm", Name: "tool", Version: "1.0.0", Scope: "global"}, {Manager: "npm", Name: "other", Version: "1.0.0", Scope: "global"}})
	if len(selected) != 1 || selected[0].Name != "tool" || len(drift) != 1 {
		t.Fatalf("selected %v, drift %v", selected, drift)
	}
}

func TestHomeRendersTwoVerticalCards(t *testing.T) {
	data := &model{dotfiles: &dotfileState{project: &config.Project{}, configPath: "/tmp/custom.toml", environment: "Environment: xdg", links: "Links: 1 healthy"}, localLoading: true}
	component, _ := homeView(data, glyph.NewApp())
	template := glyph.Build(component)
	buffer := glyph.NewBuffer(100, 24)
	template.Execute(buffer, 100, 24)
	output := homeRenderOutput(buffer, 24)
	dotfiles, packages := strings.Index(output, "Dotfiles"), strings.Index(output, "Packages")
	if dotfiles < 0 || packages <= dotfiles || !strings.Contains(output[dotfiles:packages], "Config: /tmp/custom.toml") || !strings.Contains(output[packages:], "Local inventory: loading; drift unknown") {
		t.Fatalf("card identities or vertical summaries missing:\n%s", output)
	}
	for _, want := range []string{"Environment: xdg", "Links: 1 healthy", "Outdated: unknown (network pending)", "Security: unknown (network pending)", "r full refresh"} {
		if strings.Count(output, want) != 1 {
			t.Fatalf("expected one %q:\n%s", want, output)
		}
	}

	// Apply refreshed state in place, as async dotfiles completion does.
	*data.dotfiles = dotfileState{project: &config.Project{}, configPath: "/tmp/refreshed.toml", environment: "Environment: native", links: "Links: 2 blocked", rows: []dotfileRow{{group: 1}}}
	data.localLoading, data.localChecked, data.installedCount = false, true, 7
	data.outdatedChecked, data.securityChecked, data.securityCount = true, true, 7
	data.securitySummary = security.Summary{Vulnerable: 1}
	updateHome(data)
	buffer = glyph.NewBuffer(100, 24)
	// Reuse compiled template; rebuilding would hide stale binding bugs.
	template.Execute(buffer, 100, 24)
	output = homeRenderOutput(buffer, 24)
	for _, want := range []string{"Config: /tmp/refreshed.toml", "Environment: native", "Links: 2 blocked", "Local inventory: 7 installed; 0 drift", "Outdated: 0", "Security: 7 packages checked; 1 vulnerable; 0 unknown; 0 errors"} {
		if !strings.Contains(output, want) {
			t.Fatalf("refreshed output missing %q:\n%s", want, output)
		}
		if want != "Environment: native" && want != "Config: /tmp/refreshed.toml" {
			for row := range 24 {
				if column := strings.Index(buffer.GetLine(row), want); column >= 0 && buffer.Get(utf8.RuneCountInString(buffer.GetLine(row)[:column]), row).Style.FG != glyph.Red {
					t.Fatalf("refreshed severity not red for %q", want)
				}
			}
		}
	}
	if strings.Contains(output, "custom.toml") || strings.Contains(output, "network pending") || strings.Contains(output, "loading") {
		t.Fatalf("stale summary:\n%s", output)
	}
}

func TestHomeWrapsLongConfigPath(t *testing.T) {
	path := "/tmp/" + strings.Repeat("long-config-directory/", 5) + "conficius.toml"
	data := &model{dotfiles: &dotfileState{project: &config.Project{}, configPath: path, environment: "Environment: xdg", links: "Links: 1 healthy"}}
	component, _ := homeView(data, glyph.NewApp())
	buffer := glyph.NewBuffer(60, 30)
	glyph.Build(component).Execute(buffer, 60, 30)
	output := homeRenderOutput(buffer, 30)
	// Strip borders and list markers, then join wrapped path fragments.
	var content strings.Builder
	for row := range 30 {
		line := strings.Trim(buffer.GetLine(row), " │>╭╮╰╯─")
		content.WriteString(line)
	}
	if !strings.Contains(content.String(), "Config: "+path) || !strings.Contains(output, "Environment: xdg") || !strings.Contains(output, "Links: 1 healthy") || !strings.Contains(output, "Packages") {
		t.Fatalf("long config path clipped or displaced cards:\n%s", output)
	}
}

func homeRenderOutput(buffer *glyph.Buffer, height int) string {
	lines := make([]string, height)
	for row := range height {
		lines[row] = buffer.GetLine(row)
	}
	return strings.Join(lines, "\n")
}
