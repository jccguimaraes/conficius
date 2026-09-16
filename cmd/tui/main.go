package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	glyph "github.com/kungfusheep/glyph"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/euvd"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/jccguimaraes/conficius/internal/security"
	"github.com/jccguimaraes/conficius/internal/vulnerability"
)

var version = "dev"

type outdatedRow struct {
	Package packagemanager.Package
	Mark    string
}

type vulnerabilityRow struct {
	Source    string
	Manager   string
	Package   string
	Current   string
	Status    string
	Advisory  string
	UpgradeTo string
}

type model struct {
	profile     config.ResolvedProfile
	scopes      []config.Scope
	active      *config.Scope
	packages    []packagemanager.Package
	rows        []outdatedRow
	query       packagemanager.QueryResult
	selected    map[string]bool
	plans       []packagemanager.UpdatePlan
	preview     string
	commands    []commandRow
	planSummary string
	updating    bool
	drift       []config.PackageState
	findings    []vulnerabilityRow
	mappings    []config.VulnerabilityMapping
	euvdMaps    []config.EUVDMapping
	status      string
	loading     bool
	cancel      context.CancelFunc
	generation  uint64
}

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	project, err := config.LoadDiscovered("")
	if err != nil && !errors.Is(err, config.ErrNotFound) {
		return err
	}
	if project == nil {
		project = &config.Project{Version: config.CurrentVersion}
	}
	profile, err := project.ResolveProfile("", false)
	if err != nil {
		return err
	}
	scopes, err := config.ResolveScopes(project, profile, false, nil, nil)
	if err != nil {
		return err
	}

	data := &model{profile: profile, scopes: scopes, mappings: project.Vulnerabilities.Mappings, euvdMaps: project.Vulnerabilities.EUVD, status: "Loading...", selected: make(map[string]bool)}
	app := glyph.NewApp()
	app.HideCursor()
	scopeComponent, scopeList := scopeView(data, app)
	app.View("scopes", scopeComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", app.Stop).
		Handle("<down>", scopeList.Down).
		Handle("<up>", scopeList.Up).
		Handle("r", func() { refresh(data, app) })
	resultComponent, packageList := resultView(data)
	app.View("results", resultComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("scopes") }).
		Handle("r", func() { refresh(data, app) }).
		Handle("d", func() { app.Go("drift") }).
		Handle("v", func() { app.Go("vulnerabilities") }).
		Handle("<down>", packageList.Down).
		Handle("<up>", packageList.Up).
		Handle("j", packageList.Down).
		Handle("k", packageList.Up).
		Handle("<Space>", func() { toggleSelected(data, packageList.Selected()); app.RequestRender() }).
		Handle("a", func() { toggleAll(data); app.RequestRender() }).
		Handle("u", func() { prepareUpgrade(data, app, false) }).
		Handle("U", func() { prepareUpgrade(data, app, true) })
	app.View("drift", driftView(data)).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("results") }).
		Handle("o", func() { app.Go("results") }).
		Handle("v", func() { app.Go("vulnerabilities") }).
		Handle("r", func() { refresh(data, app) })
	app.View("confirm", confirmView(data)).
		Handle("y", func() { applyUpgrades(data, app) }).
		Handle("n", func() { app.Go("results") }).
		Handle("<Esc>", func() { app.Go("results") })
	vulnerabilityComponent, vulnerabilityList := vulnerabilityView(data)
	app.View("vulnerabilities", vulnerabilityComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("results") }).
		Handle("o", func() { app.Go("results") }).
		Handle("d", func() { app.Go("drift") }).
		Handle("r", func() { refresh(data, app) }).
		Handle("<down>", vulnerabilityList.Down).
		Handle("<up>", vulnerabilityList.Up).
		Handle("j", vulnerabilityList.Down).
		Handle("k", vulnerabilityList.Up).
		Handle("<C-d>", vulnerabilityList.HalfPageDown).
		Handle("<C-u>", vulnerabilityList.HalfPageUp)
	refresh(data, app)
	defer func() {
		if data.cancel != nil {
			data.cancel()
		}
	}()
	return app.RunFrom("scopes")
}

func scopeView(data *model, app *glyph.App) (glyph.Component, *glyph.ListC[config.Scope]) {
	list := glyph.List(&data.scopes).
		Render(func(scope *config.Scope) glyph.Component {
			return glyph.Textf(&scope.Kind, ":", &scope.Name, "  ", &scope.Path)
		}).
		BindNav("j", "k").
		Handle("<Enter>", func(scope *config.Scope) {
			selectScope(data, scope)
			refresh(data, app)
			app.Go("results")
		})
	return glyph.VBox.Grow(1).Border(glyph.BorderRounded).Title("Conficius "+version)(
		glyph.Text("Profile: "+data.profile.Name).Bold(),
		glyph.Text("Scopes").Underline(),
		list,
		glyph.Text(&data.status).Dim(),
		glyph.Text("↑/↓ or j/k move  Enter open  r refresh  q quit").Dim(),
	), list
}

func resultView(data *model) (glyph.Component, *glyph.ListC[outdatedRow]) {
	list := glyph.List(&data.rows).
		Render(func(row *outdatedRow) glyph.Component {
			return glyph.HBox.Gap(2)(
				glyph.Text(&row.Mark).Width(3),
				glyph.Text(&row.Package.Manager).Width(8),
				glyph.Text(&row.Package.Name).Width(26),
				glyph.Text(&row.Package.Current).Width(16),
				glyph.Text(&row.Package.Latest).Width(16).FG(glyph.Green),
				glyph.Text(&row.Package.Scope).Width(10),
			)
		}).
		Marker("› ").MarkerStyle(glyph.Style{FG: glyph.Cyan}).
		SelectedStyle(glyph.Style{BG: glyph.RGB(40, 48, 60), Attr: glyph.AttrBold}).
		MaxVisible(1000)
	header := glyph.HBox.Gap(2)(
		glyph.Text("Mark").Width(3).Bold(), glyph.Text("Manager").Width(8).Bold(), glyph.Text("Tool").Width(26).Bold(),
		glyph.Text("Current").Width(16).Bold(), glyph.Text("Latest").Width(16).Bold(), glyph.Text("Scope").Width(10).Bold(),
	)
	return glyph.VBox.Grow(1).Border(glyph.BorderRounded).Title("Outdated packages")(
		header,
		loadingContent(&data.loading, list),
		glyph.Text(&data.status).Dim(),
		glyph.Text("↑/↓ or j/k move  Space mark  a mark all  u upgrade marked  U upgrade all  d drift  v vulnerabilities  Esc back  r refresh  q quit").Dim(),
	), list
}

func confirmView(data *model) glyph.Component {
	list := glyph.List(&data.commands).Render(func(row *commandRow) glyph.Component {
		return glyph.VBox(
			glyph.Textf(&row.Manager, "  ", &row.Scope, "  ", &row.Packages),
			glyph.Text(&row.Command).FG(glyph.Cyan),
		)
	}).Marker("").MaxVisible(100)
	return glyph.VBox.Grow(1).Border(glyph.BorderRounded).Title("Confirm upgrades")(
		glyph.Text(&data.planSummary).Bold(), loadingContent(&data.updating, list),
		glyph.Text("y apply  n/Esc cancel").Dim(),
	)
}

func driftView(data *model) glyph.Component {
	table := glyph.AutoTable(&data.drift).
		Columns("Manager", "Name", "Installed", "DesiredVersion", "Status", "Scope").
		Headers("Manager", "Tool", "Installed", "Desired", "Status", "Scope").
		Column("Status", statusStyle()).
		Column("DesiredVersion", coloredText(glyph.Green)).
		Scrollable(12).BindVimNav().BindNav("<down>", "<up>")
	return glyph.VBox.Grow(1).Border(glyph.BorderRounded).Title("Profile drift")(
		loadingContent(&data.loading, table),
		glyph.Text(&data.status).Dim(),
		glyph.Text("↑/↓ or j/k scroll  Ctrl-d/u page  o outdated  v vulnerabilities  Esc back  r refresh  q quit").Dim(),
	)
}

func vulnerabilityView(data *model) (glyph.Component, *glyph.ListC[vulnerabilityRow]) {
	list := glyph.List(&data.findings).
		Render(func(row *vulnerabilityRow) glyph.Component {
			return glyph.HBox.Gap(1)(
				glyph.Text(&row.Source).Width(6), glyph.Text(&row.Manager).Width(8), glyph.Text(&row.Package).Width(22),
				glyph.Text(&row.Current).Width(14), glyph.Text(&row.Status).Width(20), glyph.Text(&row.Advisory).Width(22),
				glyph.Text(&row.UpgradeTo).Width(14).FG(glyph.Green),
			)
		}).Marker("› ").MarkerStyle(glyph.Style{FG: glyph.Cyan}).
		SelectedStyle(glyph.Style{BG: glyph.RGB(40, 48, 60), Attr: glyph.AttrBold})
	header := glyph.HBox.Gap(1)(
		glyph.Text("Source").Width(6).Bold(), glyph.Text("Manager").Width(8).Bold(), glyph.Text("Tool").Width(22).Bold(),
		glyph.Text("Current").Width(14).Bold(), glyph.Text("Status").Width(20).Bold(), glyph.Text("CVE / Advisory").Width(22).Bold(),
		glyph.Text("Upgrade To").Width(14).Bold(),
	)
	return glyph.VBox.Grow(1).Border(glyph.BorderRounded).Title("Vulnerabilities")(
		header, loadingContent(&data.loading, list),
		glyph.Text(&data.status).Dim(),
		glyph.Text("↑/↓ or j/k move  Ctrl-d/u page  o outdated  d drift  Esc back  r refresh  q quit").Dim(),
	), list
}

func refresh(data *model, app *glyph.App) {
	if data.cancel != nil {
		data.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	data.cancel = cancel
	data.generation++
	generation := data.generation
	data.loading = true
	data.status = ""
	scopeSelection := selectedScopes(data)
	profile := data.profile.Profile
	mappings := append([]config.VulnerabilityMapping(nil), data.mappings...)
	euvdMaps := append([]config.EUVDMapping(nil), data.euvdMaps...)
	app.RequestRender()
	go func() {
		scopes := make([]packagemanager.Scope, len(scopeSelection))
		for i, scope := range scopeSelection {
			scopes[i] = packagemanager.Scope{Kind: scope.Kind, Path: scope.Path}
		}
		options := packagemanager.QueryOptionsFromProfile(profile)
		result, outdatedErr := packagemanager.QueryOutdatedScopesWithOptions(ctx, nil, scopes, options)
		inventory, inventoryErr := packagemanager.QueryInstalledScopesWithOptions(ctx, nil, scopes, options)
		if includesGlobal(scopeSelection) {
			managerPackages, managerErr := packagemanager.ManagerPackages(ctx, inventory.Managers)
			inventory.Packages = append(inventory.Packages, managerPackages...)
			inventoryErr = errors.Join(inventoryErr, managerErr)
		}
		packages := packagemanager.Records(result.Packages)
		installed := make([]config.InstalledVersion, len(inventory.Packages))
		for i, pkg := range inventory.Packages {
			installed[i] = config.InstalledVersion{Manager: pkg.Manager, Name: pkg.Name, Scope: pkg.Scope, Path: pkg.Path, Version: pkg.Version}
		}
		selected, drift := config.EvaluatePackages(profile, installed)
		packages = filterOutdated(packages, selected)
		scanPackages := inventory.Packages
		if profile.SecurityScope == "selected" {
			scanPackages = filterInstalled(inventory.Packages, selected)
		}
		findings, _ := vulnerability.Scan(ctx, scanPackages, mappings, vulnerability.NewClient(http.DefaultClient, os.Getenv("CONFICIUS_OSV_URL")))
		euvdFindings, _ := euvd.Scan(ctx, scanPackages, packages, euvdMaps, euvd.NewClient(http.DefaultClient, os.Getenv("CONFICIUS_EUVD_URL")))
		if ctx.Err() != nil {
			return
		}
		err := errors.Join(outdatedErr, inventoryErr)
		app.Apply(func() {
			if !isCurrentGeneration(data.generation, generation) {
				return
			}
			data.loading = false
			data.query = result
			data.packages = packages
			for key := range data.selected {
				if !containsPackage(packages, key) {
					delete(data.selected, key)
				}
			}
			data.rows = outdatedRows(packages, data.selected)
			data.drift = drift
			data.findings = vulnerabilityRows(findings)
			data.findings = append(data.findings, euvdRows(euvdFindings)...)
			data.status = fmt.Sprintf("%d outdated, %d drift, %d vulnerabilities", len(packages), len(drift), len(data.findings))
			if err != nil {
				data.status = err.Error()
			}
		})
	}()
}

func loadingContent(loading *bool, content glyph.Component) glyph.Component {
	return glyph.VBox.Grow(1)(glyph.If(loading).
		Then(glyph.HBox.Gap(1)(glyph.Spinner().FG(glyph.Cyan), glyph.Text("Loading...").FG(glyph.Cyan))).
		Else(content))
}

func coloredText(color glyph.Color) glyph.ColumnOption {
	return func(column *glyph.ColumnConfig) {
		column.Style(func(any) glyph.Style { return glyph.Style{FG: color} })
	}
}

func statusStyle() glyph.ColumnOption {
	return func(column *glyph.ColumnConfig) {
		column.Style(func(value any) glyph.Style {
			switch value {
			case vulnerability.StatusVulnerable, config.DriftBehind, config.DriftMissing:
				return glyph.Style{FG: glyph.Red}
			case vulnerability.StatusError, security.StatusUnknownVersionRule, config.DriftUnsupported:
				return glyph.Style{FG: glyph.Yellow}
			default:
				return glyph.Style{FG: glyph.Green}
			}
		})
	}
}

func euvdRows(findings []euvd.Finding) []vulnerabilityRow {
	rows := make([]vulnerabilityRow, 0, len(findings))
	for _, finding := range findings {
		if finding.Status == security.StatusClean {
			continue
		}
		rows = append(rows, vulnerabilityRow{Source: security.SourceEUVD, Manager: finding.Manager, Package: finding.Package, Current: finding.Version, Status: finding.Status, Advisory: finding.Advisory, UpgradeTo: finding.UpgradeTo})
	}
	return rows
}

func vulnerabilityRows(findings []vulnerability.Finding) []vulnerabilityRow {
	rows := make([]vulnerabilityRow, 0, len(findings))
	for _, finding := range findings {
		if finding.Status == vulnerability.StatusClean {
			continue
		}
		for _, advisory := range finding.Advisories {
			rows = append(rows, vulnerabilityRow{Source: security.SourceOSV, Manager: finding.Manager, Package: finding.Package, Current: finding.Version, Status: finding.Status, Advisory: advisory.ID, UpgradeTo: advisory.FixedVersion})
		}
		if len(finding.Advisories) == 0 {
			rows = append(rows, vulnerabilityRow{Source: security.SourceOSV, Manager: finding.Manager, Package: finding.Package, Current: finding.Version, Status: finding.Status})
		}
	}
	return rows
}

func selectScope(data *model, scope *config.Scope) {
	data.active = scope
}

func filterOutdated(packages []packagemanager.Package, selected []config.InstalledVersion) []packagemanager.Package {
	allowed := selectedKeys(selected)
	result := packages[:0]
	for _, pkg := range packages {
		if _, ok := allowed[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)]; ok {
			result = append(result, pkg)
		}
	}
	return result
}

func filterInstalled(packages []packagemanager.InstalledPackage, selected []config.InstalledVersion) []packagemanager.InstalledPackage {
	allowed := selectedKeys(selected)
	result := packages[:0]
	for _, pkg := range packages {
		if _, ok := allowed[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)]; ok {
			result = append(result, pkg)
		}
	}
	return result
}

func selectedKeys(packages []config.InstalledVersion) map[string]struct{} {
	keys := make(map[string]struct{}, len(packages))
	for _, pkg := range packages {
		keys[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)] = struct{}{}
	}
	return keys
}

func packageKey(scope, path, manager, name string) string {
	return scope + "\x00" + path + "\x00" + manager + "\x00" + name
}

func isCurrentGeneration(current, completed uint64) bool {
	return current == completed
}

func includesGlobal(scopes []config.Scope) bool {
	for _, scope := range scopes {
		if scope.Kind == "global" {
			return true
		}
	}
	return false
}

func selectedScopes(data *model) []config.Scope {
	if data.active != nil {
		return []config.Scope{*data.active}
	}
	return data.scopes
}
