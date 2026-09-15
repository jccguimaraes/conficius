package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	glyph "github.com/kungfusheep/glyph"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

var version = "dev"

type model struct {
	profile  config.ResolvedProfile
	scopes   []config.Scope
	packages []packagemanager.Package
	status   string
	cancel   context.CancelFunc
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

	data := &model{profile: profile, scopes: scopes, status: "Loading..."}
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
			label := scope.Kind
			if scope.Name != "" {
				label += ":" + scope.Name
			}
			if scope.Path != "" {
				label += "  " + scope.Path
			}
			return glyph.Text(label)
		}).
		BindNav("j", "k").
		OnSelect(func(_ *config.Scope) { app.Go("results") })
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
			return glyph.Text(fmt.Sprintf("%s:%s  %s -> %s  [%s]", pkg.Manager, pkg.Name, pkg.Current, pkg.Latest, pkg.Scope))
		}).
		BindVimNav()
	return glyph.VBox.Border(glyph.BorderRounded).Title("Outdated packages")(
		list,
		glyph.Text(&data.status).Dim(),
		glyph.Text("j/k move  Esc back  r refresh  q quit").Dim(),
	), list
}

func refresh(data *model, app *glyph.App) {
	if data.cancel != nil {
		data.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	data.cancel = cancel
	data.status = "Loading..."
	app.RequestRender()
	go func() {
		scopes := make([]packagemanager.Scope, len(data.scopes))
		for i, scope := range data.scopes {
			scopes[i] = packagemanager.Scope{Kind: scope.Kind, Path: scope.Path}
		}
		result, err := packagemanager.QueryOutdatedScopes(ctx, nil, scopes)
		if ctx.Err() != nil {
			return
		}
		app.Apply(func() {
			data.packages = packagemanager.Records(result.Packages)
			data.status = fmt.Sprintf("%d outdated packages", len(data.packages))
			if err != nil {
				data.status = err.Error()
			}
		})
	}()
}
