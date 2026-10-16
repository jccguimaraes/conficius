package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/jccguimaraes/conficius/internal/audit"
	"github.com/jccguimaraes/conficius/internal/wisdom"
	glyph "github.com/kungfusheep/glyph"
)

type homeCard struct {
	name, view, summary, label string
	color                      glyph.Color
	nameColor                  glyph.Color
	gap                        bool
}

func parseStartupFlags(args []string, output io.Writer) (string, error) {
	dotfiles, _, err := parseConfigFlags(args, output)
	return dotfiles, err
}

func parseConfigFlags(args []string, output io.Writer) (string, string, error) {
	flags := flag.NewFlagSet("conficius-tui", flag.ContinueOnError)
	flags.SetOutput(output)
	path := flags.String("config", "", "dotfiles configuration path")
	packages := flags.String("package-config", "", "package configuration path")
	if err := flags.Parse(args); err != nil {
		return "", "", err
	}
	if flags.NArg() != 0 {
		return "", "", fmt.Errorf("unexpected argument: %s", flags.Arg(0))
	}
	return *path, *packages, nil
}

func dotfileHomeSummary(state *dotfileState) (string, glyph.Color) {
	if state == nil || (state.project == nil && state.configErr == nil) || state.missingConfig() {
		return "Add your dotfiles when you're ready.\nRun conficius init in your dotfiles repo.", glyph.DefaultColor()
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
	parts := []string{strings.TrimPrefix(state.environment, "Environment: ") + " · " + state.links}
	if state.configErr != nil {
		parts = append(parts, "Configuration error: "+strings.SplitN(state.configErr.Error(), "\n", 2)[0])
	}
	return strings.Join(parts, "\n"), color
}

func packageHomeSummary(data *model) (string, glyph.Color) {
	local := "Local: loading"
	color := glyph.Cyan
	if data.localChecked && !data.localLoading {
		local = fmt.Sprintf("Local: %d installed", data.installedCount)
		color = glyph.Green
		if data.localErr != nil {
			local += " (partial; failed)"
			color = glyph.Red
		}
	}
	outdated := "Outdated: unknown"
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
	security := "Security: unknown"
	if data.securityScanning {
		security = "Security: unknown (scanning)"
	} else if data.securityChecked {
		security = fmt.Sprintf("Security: %d checked; %d vulnerable; %d unknown; %d errors", data.securityCount, data.securitySummary.Vulnerable, data.securitySummary.Unknown, data.securitySummary.Errors)
		if data.securitySummary.Vulnerable > 0 || data.securitySummary.Errors > 0 || data.securityErr != nil {
			color = glyph.Red
		} else if data.securitySummary.Unknown > 0 && color != glyph.Red {
			color = glyph.Yellow
		}
		if data.securityErr != nil {
			security += " (partial; failed)"
		}
	}
	parts := []string{local, outdated, security}
	if data.packageConfigErr != nil {
		parts = append(parts, "Package configuration error: "+strings.SplitN(data.packageConfigErr.Error(), "\n", 2)[0])
		color = glyph.Red
	}
	return strings.Join(parts, "\n"), color
}

func updateHome(data *model) {
	data.showWisdom = terminalWidth() >= 80 && screenSize.Height >= 16
	for i := range data.homeCards {
		card := &data.homeCards[i]
		if card.view == "scopes" {
			card.summary, card.color = packageHomeSummary(data)
		} else {
			card.summary, card.color = dotfileHomeSummary(data.dotfiles)
		}
		switch card.color {
		case glyph.Green:
			card.label = "Healthy"
		case glyph.Yellow:
			card.label = "Attention"
		case glyph.Red:
			card.label = "Error"
		default:
			card.label = "Loading"
			if card.view == "dotfiles" {
				card.label = "Optional"
			}
		}
	}
}

func homeView(data *model, app *glyph.App) (glyph.Component, *glyph.ListC[homeCard]) {
	if data.wisdom == "" {
		data.wisdom = wisdom.Random()
	}
	data.homeCards = []homeCard{{name: "Packages", view: "scopes", nameColor: glyph.Cyan}}
	if data.dotfiles != nil {
		data.homeCards[0].gap = true
		data.homeCards = append(data.homeCards, homeCard{name: "Dotfiles", view: "dotfiles"})
	}
	updateHome(data)
	cards := styleList(glyph.List(&data.homeCards)).MarkerStyle(glyph.Style{FG: glyph.Cyan, Attr: glyph.AttrBold}).SelectedStyle(glyph.Style{}).Render(func(card *homeCard) glyph.Component {
		return glyph.VBox(
			glyph.HBox.Gap(1)(glyph.Text(&card.name).Bold().FG(&card.nameColor), glyph.Text(&card.label).FG(&card.color)),
			glyph.TextBlock(&card.summary),
			glyph.If(&card.gap).Then(glyph.Text("")),
		)
	}).OnSelect(func(selected *homeCard) {
		for i := range data.homeCards {
			data.homeCards[i].nameColor = glyph.DefaultColor()
		}
		selected.nameColor = glyph.Cyan
	}).BindNav("j", "k").Handle("<Enter>", func(card *homeCard) {
		if card.view == "dotfiles" && data.dotfileCancel != nil {
			data.dotfileCancel()
		}
		app.Go(card.view)
	})
	context := func() string {
		if data.dotfiles == nil || data.dotfiles.configPath == "" {
			return "Config: none"
		}
		return "Config: " + glyph.TruncateWidthEllipsis(filepath.Base(data.dotfiles.configPath), max(1, terminalWidth()-12))
	}
	content := glyph.VBox.Grow(1)(
		glyph.If(&data.showWisdom).Then(glyph.VBox(glyph.Text(func() string {
			return glyph.TruncateWidthEllipsis(data.wisdom, max(1, terminalWidth()-4))
		}).Dim(), glyph.Text(""))),
		cards,
	)
	return screenFrame("Home · "+version, context, nil, content, "↑/↓ move  Enter open  p profiles  r/q  ? help"), cards
}

func registerHome(data *model, app *glyph.App) {
	view, cards := homeView(data, app)
	app.OnBeforeRender(func() { updateHome(data) })
	app.View("home", view).Handle("<down>", cards.Down).Handle("<up>", cards.Up).
		Handle("<Esc>", func() {}).Handle("q", app.Stop).
		Handle("p", func() { app.Go("profiles") }).
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
