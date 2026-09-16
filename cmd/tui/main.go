package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	glyph "github.com/kungfusheep/glyph"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/jccguimaraes/conficius/internal/vulnerability"
)

var version = "dev"

type vulnerabilityRow struct {
	Manager   string
	Package   string
	Current   string
	Status    string
	Advisory  string
	UpgradeTo string
}

type model struct {
	profile    config.ResolvedProfile
	scopes     []config.Scope
	active     *config.Scope
	packages   []packagemanager.Package
	drift      []config.PackageState
	findings   []vulnerabilityRow
	mappings   []config.VulnerabilityMapping
	status     string
	loading    bool
	cancel     context.CancelFunc
	generation uint64
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

	data := &model{profile: profile, scopes: scopes, mappings: project.Vulnerabilities.Mappings, status: "Loading..."}
	app := glyph.NewApp()
	app.HideCursor()
	scopeComponent, scopeList := scopeView(data, app)
	app.View("scopes", scopeComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", app.Stop).
		Handle("<down>", scopeList.Down).
		Handle("<up>", scopeList.Up).
		Handle("r", func() { refresh(data, app) })
	app.View("results", resultView(data)).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("scopes") }).
		Handle("r", func() { refresh(data, app) }).
		Handle("d", func() { app.Go("drift") }).
		Handle("v", func() { app.Go("vulnerabilities") })
	app.View("drift", driftView(data)).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("results") }).
		Handle("o", func() { app.Go("results") }).
		Handle("v", func() { app.Go("vulnerabilities") }).
		Handle("r", func() { refresh(data, app) })
	app.View("vulnerabilities", vulnerabilityView(data)).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("results") }).
		Handle("o", func() { app.Go("results") }).
		Handle("d", func() { app.Go("drift") }).
		Handle("r", func() { refresh(data, app) })
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
	return glyph.VBox.Border(glyph.BorderRounded).Title("Conficius "+version)(
		glyph.Text("Profile: "+data.profile.Name).Bold(),
		glyph.Text("Scopes").Underline(),
		list,
		glyph.Text(&data.status).Dim(),
		glyph.Text("↑/↓ or j/k move  Enter open  r refresh  q quit").Dim(),
	), list
}

func resultView(data *model) glyph.Component {
	table := glyph.AutoTable(&data.packages).
		Columns("Manager", "Name", "Current", "Latest", "Scope").
		Headers("Manager", "Tool", "Current", "Latest", "Scope").
		Column("Latest", coloredText(glyph.Green)).
		Scrollable(1000).BindVimNav().BindNav("<down>", "<up>")
	return glyph.VBox.Border(glyph.BorderRounded).Title("Outdated packages")(
		loadingContent(&data.loading, table),
		glyph.Text(&data.status).Dim(),
		glyph.Text("↑/↓ or j/k scroll  Ctrl-d/u page  d drift  v vulnerabilities  Esc back  r refresh  q quit").Dim(),
	)
}

func driftView(data *model) glyph.Component {
	table := glyph.AutoTable(&data.drift).
		Columns("Manager", "Name", "Installed", "DesiredVersion", "Status", "Scope").
		Headers("Manager", "Tool", "Installed", "Desired", "Status", "Scope").
		Column("Status", statusStyle()).
		Column("DesiredVersion", coloredText(glyph.Green)).
		Scrollable(1000).BindVimNav().BindNav("<down>", "<up>")
	return glyph.VBox.Border(glyph.BorderRounded).Title("Profile drift")(
		loadingContent(&data.loading, table),
		glyph.Text(&data.status).Dim(),
		glyph.Text("↑/↓ or j/k scroll  Ctrl-d/u page  o outdated  v vulnerabilities  Esc back  r refresh  q quit").Dim(),
	)
}

func vulnerabilityView(data *model) glyph.Component {
	table := glyph.AutoTable(&data.findings).
		Columns("Manager", "Package", "Current", "Status", "Advisory", "UpgradeTo").
		Headers("Manager", "Tool", "Current", "Status", "CVE / Advisory", "Upgrade To").
		Column("Status", statusStyle()).
		Column("UpgradeTo", coloredText(glyph.Green)).
		Scrollable(1000).BindVimNav().BindNav("<down>", "<up>")
	return glyph.VBox.Border(glyph.BorderRounded).Title("Vulnerabilities")(
		loadingContent(&data.loading, table),
		glyph.Text(&data.status).Dim(),
		glyph.Text("↑/↓ or j/k scroll  Ctrl-d/u page  o outdated  d drift  Esc back  r refresh  q quit").Dim(),
	)
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
	data.status = "Loading..."
	scopeSelection := selectedScopes(data)
	profile := data.profile.Profile
	mappings := append([]config.VulnerabilityMapping(nil), data.mappings...)
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
		findings, scanErr := vulnerability.Scan(ctx, scanPackages, mappings, vulnerability.NewClient(http.DefaultClient, os.Getenv("CONFICIUS_OSV_URL")))
		if ctx.Err() != nil {
			return
		}
		err := errors.Join(outdatedErr, inventoryErr, scanErr)
		app.Apply(func() {
			if !isCurrentGeneration(data.generation, generation) {
				return
			}
			data.loading = false
			data.packages = packages
			data.drift = drift
			data.findings = vulnerabilityRows(findings)
			data.status = fmt.Sprintf("%d outdated, %d drift, %d vulnerabilities", len(packages), len(drift), len(data.findings))
			if err != nil {
				data.status = err.Error()
			}
		})
	}()
}

func loadingContent(loading *bool, content glyph.Component) glyph.Component {
	return glyph.If(loading).
		Then(glyph.HBox.Gap(1)(glyph.Spinner().FG(glyph.Cyan), glyph.Text("Loading...").FG(glyph.Cyan))).
		Else(glyph.VBox.Grow(1)(content))
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
			case vulnerability.StatusError, config.DriftUnsupported:
				return glyph.Style{FG: glyph.Yellow}
			default:
				return glyph.Style{FG: glyph.Green}
			}
		})
	}
}

func vulnerabilityRows(findings []vulnerability.Finding) []vulnerabilityRow {
	rows := make([]vulnerabilityRow, 0, len(findings))
	for _, finding := range findings {
		if finding.Status != vulnerability.StatusVulnerable && finding.Status != vulnerability.StatusError {
			continue
		}
		rows = append(rows, vulnerabilityRow{Manager: finding.Manager, Package: finding.Package, Current: finding.Version, Status: finding.Status, Advisory: vulnerabilityIDs(finding.Advisories), UpgradeTo: fixedVersions(finding.Advisories)})
	}
	return rows
}

func vulnerabilityIDs(advisories []vulnerability.Advisory) string {
	ids := make([]string, len(advisories))
	for i, item := range advisories {
		ids[i] = item.ID
	}
	return strings.Join(ids, ", ")
}

func fixedVersions(advisories []vulnerability.Advisory) string {
	seen := map[string]struct{}{}
	var versions []string
	for _, item := range advisories {
		if item.FixedVersion == "" {
			continue
		}
		if _, exists := seen[item.FixedVersion]; exists {
			continue
		}
		seen[item.FixedVersion] = struct{}{}
		versions = append(versions, item.FixedVersion)
	}
	return strings.Join(versions, ", ")
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
