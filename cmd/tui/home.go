package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/jccguimaraes/conficius/internal/audit"
	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	glyph "github.com/kungfusheep/glyph"
)

type homeCard struct {
	name, view, summary string
	color               glyph.Color
}

func parseStartupFlags(args []string, output io.Writer) (string, error) {
	flags := flag.NewFlagSet("conficius-tui", flag.ContinueOnError)
	flags.SetOutput(output)
	path := flags.String("config", "", "configuration path")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	if flags.NArg() != 0 {
		return "", fmt.Errorf("unexpected argument: %s", flags.Arg(0))
	}
	return *path, nil
}

func evaluateInventory(profile config.Profile, packages []packagemanager.InstalledPackage) ([]config.InstalledVersion, []config.PackageState) {
	installed := make([]config.InstalledVersion, len(packages))
	for i, pkg := range packages {
		installed[i] = config.InstalledVersion{Manager: pkg.Manager, Name: pkg.Name, Scope: pkg.Scope, Path: pkg.Path, Version: pkg.Version}
	}
	return config.EvaluatePackages(profile, installed)
}

func dotfileHomeSummary(state *dotfileState) (string, glyph.Color) {
	if state == nil {
		return "Configuration unavailable", glyph.Red
	}
	color := glyph.Green
	if state.configErr != nil || state.project == nil {
		color = glyph.Red
	}
	for _, row := range state.rows {
		if row.group == 1 {
			color = glyph.Red
		} else if row.group == 0 && color != glyph.Red {
			color = glyph.Yellow
		}
	}
	for _, finding := range state.findings {
		if finding.Severity == audit.SeverityError {
			color = glyph.Red
		} else if finding.Severity == audit.SeverityWarning && color != glyph.Red {
			color = glyph.Yellow
		}
	}
	parts := []string{"Config: " + state.configPath, state.environment, state.links}
	if state.configErr != nil {
		parts = append(parts, "Configuration error: "+strings.SplitN(state.configErr.Error(), "\n", 2)[0])
	}
	return strings.Join(parts, "\n"), color
}

func packageHomeSummary(data *model) (string, glyph.Color) {
	local := "Local inventory: loading; drift unknown"
	color := glyph.Cyan
	if data.localChecked && !data.localLoading {
		local = fmt.Sprintf("Local inventory: %d installed; %d drift", data.installedCount, len(data.drift))
		color = glyph.Green
		if len(data.drift) > 0 {
			color = glyph.Yellow
		}
		if data.localErr != nil {
			local += " (inspection failed; partial counts)"
			color = glyph.Red
		}
	}
	outdated := "Outdated: unknown (network pending)"
	if data.outdatedChecked {
		outdated = fmt.Sprintf("Outdated: %d", len(data.packages))
		if len(data.packages) > 0 && color != glyph.Red {
			color = glyph.Yellow
		}
		if data.outdatedErr != nil {
			outdated = "Outdated: unknown (inspection failed)"
			color = glyph.Red
		}
	}
	security := "Security: unknown (network pending)"
	if data.securityScanning {
		security = "Security: unknown (scanning)"
	} else if data.securityChecked {
		security = fmt.Sprintf("Security: %d packages checked; %d vulnerable; %d unknown; %d errors", data.securityCount, data.securitySummary.Vulnerable, data.securitySummary.Unknown, data.securitySummary.Errors)
		if data.securitySummary.Vulnerable > 0 || data.securitySummary.Errors > 0 || data.securityErr != nil {
			color = glyph.Red
		} else if data.securitySummary.Unknown > 0 && color != glyph.Red {
			color = glyph.Yellow
		}
		if data.securityErr != nil {
			security += " (scan failed; partial counts)"
		}
	}
	return strings.Join([]string{local, outdated, security}, "\n"), color
}

func updateHome(data *model) {
	if len(data.homeCards) != 2 {
		return
	}
	data.homeCards[0].summary, data.homeCards[0].color = dotfileHomeSummary(data.dotfiles)
	data.homeCards[1].summary, data.homeCards[1].color = packageHomeSummary(data)
}

func homeView(data *model, app *glyph.App) (glyph.Component, *glyph.ListC[homeCard]) {
	data.homeCards = []homeCard{{name: "Dotfiles", view: "dotfiles"}, {name: "Packages", view: "scopes"}}
	updateHome(data)
	cards := glyph.List(&data.homeCards).Render(func(card *homeCard) glyph.Component {
		return glyph.VBox.Border(glyph.BorderRounded)(
			glyph.Text(&card.name).Bold(),
			glyph.TextBlock(&card.summary).CharWrap().FG(&card.color),
		)
	}).BindNav("j", "k").Handle("<Enter>", func(card *homeCard) {
		if card.view == "dotfiles" && data.dotfileCancel != nil {
			data.dotfileCancel()
		}
		app.Go(card.view)
	})
	return glyph.VBox.Grow(1).Border(glyph.BorderRounded).Title("Conficius "+version)(
		cards,
		glyph.Text("↑/↓ or j/k move  Enter open  r full refresh  q quit").Dim(),
	), cards
}

func registerHome(data *model, app *glyph.App) {
	view, cards := homeView(data, app)
	// Dotfiles refresh applies asynchronously to the same bound state.
	app.OnBeforeRender(func() { updateHome(data) })
	app.View("home", view).Handle("<down>", cards.Down).Handle("<up>", cards.Up).
		Handle("<Esc>", func() {}).Handle("q", app.Stop).
		Handle("r", func() {
			data.active = nil
			refresh(data, app)
			refreshHomeDotfiles(data, app)
		})
}

func refreshHomeDotfiles(data *model, app *glyph.App) {
	if data.dotfiles == nil || data.dotfiles.pending {
		return
	}
	if data.dotfileCancel != nil {
		data.dotfileCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	data.dotfileCancel = cancel
	data.dotfileGeneration++
	generation := data.dotfileGeneration
	snapshot := *data.dotfiles
	go func() {
		snapshot.refresh(nil)
		app.Apply(func() {
			if ctx.Err() != nil || generation != data.dotfileGeneration || data.dotfiles.pending {
				return
			}
			snapshot.mode, snapshot.filter, snapshot.selection = data.dotfiles.mode, data.dotfiles.filter, data.dotfiles.selection
			snapshot.environmentSummary()
			snapshot.rebuild()
			*data.dotfiles = snapshot
			updateHome(data)
		})
	}()
}
