package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
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
	for _, want := range []string{"loading", "drift unknown", "Outdated: unknown", "Security: unknown"} {
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
	if !strings.Contains(text, "3 checked; 1 vulnerable; 2 unknown") || color != glyph.Red {
		t.Fatal(text)
	}
	data.localErr, data.outdatedErr, data.securityErr = errors.New("local"), errors.New("outdated"), errors.New("security")
	text, color = packageHomeSummary(data)
	if !strings.Contains(text, "partial; failed") || !strings.Contains(text, "Outdated: unknown (inspection failed)") || color != glyph.Red {
		t.Fatal(text)
	}
}

func TestDotfileHomeSummarySeverityAndPath(t *testing.T) {
	state := &dotfileState{project: &config.Project{}, configPath: "/tmp/custom.toml", environment: "Environment: xdg", links: "Links: 1 healthy"}
	text, color := dotfileHomeSummary(state)
	if strings.Contains(text, state.configPath) || !strings.Contains(text, "xdg") || !strings.Contains(text, state.links) || color != glyph.Green {
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

func TestHomeRendersQuietVerticalMenu(t *testing.T) {
	previous := screenSize
	t.Cleanup(func() { updateScreenSize(previous.Width, previous.Height) })
	for _, size := range []glyph.Size{{Width: 40, Height: 12}, {Width: 80, Height: 24}, {Width: 120, Height: 30}} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			updateScreenSize(size.Width, size.Height)
			path := "/tmp/" + strings.Repeat("long-config-directory/", 8) + "conficius.toml"
			data := &model{dotfiles: &dotfileState{project: &config.Project{}, configPath: path, environment: "Environment: xdg", links: "Links: 1 healthy"}, localLoading: true}
			component, cards := homeView(data, glyph.NewApp())
			template := glyph.Build(component)
			buffer := glyph.NewBuffer(size.Width, size.Height)
			template.Execute(buffer, int16(size.Width), int16(size.Height))
			output := homeRenderOutput(buffer, size.Height)
			for _, want := range []string{"Conficius / Home", version, "Config: conficius.toml", "Dotfiles", "Packages", "Healthy", "Loading", "xdg", "Links: 1 healthy", "Local: loading; drift unknown", "Outdated: unknown", "Security: unknown", "? help"} {
				if strings.Count(output, want) != 1 {
					t.Fatalf("expected one %q:\n%s", want, output)
				}
			}
			if strings.Contains(output, "long-config-directory") || strings.Count(output, "╭") > 1 || strings.Count(output, "╰") > 1 {
				t.Fatalf("long path or nested borders:\n%s", output)
			}
			dotfilesRow, packagesRow := -1, -1
			for row := range size.Height {
				line := buffer.GetLine(row)
				if strings.Contains(line, "Dotfiles") {
					dotfilesRow = row
				}
				if strings.Contains(line, "Packages") {
					packagesRow = row
				}
				for column := range size.Width {
					cell := buffer.Get(column, row)
					if cell.Style.BG.Mode != glyph.ColorDefault || cell.Style.FG.Mode == glyph.ColorRGB {
						t.Fatalf("non-terminal palette at %d,%d: %+v", column, row, cell.Style)
					}
				}
				for _, name := range []string{"Dotfiles", "Packages"} {
					if column := strings.Index(line, name); column >= 0 {
						style := buffer.Get(utf8.RuneCountInString(line[:column]), row).Style
						if style.Attr&glyph.AttrBold == 0 || name == "Dotfiles" && style.FG != glyph.Cyan || name == "Packages" && style.FG.Mode != glyph.ColorDefault {
							t.Fatalf("name style %s: %+v", name, style)
						}
					}
				}
				for _, label := range []struct {
					text  string
					color glyph.Color
				}{{"Healthy", glyph.Green}, {"Loading", glyph.Cyan}} {
					if column := strings.Index(line, label.text); column >= 0 {
						style := buffer.Get(utf8.RuneCountInString(line[:column]), row).Style
						if style.FG != label.color || style.Attr&glyph.AttrBold != 0 {
							t.Fatalf("status style %s: %+v", label.text, style)
						}
					}
				}
				for _, summary := range []string{"xdg", "Local:", "Outdated:", "Security:"} {
					if column := strings.Index(line, summary); column >= 0 {
						style := buffer.Get(utf8.RuneCountInString(line[:column]), row).Style
						if style.FG.Mode != glyph.ColorDefault || style.Attr&glyph.AttrBold != 0 {
							t.Fatalf("summary style: %+v", style)
						}
					}
				}
			}
			if dotfilesRow < 0 || packagesRow <= dotfilesRow+2 || strings.Trim(buffer.GetLine(packagesRow-1), " │") != "" {
				t.Fatalf("missing blank gap:\n%s", output)
			}
			cards.Down(nil)
			*data.dotfiles = dotfileState{project: &config.Project{}, configPath: "/tmp/refreshed.toml", environment: "Environment: native", links: "Links: 2 blocked", rows: []dotfileRow{{group: 1}}}
			data.localLoading, data.localChecked, data.installedCount = false, true, 7
			data.drift = []config.PackageState{{}}
			updateHome(data)
			buffer = glyph.NewBuffer(size.Width, size.Height)
			template.Execute(buffer, int16(size.Width), int16(size.Height))
			output = homeRenderOutput(buffer, size.Height)
			for _, want := range []string{"Config: refreshed.toml", "native", "Links: 2 blocked", "Local: 7 installed; 1 drift", "Error", "Attention"} {
				if !strings.Contains(output, want) {
					t.Fatalf("stale binding %q:\n%s", want, output)
				}
			}
			for row := range size.Height {
				line := buffer.GetLine(row)
				if column := strings.Index(line, "Packages"); column >= 0 && buffer.Get(utf8.RuneCountInString(line[:column]), row).Style.FG != glyph.Cyan {
					t.Fatalf("selected name not cyan:\n%s", output)
				}
				for _, label := range []struct {
					text  string
					color glyph.Color
				}{{"Error", glyph.Red}, {"Attention", glyph.Yellow}} {
					if column := strings.Index(line, label.text); column >= 0 && buffer.Get(utf8.RuneCountInString(line[:column]), row).Style.FG != label.color {
						t.Fatalf("stale status color %s:\n%s", label.text, output)
					}
				}
			}
		})
	}
}

func homeRenderOutput(buffer *glyph.Buffer, height int) string {
	lines := make([]string, height)
	for row := range height {
		lines[row] = buffer.GetLine(row)
	}
	return strings.Join(lines, "\n")
}
