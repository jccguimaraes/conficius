package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/jccguimaraes/conficius/internal/security"
	"github.com/kungfusheep/glyph"
	"github.com/kungfusheep/riffkey"
)

func TestPackageViewsResponsive(t *testing.T) {
	data := &model{
		rows:     []outdatedRow{{Package: packagemanager.Package{Name: "typescript", Current: "5.8.2", Latest: "5.9.3", Manager: "npm", Scope: "project"}, Mark: "[x]"}},
		drift:    []config.PackageState{{Name: "node", Status: config.DriftBehind, Installed: "20.19.0", DesiredVersion: "22.22.4", Manager: "mise", Scope: "global"}},
		findings: []vulnerabilityRow{{Evidence: security.Evidence{Package: "lodash", Status: "vulnerable", Score: 8.1, UpgradeTo: "4.17.21", Advisory: "GHSA-29mw-wpgm-hmr9", Source: "OSV"}, Current: "4.17.20", RuleCount: "2"}},
	}
	outdated, _ := resultView(data)
	drift, _ := driftView(data)
	vuln, _ := vulnerabilityView(data)
	for name, view := range map[string]struct {
		component glyph.Component
		want      []string
	}{
		"outdated":      {outdated, []string{"typescript", "[x]", "5.8.2", "5.9.3"}},
		"drift":         {drift, []string{"node", config.DriftBehind}},
		"vulnerability": {vuln, []string{"lodash", "vulnerable", "8.1"}},
	} {
		template := glyph.Build(view.component)
		for _, size := range [][2]int{{40, 12}, {80, 24}, {120, 30}, {40, 12}} {
			t.Run(fmt.Sprintf("%s/%dx%d", name, size[0], size[1]), func(t *testing.T) {
				updateScreenSize(size[0], size[1])
				buffer := glyph.NewBuffer(size[0], size[1])
				template.Execute(buffer, int16(size[0]), int16(size[1]))
				output := homeRenderOutput(buffer, size[1])
				wantValues := append([]string{}, view.want...)
				if size[0] >= 80 {
					if name == "drift" {
						wantValues = append(wantValues, "20.19.0", "22.22.4")
					}
					if name == "vulnerability" {
						wantValues = append(wantValues, "4.17.20", "4.17.21")
					}
				}
				for _, want := range append(wantValues, "Enter", "? help") {
					if !strings.Contains(output, want) {
						t.Fatalf("missing %q:\n%s", want, output)
					}
				}
			})
		}
	}
	updateScreenSize(80, 24)
}

func TestPackageLoadingRetainsRows(t *testing.T) {
	loading := true
	template := glyph.Build(loadingContent(&loading, glyph.Text("existing-row")))
	buffer := glyph.NewBuffer(40, 12)
	template.Execute(buffer, 40, 12)
	output := homeRenderOutput(buffer, 12)
	if !strings.Contains(output, "Refreshing") || !strings.Contains(output, "existing-row") {
		t.Fatal(output)
	}
	loading = false
	template.Execute(buffer, 40, 12)
	output = homeRenderOutput(buffer, 12)
	if strings.Contains(output, "Refreshing") || !strings.Contains(output, "existing-row") {
		t.Fatal(output)
	}
}

func TestCommandPreviewScrolls(t *testing.T) {
	defer updateScreenSize(80, 24)
	data := &model{planSummary: "20 upgrades"}
	for i := range 20 {
		data.commands = append(data.commands, commandRow{Manager: "npm", Scope: "project", Packages: fmt.Sprintf("tool-%02d", i), Command: fmt.Sprintf("npm update tool-%02d", i)})
	}
	for _, size := range [][2]int{{40, 12}, {80, 24}, {120, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			updateScreenSize(size[0], size[1])
			view := confirmView(data)
			template := glyph.Build(view)
			app := glyph.NewApp()
			app.View("confirm", view)
			app.Go("confirm")
			buffer := glyph.NewBuffer(size[0], size[1])
			render := func() string {
				template.Execute(buffer, int16(size[0]), int16(size[1]))
				return homeRenderOutput(buffer, size[1])
			}
			initial := render()
			if !strings.Contains(initial, "npm update tool-00") || strings.Contains(initial, "npm update tool-19") || !strings.Contains(initial, "scroll") {
				t.Fatalf("unexpected initial preview:\n%s", initial)
			}
			for _, keys := range [][2]string{{"<down>", "<up>"}, {"j", "k"}, {"<C-d>", "<C-u>"}} {
				for range len(data.commands) {
					app.Input().Dispatch(riffkey.ParsePattern(keys[0])[0])
					render()
				}
				if output := render(); !strings.Contains(output, "npm update tool-19") || strings.Contains(output, "npm update tool-00") {
					t.Fatalf("%s did not scroll to last command:\n%s", keys[0], output)
				}
				for range len(data.commands) {
					app.Input().Dispatch(riffkey.ParsePattern(keys[1])[0])
					render()
				}
				if output := render(); !strings.Contains(output, "npm update tool-00") || strings.Contains(output, "npm update tool-19") {
					t.Fatalf("%s did not scroll to first command:\n%s", keys[1], output)
				}
			}
		})
	}
}

func TestPackageDetailRetainsSecondaryFields(t *testing.T) {
	row := outdatedRow{Package: packagemanager.Package{Name: "tool", Manager: "npm", Scope: "project", Path: "/very/long/project/path", Current: "1.0", Latest: "2.0", Requested: "^1", Role: "direct", DependencyClass: "dev", Manifest: "package.json", Config: "npmrc", Executable: "/bin/npm"}, Mark: "[x]", Failed: true, Error: "first line\nsecond line"}
	text := packageDetails(&row)
	for _, want := range []string{"tool", "Manager: npm   Scope: project", row.Package.Path, "Current: 1.0   Latest: 2.0", "Requested: ^1   Role: direct   Dependency class: dev", "package.json", "npmrc", "/bin/npm", "Error\n" + row.Error} {
		if !strings.Contains(text, want) {
			t.Fatalf("detail missing %q", want)
		}
	}
	if !strings.HasPrefix(text, "tool\n\n") {
		t.Fatalf("missing name heading: %q", text)
	}
	for _, unwanted := range []string{"Mark", "[x]", "Failed", "true"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("detail contains boilerplate %q: %s", unwanted, text)
		}
	}
	drift := config.PackageState{Name: "node", Status: config.DriftBehind, Installed: "20", DesiredVersion: "22", Manager: "mise", Scope: "global", Path: "/repo", DependencyClass: "runtime"}
	for _, want := range []string{drift.Name, drift.Status, drift.Installed, drift.DesiredVersion, drift.Manager, drift.Scope, drift.Path, drift.DependencyClass} {
		if !strings.Contains(driftDetails(&drift), want) {
			t.Fatalf("drift detail missing %q", want)
		}
	}
}

func TestPackageHeadingAlignmentAndPalette(t *testing.T) {
	updateScreenSize(120, 30)
	defer updateScreenSize(80, 24)
	row := &config.PackageState{Name: "node", Status: config.DriftBehind, Installed: "20", DesiredVersion: "22", Manager: "mise", Scope: "global"}
	columns := func() []packageColumn { return driftColumns(row) }
	buffer := glyph.NewBuffer(120, 10)
	glyph.Build(glyph.VBox(packageTableRow("drift", columns, true), glyph.HRule(), packageTableRow("drift", columns, false))).Execute(buffer, 120, 10)
	for _, pair := range [][2]string{{"Tool", "node"}, {"State", config.DriftBehind}, {"Installed", "20"}, {"Desired", "22"}, {"Manager", "mise"}, {"Scope", "global"}} {
		heading, value := strings.Index(buffer.GetLine(0), pair[0]), strings.Index(buffer.GetLine(2), pair[1])
		if heading < 0 || heading != value {
			t.Fatalf("column mismatch %v: %q / %q", pair, buffer.GetLine(0), buffer.GetLine(2))
		}
		if buffer.Get(heading, 0).Style.Attr&glyph.AttrBold == 0 {
			t.Fatalf("heading not bold: %s", pair[0])
		}
		if buffer.Get(value, 2).Style.Attr&glyph.AttrBold != 0 {
			t.Fatalf("value unexpectedly bold: %s", pair[1])
		}
	}
	if !strings.Contains(buffer.GetLine(1), "─") {
		t.Fatal("missing thin separator")
	}
	for _, score := range []float64{0, 1, 5, 8, 10} {
		if severityColor(score).Mode == glyph.ColorRGB {
			t.Fatalf("RGB severity: %g", score)
		}
	}
}

func TestPackageRowsStaySingleLineAndUpdateSelection(t *testing.T) {
	defer updateScreenSize(80, 24)
	data := &model{rows: []outdatedRow{
		{Package: packagemanager.Package{Name: strings.Repeat("界", 60), Current: strings.Repeat("1", 60), Latest: "2.0"}, Mark: "[ ]"},
		{Package: packagemanager.Package{Name: "second", Current: "3.0", Latest: "4.0"}, Mark: "[x]"},
	}}
	component, list := resultView(data)
	template := glyph.Build(component)
	for _, size := range [][2]int{{40, 12}, {80, 24}, {120, 30}, {40, 12}} {
		updateScreenSize(size[0], size[1])
		list.SetIndex(0)
		buffer := glyph.NewBuffer(size[0], size[1])
		template.Execute(buffer, int16(size[0]), int16(size[1]))
		first, second := -1, -1
		for y := range size[1] {
			if strings.Contains(buffer.GetLine(y), "界") {
				if first >= 0 {
					t.Fatal("name wrapped")
				}
				first = y
			}
			if strings.Contains(buffer.GetLine(y), "second") {
				second = y
			}
		}
		if first < 0 || second != first+1 {
			t.Fatalf("rows not adjacent:\n%s", homeRenderOutput(buffer, size[1]))
		}
		list.Down(nil)
		data.rows[1].Package.Latest = "5.0"
		template.Execute(buffer, int16(size[0]), int16(size[1]))
		if list.Index() != 1 || !strings.Contains(buffer.GetLine(second), "›") || !strings.Contains(buffer.GetLine(second), "5.0") {
			t.Fatal(homeRenderOutput(buffer, size[1]))
		}
	}
}

func TestOutdatedColumnOrderAndVisibility(t *testing.T) {
	defer updateScreenSize(80, 24)
	row := outdatedRow{Package: packagemanager.Package{Name: "typescript", Manager: "npm", Scope: "project", Current: "5.8.2", Latest: "5.9.3"}, Mark: "[x]", Error: "failure"}
	columns := func() []packageColumn { return outdatedColumns(&row) }
	view, _ := resultView(&model{rows: []outdatedRow{row}})
	template := glyph.Build(view)
	for _, width := range []int{200, 120, 110, 109, 92, 91, 80, 76, 75, 40, 200} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			updateScreenSize(width, 12)
			visible := packageColumns("outdated", width, columns())
			if len(visible) != 7 || visible[0].heading != "" || visible[0].width != 3 {
				t.Fatalf("column indexes or mark width changed: %+v", visible)
			}
			buffer := glyph.NewBuffer(width, 12)
			template.Execute(buffer, int16(width), 12)
			var heading, values string
			for y := range 12 {
				var lineBuilder strings.Builder
				for x := range width {
					r := buffer.Get(x, y).Rune
					if r == 0 || r > 127 {
						r = ' '
					}
					lineBuilder.WriteRune(r)
				}
				line := lineBuilder.String()
				if strings.Contains(line, "Tool") {
					heading = line
				}
				if strings.Contains(line, "typescript") {
					values = line
				}
			}
			mark := strings.Index(values, "[x]")
			if mark < 0 || strings.Contains(heading, "Mar") || strings.TrimSpace(heading[mark:mark+3]) != "" {
				t.Fatalf("invalid mark column: %q / %q", heading, values)
			}
			previous := -1
			for i, pair := range [][2]string{{"", "[x]"}, {"Manager", "npm"}, {"Scope", "project"}, {"Tool", "typescript"}, {"Current", "5.8.2"}, {"Latest", "5.9.3"}, {"Error", "failure"}} {
				hidden := pair[0] == "Error" && width < 110 || pair[0] == "Scope" && width < 92 || pair[0] == "Manager" && width < 76
				if hidden {
					if visible[i].width != 0 || strings.Contains(heading, pair[0]) || strings.Contains(values, pair[1]) {
						t.Fatalf("hidden column %s rendered: %q / %q", pair[0], heading, values)
					}
					continue
				}
				position := strings.Index(values, pair[1])
				if position <= previous || (pair[0] != "" && strings.Index(heading, pair[0]) != position) {
					t.Fatalf("column order/alignment %v: %q / %q", pair, heading, values)
				}
				previous = position
			}
		})
	}
}

func TestOutdatedWideColumnsStayCompact(t *testing.T) {
	defer updateScreenSize(80, 24)
	row := outdatedRow{Package: packagemanager.Package{Name: "typescript", Manager: "npm", Scope: "project", Current: "5.8.2", Latest: "5.9.3"}, Mark: "[x]", Failed: true, Error: "npm update failed: /very/long/path\nfull command output"}
	data := &model{rows: []outdatedRow{row}}
	view, _ := resultView(data)
	template := glyph.Build(view)
	for _, width := range []int{120, 200, 120, 200} {
		updateScreenSize(width, 30)
		buffer := glyph.NewBuffer(width, 30)
		template.Execute(buffer, int16(width), 30)
		headingY, rowY := -1, -1
		for y := range 30 {
			if strings.Contains(buffer.GetLine(y), "Current") {
				headingY = y
			}
			if strings.Contains(buffer.GetLine(y), "typescript") {
				rowY = y
			}
		}
		if headingY < 0 || rowY < 0 {
			t.Fatal(homeRenderOutput(buffer, 30))
		}
		if buffer.Get(1, rowY).Rune != '›' || buffer.Get(3, rowY).Rune != '[' || buffer.Get(5, rowY).Rune != ']' {
			t.Fatalf("cursor/checkbox separated: %q", buffer.GetLine(rowY))
		}
		for i, pair := range [][2]string{{"Manager", "npm"}, {"Scope", "project"}, {"Tool", "typescript"}, {"Current", "5.8.2"}, {"Latest", "5.9.3"}, {"Error", "Update failed; Enter details"}} {
			x := []int{7, 16, 27, 52, 69, 86}[i]
			for j, r := range pair[0] {
				if buffer.Get(x+j, headingY).Rune != r {
					t.Fatalf("%dx30 heading %s not at %d: %q", width, pair[0], x, buffer.GetLine(headingY))
				}
			}
			for j, r := range pair[1] {
				if buffer.Get(x+j, rowY).Rune != r {
					t.Fatalf("%dx30 value %s not at %d: %q", width, pair[1], x, buffer.GetLine(rowY))
				}
			}
		}
		if strings.Contains(homeRenderOutput(buffer, 30), "/very/long/path") || !strings.Contains(packageDetails(&data.rows[0]), row.Error) {
			t.Fatal("error summary or full details lost")
		}
	}
	data.rows[0].Failed = false
	data.rows[0].Error = strings.Repeat("error ", 16)
	buffer := glyph.NewBuffer(200, 30)
	template.Execute(buffer, 200, 30)
	if !strings.Contains(homeRenderOutput(buffer, 30), data.rows[0].Error) {
		t.Fatal("error column did not receive remaining width")
	}
	data.rows[0].Failed = true
	data.rows[0].Error = ""
	template.Execute(buffer, 200, 30)
	if strings.Contains(homeRenderOutput(buffer, 30), "Update failed") {
		t.Fatal("empty error shows failure summary")
	}
}

func TestVulnerabilityWideColumnsStayCompact(t *testing.T) {
	defer updateScreenSize(80, 24)
	data := &model{findings: []vulnerabilityRow{{Evidence: security.Evidence{Package: "lodash", Status: "vulnerable", Score: 8.1, UpgradeTo: "4.17.21", Advisory: "GHSA-29mw-wpgm-hmr9", Source: "OSV"}, Current: "4.17.20", RuleCount: "2"}}}
	view, _ := vulnerabilityView(data)
	template := glyph.Build(view)
	for i, width := range []int{200, 40, 80, 120, 200} {
		updateScreenSize(width, 30)
		score, color := 8.1, glyph.Yellow
		if i%2 == 0 {
			score, color = 10, glyph.Red
		}
		data.findings[0].Score = score
		buffer := glyph.NewBuffer(width, 30)
		template.Execute(buffer, int16(width), 30)
		headingY, rowY := -1, -1
		for y := range 30 {
			if strings.Contains(buffer.GetLine(y), "CVSS") {
				headingY = y
			}
			if strings.Contains(buffer.GetLine(y), "lodash") {
				rowY = y
			}
		}
		if headingY < 0 || rowY < 0 {
			t.Fatal(homeRenderOutput(buffer, 30))
		}
		pairs := [][2]string{{"Tool", "lodash"}, {"Status", "vulnerable"}, {"CVSS", fmt.Sprint(score)}, {"Current", "4.17.20"}, {"Fix", "4.17.21"}, {"Source", "OSV"}, {"Advisory", "GHSA-29mw-wpgm-hmr9"}, {"Rules", "2"}}
		if width < 120 {
			pairs = pairs[:3]
		}
		for j, pair := range pairs {
			x := -1
			for column := range width {
				if buffer.Get(column, headingY).Rune == rune(pair[0][0]) {
					x = column
					break
				}
			}
			if width >= 120 {
				x = []int{3, 26, 39, 46, 61, 76, 83, 108}[j]
			}
			if x < 0 {
				t.Fatalf("missing %s heading", pair[0])
			}
			for k, r := range pair[0] {
				if buffer.Get(x+k, headingY).Rune != r {
					t.Fatalf("%dx30 heading %s not at %d: %q", width, pair[0], x, buffer.GetLine(headingY))
				}
			}
			for k, r := range pair[1] {
				if buffer.Get(x+k, rowY).Rune != r {
					t.Fatalf("%dx30 value %s not at %d: %q", width, pair[1], x, buffer.GetLine(rowY))
				}
			}
			if pair[0] == "CVSS" && buffer.Get(x, rowY).Style.FG != color {
				t.Fatalf("stale severity at width %d", width)
			}
		}
		if width >= 120 {
			for j, column := range packageColumns("vulnerability", width, vulnerabilityColumns(&data.findings[0])) {
				if column.width != []int{22, 12, 6, 14, 14, 6, 24, 5}[j] {
					t.Fatalf("%dx30 column %s stretched to %d", width, column.heading, column.width)
				}
			}
		}
	}
}

func TestPackageDetailsOmitEmptyFields(t *testing.T) {
	row := outdatedRow{Package: packagemanager.Package{Name: "tool", Current: "1", Latest: "2"}, Mark: "[ ]"}
	want := "tool\n\nCurrent: 1   Latest: 2"
	if text := packageDetails(&row); text != want {
		t.Fatalf("empty detail fields retained: %q", text)
	}
	row.Package.Manifest = "package.json"
	row.Package.Role = "direct"
	want += "\n\nManifest: package.json\n\nRole: direct"
	if text := packageDetails(&row); text != want {
		t.Fatalf("optional detail fields wrong: %q", text)
	}
	row.Error = "first line\n" + strings.Repeat("full error ", 100) + "\nlast line"
	if text := packageDetails(&row); text != want+"\n\nError\n"+row.Error {
		t.Fatalf("full error not retained: %q", text)
	}
	row.Error = ""
	row.Failed = true
	if text := packageDetails(&row); text != want {
		t.Fatalf("empty failure boilerplate retained: %q", text)
	}
}
