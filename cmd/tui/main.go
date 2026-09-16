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
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/jccguimaraes/conficius/internal/vulnerability"
)

var version = "dev"

type model struct {
	profile    config.ResolvedProfile
	scopes     []config.Scope
	active     *config.Scope
	packages   []packagemanager.Package
	drift      []config.PackageState
	findings   []vulnerability.Finding
	mappings   []config.VulnerabilityMapping
	status     string
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
	resultComponent, resultList := resultView(data)
	app.View("results", resultComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("scopes") }).
		Handle("<down>", resultList.Down).
		Handle("<up>", resultList.Up).
		Handle("r", func() { refresh(data, app) }).
		Handle("d", func() { app.Go("drift") }).
		Handle("v", func() { app.Go("vulnerabilities") })
	driftComponent, driftList := driftView(data)
	app.View("drift", driftComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("results") }).
		Handle("<down>", driftList.Down).
		Handle("<up>", driftList.Up).
		Handle("o", func() { app.Go("results") }).
		Handle("v", func() { app.Go("vulnerabilities") }).
		Handle("r", func() { refresh(data, app) })
	vulnerabilityComponent, vulnerabilityList := vulnerabilityView(data)
	app.View("vulnerabilities", vulnerabilityComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("results") }).
		Handle("<down>", vulnerabilityList.Down).
		Handle("<up>", vulnerabilityList.Up).
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
		glyph.Text("j/k move  Enter open  r refresh  q quit").Dim(),
	), list
}

func resultView(data *model) (glyph.Component, *glyph.ListC[packagemanager.Package]) {
	list := glyph.List(&data.packages).
		Render(func(pkg *packagemanager.Package) glyph.Component {
			return glyph.Textf(&pkg.Manager, ":", &pkg.Name, "  ", &pkg.Current, " -> ", &pkg.Latest, "  [", &pkg.Scope, "]")
		}).
		BindVimNav()
	return glyph.VBox.Border(glyph.BorderRounded).Title("Outdated packages")(
		list,
		glyph.Text(&data.status).Dim(),
		glyph.Text("j/k move  d drift  v vulnerabilities  Esc back  r refresh  q quit").Dim(),
	), list
}

func driftView(data *model) (glyph.Component, *glyph.ListC[config.PackageState]) {
	list := glyph.List(&data.drift).
		Render(func(state *config.PackageState) glyph.Component {
			return glyph.Textf(&state.Manager, ":", &state.Name, "  ", &state.Installed, " -> ", &state.DesiredVersion, "  [", &state.Status, "]")
		}).BindVimNav()
	return glyph.VBox.Border(glyph.BorderRounded).Title("Profile drift")(
		list,
		glyph.Text(&data.status).Dim(),
		glyph.Text("j/k move  o outdated  v vulnerabilities  Esc back  r refresh  q quit").Dim(),
	), list
}

func vulnerabilityView(data *model) (glyph.Component, *glyph.ListC[vulnerability.Finding]) {
	list := glyph.List(&data.findings).
		Render(func(finding *vulnerability.Finding) glyph.Component {
			return glyph.Textf(&finding.Manager, ":", &finding.Package, "@", &finding.Version, "  ", &finding.Status)
		}).BindVimNav()
	return glyph.VBox.Border(glyph.BorderRounded).Title("Vulnerabilities")(
		list,
		glyph.Text(&data.status).Dim(),
		glyph.Text("j/k move  o outdated  d drift  Esc back  r refresh  q quit").Dim(),
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
			data.packages = packages
			data.drift = drift
			data.findings = findings
			data.status = fmt.Sprintf("%d outdated, %d drift, %d findings", len(packages), len(drift), len(findings))
			if err != nil {
				data.status = err.Error()
			}
		})
	}()
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

func selectedScopes(data *model) []config.Scope {
	if data.active != nil {
		return []config.Scope{*data.active}
	}
	return data.scopes
}
