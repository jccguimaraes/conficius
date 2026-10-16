package main

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/kungfusheep/glyph"
)

type packageColumn struct {
	heading string
	value   any
	width   int
	color   any
}

func packageColumns(kind string, width int, columns []packageColumn) []packageColumn {
	count := len(columns)
	switch kind {
	case "outdated":
		for i := range columns {
			heading := columns[i].heading
			if heading == "Error" && width < 110 || heading == "Scope" && width < 92 || heading == "Manager" && width < 76 {
				columns[i].width = 0
				count--
			}
		}
	default:
		if kind == "vulnerability" && width < 140 {
			count -= 2
		}
		if width < 118 {
			count--
		}
		if width < 100 {
			count--
		}
		if width < 84 {
			count--
		}
		if width < 58 {
			count = 3
		}
	}
	if kind != "outdated" {
		columns = columns[:count]
	}
	available := width - 6 - (count - 1) // frame, padding, list marker, column gaps
	if kind == "outdated" || kind == "vulnerability" {
		preferred := 0
		for _, column := range columns {
			if column.heading != "Error" {
				preferred += column.width
			}
		}
		if available >= preferred {
			for i := range columns {
				if columns[i].heading == "Error" && columns[i].width > 0 {
					columns[i].width = available - preferred
				}
			}
			return columns
		}
	}
	weight := 0
	for _, column := range columns {
		if kind == "outdated" && column.heading == "" {
			available -= 3
			continue
		}
		weight += column.width
	}
	for i := range columns {
		if columns[i].width == 0 {
			continue
		}
		if kind == "outdated" && columns[i].heading == "" {
			columns[i].width = 3
			continue
		}
		w := available * columns[i].width / weight
		weight -= columns[i].width
		available -= w
		columns[i].width = w
	}
	return columns
}

func packageTableRow(kind string, values func() []packageColumn, heading bool) glyph.Component {
	columns := values()
	cells := make([]glyph.Component, 0, len(columns)*2)
	for index, column := range columns {
		// Scalar widths keep one List template and one line per item at every size.
		width := func(gap bool) any {
			var cases []glyph.MatchCase[int]
			for _, size := range []int{120, 118, 110, 100, 96, 92, 84, 76, 58, 40} {
				visible := packageColumns(kind, size, values())
				w := 0
				if index < len(visible) && visible[index].width > 0 {
					w = visible[index].width
					if gap {
						w = 0
						if index > 0 {
							w = 1
						}
					}
				}
				cases = append(cases, glyph.Gte(size, w))
			}
			return glyph.Match(&screenSize.Width, cases...).Default(0)
		}
		value := column.value
		if heading {
			value = column.heading
		}
		var cell glyph.Component = glyph.Text(value).FG(column.color)
		if heading {
			cell = glyph.Text(value).Bold()
		} else if component, ok := value.(glyph.Component); ok {
			cell = component
		}
		box := glyph.VBox.Width(width(false))
		if kind == "outdated" && column.heading == "Error" {
			box = box.Grow(glyph.IfOrd(&screenSize.Width).Gte(110).Then(1).Else(0))
		}
		if kind == "outdated" || kind == "vulnerability" {
			cells = append(cells, glyph.VBox.Width(width(true))())
		} else {
			cells = append(cells, glyph.Space().Width(width(true)))
		}
		cells = append(cells, box(cell))
	}
	return glyph.HBox.Height(1)(cells...)
}

func packageContext(data *model) any {
	return func() string {
		text := "Profile: " + data.profile.Name
		if data.active != nil {
			text += " · Scope: " + data.active.Kind + ":" + data.active.Name
		}
		if data.cacheRefreshErr != "" {
			text += " · " + data.cacheRefreshErr
		}
		return text
	}
}

func packageTable(kind string, columns func() []packageColumn, loading *bool, list glyph.Component) glyph.Component {
	return glyph.VBox.Grow(1)(glyph.HBox(glyph.Space().Width(2), glyph.VBox.Grow(1)(packageTableRow(kind, columns, true))), glyph.HRule(), loadingContent(loading, list))
}

func switchProfile(data *model, name string) {
	data.profileOverride = name
	data.active = nil
	clear(data.selected)
	clear(data.failed)
	clear(data.attempted)
	resetUpgradePlans(data)
	data.packages, data.rows, data.findings = nil, nil, nil
	data.query = packagemanager.QueryResult{}
	data.detail, data.packageDetailText = vulnerabilityRow{}, ""
	data.installedCount, data.securityCount = 0, 0
	data.localChecked, data.outdatedChecked, data.securityChecked = false, false, false
	data.updateErrorLines = nil
	data.runInteractive = false
}

type profileRow struct {
	Name   string
	Active bool
}

func profileView(data *model, app *glyph.App) (glyph.Component, *glyph.ListC[profileRow]) {
	list := styleList(glyph.List(&data.profileRows)).Render(func(row *profileRow) glyph.Component {
		return glyph.HBox.Gap(1)(glyph.Text(&row.Name).FG(glyph.If(&row.Active).Then(glyph.Cyan).Else(glyph.Color{})), glyph.If(&row.Active).Then(glyph.Text("(active)").FG(glyph.Cyan)))
	}).BindNav("j", "k").Handle("<Enter>", func(row *profileRow) {
		switchProfile(data, row.Name)
		refreshLocal(data, app)
		if data.packageConfigErr == nil {
			app.Go("scopes")
		}
	})
	return screenFrame("Packages / Profiles", packageContext(data), &data.status, list, "↑/↓ move  Enter choose  Esc Home"), list
}

func scopeView(data *model, app *glyph.App) (glyph.Component, *glyph.ListC[config.Scope]) {
	list := styleList(glyph.List(&data.scopes)).Render(func(scope *config.Scope) glyph.Component {
		return glyph.HBox.Gap(2)(glyph.If(&scope.Name).Then(glyph.Text(&scope.Name)).Else(glyph.Text(&scope.Kind)), glyph.Text(&scope.Path).Dim())
	}).BindNav("j", "k").Handle("<Enter>", func(scope *config.Scope) {
		selectScope(data, scope)
		refresh(data, app)
		if data.packageConfigErr == nil && data.active != nil {
			app.Go("results")
		}
	})
	return screenFrame("Packages / Scopes", packageContext(data), &data.status, list, cacheRefreshFooter("↑/↓ move  Enter open  p profiles", runtime.GOOS)), list
}

func outdatedColumns(row *outdatedRow) []packageColumn {
	markColor := glyph.If(&row.Failed).Then(glyph.Red).Else(glyph.Color{})
	errorText := glyph.If(&row.Failed).Then(glyph.If(&row.Error).Then(glyph.Text("Update failed; Enter details").FG(glyph.Red))).Else(glyph.Text(&row.Error).FG(glyph.Red))
	return []packageColumn{{"", &row.Mark, 3, markColor}, {"Manager", &row.Package.Manager, 8, glyph.Color{}}, {"Scope", &row.Package.Scope, 10, glyph.Color{}}, {"Tool", &row.Package.Name, 24, glyph.Color{}}, {"Current", &row.Package.Current, 16, glyph.Color{}}, {"Latest", &row.Package.Latest, 16, glyph.Color{}}, {"Error", errorText, 24, glyph.Red}}
}

func resultView(data *model) (glyph.Component, *glyph.ListC[outdatedRow]) {
	list := styleList(glyph.List(&data.rows)).Render(func(row *outdatedRow) glyph.Component {
		return packageTableRow("outdated", func() []packageColumn { return outdatedColumns(row) }, false)
	})
	content := packageTable("outdated", func() []packageColumn { return outdatedColumns(&outdatedRow{}) }, &data.loading, list)
	return screenFrame("Packages / Outdated", packageContext(data), &data.status, content, cacheRefreshFooter("↑/↓ move  Enter details  Space mark  p profiles", runtime.GOOS), &data.securityScanning), list
}

func updateErrorView(data *model) (glyph.Component, *glyph.ListC[string]) {
	list := styleList(glyph.List(&data.updateErrorLines)).Render(func(line *string) glyph.Component {
		return glyph.VBox.Width(glyph.IfOrd(&screenSize.Width).Gte(160).Then(154).Else(glyph.IfOrd(&screenSize.Width).Gte(120).Then(114).Else(glyph.IfOrd(&screenSize.Width).Gte(80).Then(74).Else(34))))(glyph.TextBlock(line).CharWrap().FG(glyph.Red))
	})
	return screenFrame("Packages / Update error", packageContext(data), &data.status, list, "↑/↓ scroll  Esc back"), list
}

func confirmView(data *model) glyph.Component {
	list := glyph.List(&data.commands).Render(func(row *commandRow) glyph.Component {
		return glyph.VBox(glyph.Textf(&row.Manager, "  ", &row.Scope, "  ", &row.Packages), glyph.TextBlock(&row.Command).CharWrap())
	}).Marker("").BindNav("<down>", "<up>").BindNav("j", "k").BindHalfPageNav("<C-d>", "<C-u>")
	return screenFrame("Packages / Confirm upgrades", packageContext(data), &data.status, glyph.VBox.Grow(1)(glyph.Text(&data.planSummary).Bold(), glyph.HRule(), loadingContent(&data.updating, list)), "↑/↓/j/k scroll  Ctrl-d/u page  y apply  n/Esc cancel")
}

func vulnerabilityColumns(row *vulnerabilityRow) []packageColumn {
	color := glyph.Match(&row.Score, glyph.Where(func(score float64) bool { return score >= 9 }, glyph.Red), glyph.Where(func(score float64) bool { return score >= 7 }, glyph.Yellow), glyph.Where(func(score float64) bool { return score >= 4 }, glyph.Yellow), glyph.Where(func(score float64) bool { return score > 0 }, glyph.Blue)).Default(glyph.Color{})
	return []packageColumn{{"Tool", &row.Package, 22, glyph.Color{}}, {"Status", &row.Status, 12, glyph.Match(&row.Status, glyph.Eq("vulnerable", glyph.Red), glyph.Eq("error", glyph.Red), glyph.Eq("unknown", glyph.Yellow), glyph.Eq("unmapped", glyph.Yellow), glyph.Eq("clean", glyph.Green)).Default(glyph.Color{})}, {"CVSS", &row.Score, 6, color}, {"Current", &row.Current, 14, glyph.Color{}}, {"Fix", &row.UpgradeTo, 14, glyph.Color{}}, {"Source", &row.Source, 6, glyph.Color{}}, {"Advisory", &row.Advisory, 24, glyph.Color{}}, {"Rules", &row.RuleCount, 5, glyph.Color{}}, {"Class", &row.DependencyClass, 12, glyph.Color{}}, {"Origin", glyph.Text(func() string { return vulnerabilityOrigin(row.Evidence) }).Dim(), 13, glyph.Color{}}}
}

func vulnerabilityView(data *model) (glyph.Component, *glyph.ListC[vulnerabilityRow]) {
	list := styleList(glyph.List(&data.findings)).Render(func(row *vulnerabilityRow) glyph.Component {
		return packageTableRow("vulnerability", func() []packageColumn { return vulnerabilityColumns(row) }, false)
	})
	options := func() string {
		indirect, graph := "off", "off"
		if data.includeIndirect {
			indirect = "on"
		}
		if data.includeGraphOnly {
			graph = "on"
		}
		return "Indirect " + indirect + " · Graph " + graph
	}
	scope := glyph.IfOrd(&screenSize.Width).Gte(50).
		Then(glyph.Text(func() string { return "Direct + tools · " + options() }).Dim()).
		Else(glyph.VBox(glyph.Text("Direct + tools").Dim(), glyph.Text(options).Dim()))
	empty := glyph.Text(func() string {
		if len(data.findings) != 0 {
			return ""
		}
		if data.securityChecked {
			return "No findings in selected scope."
		}
		return "No findings yet in selected scope."
	})
	content := glyph.VBox.Grow(1)(scope, empty, packageTable("vulnerability", func() []packageColumn { return vulnerabilityColumns(&vulnerabilityRow{}) }, &data.loading, list))
	return screenFrame("Packages / Vulnerabilities", packageContext(data), &data.status, content, "↑/↓ move  Enter details  i indirect  g graph-only", &data.securityScanning), list
}

func vulnerabilityDetailContent(row *vulnerabilityRow) (glyph.Component, *glyph.TextViewC) {
	var text string
	viewer := glyph.TextView(&text).Grow(1).BindNav("j", "k").BindNav("<down>", "<up>").BindPageNav("<C-d>", "<C-u>")
	// TextBlock funcs are not evaluated by glyph v0.8.0; Text evaluates before the layer sync.
	binding := glyph.HBox.Height(0)(glyph.Text(func() string {
		formatted := fmt.Sprintf("Package\n%s\n\nClass\n%s\n\nScan scope\n%s\n\nManager\n%s\n\nScope\n%s\n\nPath\n%s\n\nStatus\n%s\n\nCurrent\n%s\n\nSource\n%s\n\nCVE/advisory\n%s\n\nSource ID\n%s\n\nCVSS\n%g\n\nUnsupported rules\n%s\n\nAliases\n%s\n\nUpgrade target\n%s\n\nUpgrade note\n%s\n\nDescription\n%s\n\nReference\n%s\n\nCVE URL\n%s\n\nReason/error\n%s", row.Package, row.DependencyClass, row.ScanScope, row.Manager, row.Scope, row.Path, row.Status, row.Current, row.Source, row.Advisory, row.SourceID, row.Score, strings.Join(row.Rules, ", "), strings.Join(row.Aliases, ", "), row.UpgradeTo, row.UpgradeNote, row.Description, row.Reference, row.CVELink, row.Reason)
		formatted = "Read-only security evidence; no mark or update actions.\nInstalled version is checked against advisory ranges. Fixed version is advisory evidence, not an update plan.\nUsage describes current-build module membership, not vulnerable call reachability.\n\nOrigin\n" + vulnerabilityOrigin(row.Evidence) + "\n\n" + formatted
		if text != formatted {
			text = formatted
			viewer.Layer().ScrollToTop()
		}
		return ""
	}))
	return glyph.VBox.Grow(1)(binding, viewer), viewer
}

func vulnerabilityDetailView(data *model, app *glyph.App) glyph.Component {
	viewer, _ := vulnerabilityDetailContent(&data.detail)
	link := glyph.Jump(glyph.Text(&data.detail.Advisory).FG(glyph.Cyan).Underline(), func() {
		if err := openURL(data.detail.CVELink); err != nil {
			data.status = err.Error()
			app.RequestRender()
		}
	})
	content := glyph.VBox.Grow(1)(viewer, link)
	return screenFrame("Packages / Vulnerability details", packageContext(data), &data.status, content, "↑/↓ scroll  f open link  Esc back")
}

func packageDetails(row *outdatedRow) string {
	pkg := row.Package
	fields := func(separator string, pairs ...[2]string) string {
		var lines []string
		for _, pair := range pairs {
			if pair[1] != "" {
				lines = append(lines, pair[0]+": "+pair[1])
			}
		}
		return strings.Join(lines, separator)
	}
	sections := []string{pkg.Name, fmt.Sprintf("Current: %s   Latest: %s", pkg.Current, pkg.Latest)}
	for _, section := range []string{
		fields("   ", [2]string{"Manager", pkg.Manager}, [2]string{"Scope", pkg.Scope}),
		fields("\n", [2]string{"Path", pkg.Path}, [2]string{"Manifest", pkg.Manifest}, [2]string{"Config", pkg.Config}, [2]string{"Executable", pkg.Executable}),
		fields("   ", [2]string{"Requested", pkg.Requested}, [2]string{"Role", string(pkg.Role)}, [2]string{"Dependency class", pkg.DependencyClass}),
	} {
		if section != "" {
			sections = append(sections, section)
		}
	}
	if row.Error != "" {
		sections = append(sections, "Error\n"+row.Error)
	}
	return strings.Join(sections, "\n\n")
}

func loadingContent(loading *bool, content glyph.Component) glyph.Component {
	return glyph.VBox.Grow(1)(glyph.If(loading).Then(glyph.HBox.Gap(1)(glyph.Spinner().FG(glyph.Cyan), glyph.Text("Refreshing…").FG(glyph.Cyan))), content)
}
