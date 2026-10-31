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

func TestPackageViewsShowActiveProjectPath(t *testing.T) {
	previous := screenSize
	t.Cleanup(func() { updateScreenSize(previous.Width, previous.Height) })
	data := &model{active: &config.Scope{Kind: "project", Name: "web", Path: "/work/projects/web"}}
	outdated, _ := resultView(data)
	vulnerabilities, _ := vulnerabilityView(data)
	for _, view := range []glyph.Component{outdated, vulnerabilities} {
		template := glyph.Build(view)
		for _, width := range []int{120, 40, 80} {
			updateScreenSize(width, 24)
			buffer := glyph.NewBuffer(width, 24)
			template.Execute(buffer, int16(width), 24)
			if line := buffer.GetLine(2); !strings.Contains(line, "Path: /work/projects/web") {
				t.Fatalf("width %d missing project path: %q", width, line)
			}
		}
		data.active.Path = "/work/projects/api"
		buffer := glyph.NewBuffer(80, 24)
		template.Execute(buffer, 80, 24)
		if line := buffer.GetLine(2); !strings.Contains(line, "Path: /work/projects/api") {
			t.Fatalf("project path did not update: %q", line)
		}
		data.active.Path = "/work/projects/web"
	}
	data.active = &config.Scope{Kind: "global"}
	if text := packageContext(data).(func() string)(); strings.Contains(text, "Path:") {
		t.Fatalf("global scope shows empty path: %q", text)
	}
}

func TestPackageViewsResponsive(t *testing.T) {
	data := &model{
		rows:     []outdatedRow{{Package: packagemanager.Package{Name: "typescript", Current: "5.8.2", Latest: "5.9.3", Manager: "npm", Scope: "project"}, Mark: "[x]"}},
		findings: []vulnerabilityRow{{Evidence: security.Evidence{Package: "lodash", Status: "vulnerable", Score: 8.1, UpgradeTo: "4.17.21", Advisory: "GHSA-29mw-wpgm-hmr9", Source: "OSV"}, Current: "4.17.20", RuleCount: "2"}},
	}
	outdated, _ := resultView(data)
	vuln, _ := vulnerabilityView(data)
	for name, view := range map[string]struct {
		component glyph.Component
		want      []string
	}{
		"outdated":      {outdated, []string{"typescript", "[x]", "5.8.2", "5.9.3"}},
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

func TestVulnerabilityScopePersistsAcrossTogglesAndScanStates(t *testing.T) {
	defer updateScreenSize(80, 24)
	for _, width := range []int{40, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			updateScreenSize(width, 12)
			data := &model{}
			view, _ := vulnerabilityView(data)
			template := glyph.Build(view)
			render := func(indirect, graph string) string {
				t.Helper()
				buffer := glyph.NewBuffer(width, 12)
				template.Execute(buffer, int16(width), 12)
				output := homeRenderOutput(buffer, 12)
				for _, want := range []string{"Direct + tools", "Indirect " + indirect, "Graph " + graph} {
					if !strings.Contains(output, want) {
						t.Fatalf("missing %q:\n%s", want, output)
					}
				}
				return output
			}
			if output := render("off", "off"); !strings.Contains(output, "No findings yet in selected scope.") {
				t.Fatal(output)
			}
			toggleIndirect(data, func() {})
			data.loading, data.securityScanning, data.securityCount = true, true, 99
			render("on", "off")
			toggleGraphOnly(data, func() {})
			render("on", "on")
			toggleIndirect(data, func() {})
			data.loading, data.securityScanning, data.securityChecked = false, false, true
			if output := render("off", "on"); !strings.Contains(output, "No findings in selected scope.") {
				t.Fatal(output)
			}
			toggleGraphOnly(data, func() {})
			data.findings = []vulnerabilityRow{{Evidence: security.Evidence{Package: "lodash", Status: "vulnerable"}}}
			if output := render("off", "off"); strings.Contains(output, "No findings") || !strings.Contains(output, "lodash") {
				t.Fatal(output)
			}
		})
	}
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

}

func TestPackageHeadingAlignmentAndPalette(t *testing.T) {
	updateScreenSize(120, 30)
	defer updateScreenSize(80, 24)
	row := &outdatedRow{Package: packagemanager.Package{Name: "node", Current: "20", Latest: "22", Manager: "mise", Scope: "global"}}
	columns := func() []packageColumn { return outdatedColumns(row) }
	buffer := glyph.NewBuffer(120, 10)
	glyph.Build(glyph.VBox(packageTableRow("outdated", columns, true), glyph.HRule(), packageTableRow("outdated", columns, false))).Execute(buffer, 120, 10)
	for _, pair := range [][2]string{{"Tool", "node"}, {"Current", "20"}, {"Latest", "22"}, {"Manager", "mise"}, {"Scope", "global"}} {
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
	row := outdatedRow{Package: packagemanager.Package{Name: "typescript", Manager: "npm", Scope: "project", Current: "5.8.2", Requested: "^5.8", Latest: "5.9.3"}, Mark: "[x]", Security: "Unknown", Error: "failure"}
	columns := func() []packageColumn { return outdatedColumns(&row) }
	view, _ := resultView(&model{rows: []outdatedRow{row}})
	template := glyph.Build(view)
	for _, width := range []int{200, 120, 118, 117, 110, 109, 100, 99, 92, 91, 80, 76, 75, 40, 200} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			updateScreenSize(width, 12)
			visible := packageColumns("outdated", width, columns())
			if len(visible) != 9 || visible[0].heading != "" || visible[0].width != 3 {
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
			for i, pair := range [][2]string{{"", "[x]"}, {"Manager", "npm"}, {"Scope", "project"}, {"Tool", "typescript"}, {"Requested", "^5.8"}, {"Current", "5.8.2"}, {"Latest", "5.9.3"}, {"Security", "Unknown"}, {"Error", "failure"}} {
				hidden := pair[0] == "Security" && width < 100 || pair[0] == "Requested" && width < 118 || pair[0] == "Error" && width < 110 || pair[0] == "Scope" && width < 92 || pair[0] == "Manager" && width < 76
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

func TestMiseRequestedAndInstalledVersions(t *testing.T) {
	previous := screenSize
	t.Cleanup(func() { updateScreenSize(previous.Width, previous.Height) })
	data := &model{rows: []outdatedRow{
		{Package: packagemanager.Package{Name: "missing-tool", Manager: "mise", Requested: "1.2.3", Latest: "1.8.0"}},
		{Package: packagemanager.Package{Name: "installed-tool", Manager: "mise", Current: "24.18.0", Requested: "24.18.0", Latest: "26.13.0"}},
	}}
	view, _ := resultView(data)
	template := glyph.Build(view)
	for _, width := range []int{200, 120, 80, 200} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			updateScreenSize(width, 24)
			buffer := glyph.NewBuffer(width, 24)
			template.Execute(buffer, int16(width), 24)
			var heading string
			rows := map[string]int{}
			line := func(y int) string {
				cells := make([]rune, width)
				for x := range width {
					cells[x] = buffer.Get(x, y).Rune
					if cells[x] == 0 || cells[x] > 127 {
						cells[x] = ' '
					}
				}
				return string(cells)
			}
			for y := range 24 {
				text := line(y)
				if strings.Contains(text, "Current") {
					heading = text
				}
				for _, row := range data.rows {
					if strings.Contains(text, row.Package.Name) {
						rows[row.Package.Name] = y
					}
				}
			}
			current, latest, requested := strings.Index(heading, "Current"), strings.Index(heading, "Latest"), strings.Index(heading, "Requested")
			if len(rows) != 2 || current < 0 || latest <= current || (requested >= 0) != (width >= 118) {
				t.Fatalf("width %d missing rows or columns:\n%s", width, homeRenderOutput(buffer, 24))
			}
			for _, row := range data.rows {
				y := rows[row.Package.Name]
				text := line(y)
				wantCurrent := row.Package.Current
				if wantCurrent == "" {
					wantCurrent = "[MISSING]"
					for x := current; x < current+len(wantCurrent); x++ {
						if buffer.Get(x, y).Style.FG != glyph.Yellow {
							t.Fatalf("width %d missing marker not yellow at %d: %q", width, x, text)
						}
					}
				}
				if !strings.HasPrefix(text[current:], wantCurrent) || !strings.HasPrefix(text[latest:], row.Package.Latest) {
					t.Fatalf("width %d incorrect installed/latest versions: %q", width, text)
				}
				if requested >= 0 {
					if requested >= current || strings.TrimSpace(text[requested:current]) != row.Package.Requested {
						t.Fatalf("width %d incorrect requested version: %q", width, text)
					}
				} else if row.Package.Current == "" && strings.Contains(text, row.Package.Requested) {
					t.Fatalf("width %d hidden requested version rendered: %q", width, text)
				}
				if row.Package.Current != "" && strings.Contains(text, "[MISSING]") {
					t.Fatalf("width %d installed row marked missing: %q", width, text)
				}
			}
		})
	}
}

func TestOutdatedWideColumnsStayCompact(t *testing.T) {
	defer updateScreenSize(80, 24)
	row := outdatedRow{Package: packagemanager.Package{Name: "typescript", Manager: "npm", Scope: "project", Current: "5.8.2", Requested: "^5.8", Latest: "5.9.3"}, Mark: "[x]", Security: "Unknown", Failed: true, Error: "npm update failed: /very/long/path\nfull command output"}
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
		for i, pair := range [][2]string{{"Manager", "npm"}, {"Scope", "project"}, {"Tool", "typescript"}, {"Requested", "^5.8"}, {"Current", "5.8.2"}, {"Latest", "5.9.3"}, {"Security", "Unknown"}, {"Error", "Update failed"}} {
			x := []int{7, 16, 27, 46, 59, 72, 85, 104}[i]
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
	data.rows[0].Error = strings.Repeat("error ", 12)
	buffer := glyph.NewBuffer(200, 30)
	template.Execute(buffer, 200, 30)
	if !strings.Contains(homeRenderOutput(buffer, 30), strings.TrimRight(data.rows[0].Error, " ")) {
		t.Fatalf("error column did not receive remaining width:\n%s", homeRenderOutput(buffer, 30))
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
		} else if width < 140 {
			pairs = pairs[:7]
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
				columns := packageColumns("vulnerability", width-2, vulnerabilityColumns(&data.findings[0]))
				x = 3
				for k := 0; k < j; k++ {
					x += columns[k].width + 1
				}
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
		if width >= 140 {
			for j, column := range packageColumns("vulnerability", width-2, vulnerabilityColumns(&data.findings[0])) {
				if column.width != []int{22, 12, 6, 14, 14, 6, 24, 5, 12, 13}[j] {
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
