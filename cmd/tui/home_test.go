package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	for _, want := range []string{"loading", "Outdated: unknown", "Security: unknown"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	data.localLoading, data.localChecked, data.installedCount = false, true, 3
	text, _ = packageHomeSummary(data)
	if !strings.Contains(text, "3 installed") || strings.Contains(text, "Outdated: 0") || strings.Contains(text, "0 vulnerable") {
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

func TestHomeWisdomStaysFixedAcrossResize(t *testing.T) {
	previous := screenSize
	t.Cleanup(func() { updateScreenSize(previous.Width, previous.Height) })
	data := &model{}
	component, _ := homeView(data, glyph.NewApp())
	picked := data.wisdom
	if !strings.HasPrefix(picked, "Conficius says: ") {
		t.Fatalf("unexpected wisdom: %q", picked)
	}
	template := glyph.Build(component)
	for _, width := range []int{120, 40, 80, 120} {
		updateScreenSize(width, 30)
		updateHome(data)
		buffer := glyph.NewBuffer(width, 30)
		template.Execute(buffer, int16(width), 30)
		output := homeRenderOutput(buffer, 30)
		if strings.Contains(output, "Conficius says:") != (width >= 80) {
			t.Fatalf("wrong wisdom visibility at width %d:\n%s", width, output)
		}
		if data.wisdom != picked {
			t.Fatal("refresh changed wisdom")
		}
	}
}

func TestHomeConfigErrorsRemainVisible(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
		err  error
	}{
		{name: "explicit missing config", path: "missing.toml", err: config.ErrNotFound},
		{name: "invalid config", path: "conficius.toml", err: errors.New("invalid config")},
		{name: "discovery failure", err: errors.New("get working directory: permission denied")},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := &model{dotfiles: newDotfileState(nil, test.err, test.path)}
			homeView(data, glyph.NewApp())
			if len(data.homeCards) != 2 || data.homeCards[0].view != "scopes" || data.homeCards[1].label != "Error" {
				t.Fatalf("config error hidden: %+v", data.homeCards)
			}
		})
	}
}

func TestHomeWithoutConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, discoverErr := config.Discover("")
	project, loadErr := config.LoadDiscovered("")
	data := &model{dotfiles: newDotfileState(project, errors.Join(discoverErr, loadErr), path)}
	component, cards := homeView(data, glyph.NewApp())
	buffer := glyph.NewBuffer(80, 24)
	glyph.Build(component).Execute(buffer, 80, 24)
	output := homeRenderOutput(buffer, 24)
	if len(data.homeCards) != 2 || cards.Selected().view != "scopes" || data.homeCards[1].color == glyph.Red {
		t.Fatalf("expected packages first and neutral dotfiles: %+v", data.homeCards)
	}
	if !strings.Contains(output, "Packages") || !strings.Contains(output, "Config: none") || !strings.Contains(output, "Optional") || !strings.Contains(output, "Add your dotfiles when you're ready.") || !strings.Contains(output, "Run conficius init in your dotfiles repo.") || strings.Contains(output, "Not detected") || strings.Contains(output, "Error") {
		t.Fatalf("missing config should show neutral notice:\n%s", output)
	}
}

func TestHomeRendersQuietVerticalMenu(t *testing.T) {
	previous := screenSize
	t.Cleanup(func() { updateScreenSize(previous.Width, previous.Height) })
	for _, size := range []glyph.Size{{Width: 40, Height: 12}, {Width: 80, Height: 24}, {Width: 120, Height: 30}} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			updateScreenSize(size.Width, size.Height)
			path := "/tmp/" + strings.Repeat("long-config-directory/", 8) + "conficius.toml"
			data := &model{wisdom: "Conficius says: To make a mistake and not roll it back is the real mistake.", dotfiles: &dotfileState{project: &config.Project{}, configPath: path, environment: "Environment: xdg", links: "Links: 1 healthy"}, localLoading: true}
			component, cards := homeView(data, glyph.NewApp())
			template := glyph.Build(component)
			buffer := glyph.NewBuffer(size.Width, size.Height)
			template.Execute(buffer, int16(size.Width), int16(size.Height))
			output := homeRenderOutput(buffer, size.Height)
			for _, want := range []string{"Conficius / Home", version, "Config: conficius.toml", "Dotfiles", "Packages", "Healthy", "Loading", "xdg", "Links: 1 healthy", "Local: loading", "Outdated: unknown", "Security: unknown", "? help"} {
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
						if style.Attr&glyph.AttrBold == 0 || name == "Packages" && style.FG != glyph.Cyan || name == "Dotfiles" && style.FG.Mode != glyph.ColorDefault {
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
			if packagesRow < 0 || dotfilesRow <= packagesRow+2 || strings.Trim(buffer.GetLine(dotfilesRow-1), " │") != "" {
				t.Fatalf("missing blank gap:\n%s", output)
			}
			cards.Down(nil)
			*data.dotfiles = dotfileState{project: &config.Project{}, configPath: "/tmp/refreshed.toml", environment: "Environment: native", links: "Links: 2 blocked", rows: []dotfileRow{{group: 1}}}
			data.localLoading, data.localChecked, data.installedCount = false, true, 7
			updateHome(data)
			buffer = glyph.NewBuffer(size.Width, size.Height)
			template.Execute(buffer, int16(size.Width), int16(size.Height))
			output = homeRenderOutput(buffer, size.Height)
			for _, want := range []string{"Config: refreshed.toml", "native", "Links: 2 blocked", "Local: 7 installed", "Error", "Healthy"} {
				if !strings.Contains(output, want) {
					t.Fatalf("stale binding %q:\n%s", want, output)
				}
			}
			for row := range size.Height {
				line := buffer.GetLine(row)
				if column := strings.Index(line, "Dotfiles"); column >= 0 && buffer.Get(utf8.RuneCountInString(line[:column]), row).Style.FG != glyph.Cyan {
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

func TestSeparateConfigFlags(t *testing.T) {
	var output bytes.Buffer
	dotfiles, packages, err := parseConfigFlags([]string{"--config=dotfiles.toml", "--package-config=packages.toml"}, &output)
	if err != nil || dotfiles != "dotfiles.toml" || packages != "packages.toml" {
		t.Fatalf("flags: %q %q %v", dotfiles, packages, err)
	}
}

func TestStartupConfigsIndependent(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dotfilePath := filepath.Join(t.TempDir(), "dotfiles.toml")
	packagePath := filepath.Join(t.TempDir(), "packages.toml")
	if err := os.WriteFile(dotfilePath, []byte("version = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packagePath, []byte(fmt.Sprintf("version = %d\nlayout = \"os-native\"\n", config.CurrentVersion)), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := startupModel(dotfilePath, packagePath)
	if err != nil {
		t.Fatal(err)
	}
	if data.dotfiles.packageConfigPath != packagePath {
		t.Fatalf("explicit package config lost: %q", data.dotfiles.packageConfigPath)
	}
	if data.packageConfigErr != nil || data.dotfiles.project == nil || data.dotfiles.configErr != nil || string(data.dotfiles.mode) != "os-native" {
		t.Fatalf("package error: %v; dotfile error: %v; mode: %s", data.packageConfigErr, data.dotfiles.configErr, data.dotfiles.mode)
	}
	if err := os.WriteFile(packagePath, []byte("invalid ["), 0600); err != nil {
		t.Fatal(err)
	}
	data, err = startupModel(dotfilePath, packagePath)
	if err != nil {
		t.Fatal(err)
	}
	if data.dotfiles.packageConfigPath != packagePath {
		t.Fatalf("invalid explicit package config lost: %q", data.dotfiles.packageConfigPath)
	}
	if data.packageConfigErr == nil || data.dotfiles.project == nil || data.dotfiles.configErr != nil {
		t.Fatalf("package error affected dotfiles: %+v", data)
	}
	text, color := packageHomeSummary(data)
	if color != glyph.Red || !strings.Contains(text, "Package configuration error:") {
		t.Fatal(text)
	}
	data, err = startupModel(filepath.Join(t.TempDir(), "missing.toml"), "")
	if err != nil {
		t.Fatal(err)
	}
	if data.dotfiles.packageConfigPath != "" {
		t.Fatalf("implicit package config marked explicit: %q", data.dotfiles.packageConfigPath)
	}
	if data.packageConfigErr != nil || data.dotfiles.configErr == nil || string(data.dotfiles.mode) != "xdg" {
		t.Fatalf("dotfiles error affected packages: %+v", data)
	}
}

func TestReloadPackageConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(config.ProfileEnvironment, "")
	path := filepath.Join(t.TempDir(), "explicit.toml")
	first, second := t.TempDir(), t.TempDir()
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	project := func(name, path string) string {
		return fmt.Sprintf("[[profiles.default.projects]]\nname = %q\npath = %q\n", name, path)
	}
	base := fmt.Sprintf("version = %d\n", config.CurrentVersion)
	write(base + project("first", first))
	data, err := startupModel("", path)
	if err != nil || data.packageConfigErr != nil {
		t.Fatalf("startup: %v, %v", err, data.packageConfigErr)
	}
	if data.packageConfigPath != path || len(data.scopes) != 2 {
		t.Fatalf("explicit startup: path %q, scopes %+v", data.packageConfigPath, data.scopes)
	}
	data.active = &data.scopes[1]
	oldActive := data.active
	updated := base + `layout = "os-native"
[profiles.default]
[profiles.default.managers.npm]
enabled = false
[[vulnerabilities.mappings]]
manager = "mise"
package = "tool"
ecosystem = "npm"
name = "renamed-tool"
[[vulnerabilities.euvd]]
manager = "npm"
package = "tool"
product = "product"
vendor = "vendor"
` + project("second", second) + project("renamed-first", first)
	write(updated)
	if err := reloadPackageConfig(data); err != nil {
		t.Fatal(err)
	}
	if len(data.scopes) != 3 || data.active != &data.scopes[2] || data.active == oldActive || data.active.Name != "renamed-first" {
		t.Fatalf("scope addition/remap: %+v, active %+v", data.scopes, data.active)
	}
	profile := data.profile.Profile
	if *profile.Managers["npm"].Enabled {
		t.Fatalf("policy not reloaded: %+v", profile)
	}
	if len(data.mappings) != 1 || data.mappings[0].Name != "renamed-tool" || len(data.euvdMaps) != 1 || data.euvdMaps[0].Product != "product" || string(data.dotfiles.mode) != "os-native" {
		t.Fatal("mappings or layout not reloaded")
	}
	lastProfile, lastScopes, lastActive := data.profile, data.scopes, data.active
	lastMappings, lastEUVD, lastMode := data.mappings, data.euvdMaps, data.dotfiles.mode
	for _, invalid := range []string{
		"invalid [",
		base + "profile = 'missing'\n",
		base + project("missing", filepath.Join(t.TempDir(), "absent")),
	} {
		write(invalid)
		if err := reloadPackageConfig(data); err == nil || data.packageConfigErr == nil {
			t.Fatal("invalid config accepted")
		}
		if !reflect.DeepEqual(data.profile, lastProfile) || !reflect.DeepEqual(data.scopes, lastScopes) || data.active != lastActive || !reflect.DeepEqual(data.mappings, lastMappings) || !reflect.DeepEqual(data.euvdMaps, lastEUVD) || data.dotfiles.mode != lastMode {
			t.Fatal("failed reload replaced last valid state")
		}
	}
	write(base + project("second", second))
	if err := reloadPackageConfig(data); err != nil || data.packageConfigErr != nil {
		t.Fatalf("repair: %v, %v", err, data.packageConfigErr)
	}
	if len(data.scopes) != 2 || data.active != nil || len(data.mappings) != 0 || len(data.euvdMaps) != 0 || string(data.dotfiles.mode) != "xdg" {
		t.Fatalf("removed config remains: %+v", data)
	}
}

func TestRefreshPackagesAbortsInvalidConfig(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(fmt.Sprintf("full=%t", full), func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv(config.ProfileEnvironment, "")
			path := filepath.Join(t.TempDir(), "packages.toml")
			if err := os.WriteFile(path, []byte(fmt.Sprintf("version = %d\n", config.CurrentVersion)), 0600); err != nil {
				t.Fatal(err)
			}
			data, err := startupModel("", path)
			if err != nil || data.packageConfigErr != nil {
				t.Fatalf("startup: %v, %v", err, data.packageConfigErr)
			}
			oldScopes := data.scopes
			data.loading, data.localLoading, data.securityScanning = true, true, true
			data.generation = 7
			data.selected["stale"] = true
			data.plans = []packagemanager.UpdatePlan{{Executable: "/must-not-run"}}
			data.commands = []commandRow{{Command: "stale"}}
			data.preview, data.planSummary = "stale", "stale"
			data.homeCards = []homeCard{{view: "scopes"}}
			cancelled := false
			data.cancel = func() {
				cancelled = true
				// Cancellation must precede reload, so reload sees this failure.
				if err := os.WriteFile(path, []byte("invalid ["), 0600); err != nil {
					t.Fatal(err)
				}
			}
			refreshPackages(data, glyph.NewApp(), full)
			if !cancelled || data.generation != 8 || data.loading || data.localLoading || data.securityScanning || data.localChecked || data.outdatedChecked {
				t.Fatalf("refresh did not abort before manager queries: %+v", data)
			}
			if data.packageConfigErr == nil || !strings.Contains(data.status, "Package configuration error:") || data.homeCards[0].color != glyph.Red {
				t.Fatalf("error hidden: %q", data.status)
			}
			if !reflect.DeepEqual(data.scopes, oldScopes) || len(data.selected) != 0 || len(data.plans) != 0 || len(data.commands) != 0 || data.preview != "" || data.planSummary != "" {
				t.Fatal("stale selection/plans retained or valid scopes lost")
			}
		})
	}
}

func TestSessionProfileSwitchAndMissingSelection(t *testing.T) {
	t.Setenv(config.ProfileEnvironment, "work")
	path := filepath.Join(t.TempDir(), "packages.toml")
	content := "version = 2\n[profiles.work]\n[profiles.personal.managers.npm]\nenabled = false\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := startupModel("", path)
	if err != nil || data.packageConfigErr != nil {
		t.Fatalf("startup: %v, %v", err, data.packageConfigErr)
	}
	if data.profile.Name != "work" {
		t.Fatal(data.profile.Name)
	}
	data.active = &data.scopes[0]
	data.selected["old"], data.failed["old"], data.attempted["old"] = true, "failed", "1"
	data.plans = []packagemanager.UpdatePlan{{Executable: "must-not-run"}}
	data.rows = []outdatedRow{{}}
	data.findings = []vulnerabilityRow{{}}
	data.packages = []packagemanager.Package{{Name: "stale"}}
	switchProfile(data, "personal")
	if data.active != nil || len(data.selected)+len(data.failed)+len(data.attempted)+len(data.plans)+len(data.rows)+len(data.findings)+len(data.packages) != 0 {
		t.Fatal("switch retained stale state")
	}
	for range 2 {
		if err := reloadPackageConfig(data); err != nil || data.profile.Name != "personal" {
			t.Fatalf("override lost: %v, %s", err, data.profile.Name)
		}
	}
	if os.Getenv(config.ProfileEnvironment) != "work" {
		t.Fatal("environment changed")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != content {
		t.Fatal("profile switch wrote config")
	}
	view, _ := profileView(data, glyph.NewApp())
	assertRenderedContains(t, view, "default")
	assertRenderedContains(t, view, "personal")
	assertRenderedContains(t, view, "(active)")
	if err := os.WriteFile(path, []byte("version = 2\n[profiles.work]\n[profiles.new]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	refreshPackages(data, glyph.NewApp(), false)
	if data.packageConfigErr == nil || !strings.Contains(data.status, "personal") || !reflect.DeepEqual(data.profiles, []string{"default", "new", "work"}) {
		t.Fatalf("missing selection not visible: %v, %s, %v", data.packageConfigErr, data.status, data.profiles)
	}
	prepareUpgrade(data, glyph.NewApp(), true)
	applyUpgrades(data, glyph.NewApp())
	if data.runInteractive || len(data.plans) != 0 || !strings.Contains(data.status, "blocked") {
		t.Fatal("missing profile allowed mutations")
	}
	switchProfile(data, "work")
	if err := reloadPackageConfig(data); err != nil || data.packageConfigErr != nil {
		t.Fatalf("recovery failed: %v", err)
	}
}
