package main

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	glyph "github.com/kungfusheep/glyph"
	"github.com/kungfusheep/riffkey"
)

var screenSize = glyph.Size{Width: 80, Height: 24}
var screenTooSmall bool

func updateScreenSize(width, height int) {
	screenSize = glyph.Size{Width: width, Height: height}
	screenTooSmall = terminalTooSmall()
}

func terminalWidth() int { return screenSize.Width }

func terminalTooSmall() bool { return screenSize.Width < 40 || screenSize.Height < 12 }

func styleList[T any](list *glyph.ListC[T]) *glyph.ListC[T] {
	return list.Marker("› ").MarkerStyle(glyph.Style{FG: glyph.Cyan}).SelectedStyle(glyph.Style{Attr: glyph.AttrBold})
}

func styledTextView(text *string) *glyph.TextViewC {
	viewer := glyph.TextView(text).Grow(1).BindNav("j", "k").BindNav("<down>", "<up>").BindPageNav("<C-d>", "<C-u>")
	layer := viewer.Layer()
	render := layer.Render
	var lastText string
	var lastWidth int
	layer.Render = func() {
		*text = strings.ReplaceAll(strings.ReplaceAll(*text, "\r\n", "\n"), "\r", "\n")
		width, scroll := layer.ViewportWidth(), layer.ScrollY()
		preserve := width != lastWidth && *text == lastText
		render()
		if preserve {
			layer.ScrollTo(scroll)
		}
		lastText, lastWidth = *text, width
	}
	return viewer
}

func detailTextView(text *string, title bool) *glyph.TextViewC {
	viewer := styledTextView(text)
	layer := viewer.Layer()
	render := layer.Render
	var lastText string
	var lastWidth int
	layer.Render = func() {
		render()
		width := layer.ViewportWidth()
		if width <= 0 || (*text == lastText && width == lastWidth) {
			return
		}
		lastText, lastWidth = *text, width
		buffer := layer.Buffer()
		x, y := 0, 0
		paint := func(value string, style glyph.Style) {
			for _, r := range value {
				if r == '\n' {
					x, y = 0, y+1
					continue
				}
				n := 1
				if r == '\t' {
					n = 4 - x%4
				}
				for range n {
					if x >= width {
						x, y = 0, y+1
					}
					buffer.SetStyle(x, y, style)
					x++
				}
			}
		}
		for i, section := range strings.Split(*text, "\n\n") {
			if i > 0 {
				paint("\n\n", glyph.Style{})
			}
			label, value, field := strings.Cut(section, "\n")
			if field && strings.Contains("|Origin|Package|Class|Scan scope|Manager|Scope|Path|Status|Current|Source|CVE/advisory|Source ID|CVSS|Unsupported rules|Aliases|Upgrade target|Upgrade note|Description|Reference|CVE URL|Reason/error|Error|Failure|Affected paths:|", "|"+label+"|") {
				paint(label, glyph.Style{FG: glyph.Cyan, Attr: glyph.AttrBold})
				paint("\n"+value, detailValueStyle(label, value))
			} else if i == 0 && title {
				paint(section, glyph.Style{FG: glyph.Cyan, Attr: glyph.AttrBold})
			} else {
				for j, line := range strings.Split(section, "\n") {
					if j > 0 {
						paint("\n", glyph.Style{})
					}
					for k, part := range strings.Split(line, "   ") {
						if k > 0 {
							paint("   ", glyph.Style{})
						}
						key, val, ok := strings.Cut(part, ": ")
						if ok {
							style := glyph.Style{Attr: glyph.AttrDim}
							if i == 0 && key == "Tool" {
								style = glyph.Style{FG: glyph.Cyan, Attr: glyph.AttrBold}
							}
							paint(key+": ", style)
							paint(val, detailValueStyle(key, val))
						} else {
							paint(part, glyph.Style{})
						}
					}
				}
			}
		}
	}
	return viewer
}

func detailValueStyle(label, value string) glyph.Style {
	switch label {
	case "Path", "Manifest", "Config", "Executable", "Config root", "Target", "Affected paths":
		return glyph.Style{Attr: glyph.AttrDim}
	case "Source", "Current":
		if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~") {
			return glyph.Style{Attr: glyph.AttrDim}
		}
	case "Affected paths:":
		return glyph.Style{Attr: glyph.AttrDim}
	case "Error", "Reason/error", "Failure", "Rollback incomplete":
		if value != "" {
			return glyph.Style{FG: glyph.Red}
		}
	case "Upgrade note":
		return glyph.Style{FG: glyph.Yellow}
	case "Status", "State":
		switch strings.ToLower(value) {
		case "vulnerable", "error", "blocked", "regular conflict · blocked", "unsafe link · blocked":
			return glyph.Style{FG: glyph.Red}
		case "unknown", "unmapped", "warning", "missing", "wrong link · safe":
			return glyph.Style{FG: glyph.Yellow}
		case "clean", "healthy", "ok":
			return glyph.Style{FG: glyph.Green}
		case "not applicable":
			return glyph.Style{Attr: glyph.AttrDim}
		}
	case "CVSS":
		score, _ := strconv.ParseFloat(value, 64)
		switch {
		case score >= 9:
			return glyph.Style{FG: glyph.Red}
		case score >= 4:
			return glyph.Style{FG: glyph.Yellow}
		case score > 0:
			return glyph.Style{FG: glyph.Blue}
		}
	}
	return glyph.Style{}
}

func screenContext(context any) string {
	var text string
	switch value := context.(type) {
	case string:
		text = value
	case *string:
		if value != nil {
			text = *value
		}
	case func() string:
		text = value()
	default:
		text = fmt.Sprint(value)
	}
	text = strings.Join(strings.Fields(text), " ")
	width := max(1, terminalWidth()-4)
	if glyph.StringWidth(text) <= width {
		return text
	}
	// Keep trailing path components visible instead of their shared prefix.
	runes := []rune(text)
	for len(runes) > 0 && glyph.StringWidth(string(runes))+1 > width {
		runes = runes[1:]
	}
	return "…" + string(runes)
}

func screenFooterActions(help string) []string {
	width := max(1, terminalWidth()-4)
	var primary []string
	for _, part := range strings.Split(help, "  ") {
		part = strings.Join(strings.Fields(part), " ")
		if part != "" && part != "? help" && part != "q quit" && part != "Esc back" {
			primary = append(primary, part)
		}
	}
	for len(primary) > 0 && glyph.StringWidth(strings.Join(append(primary, "? help"), " · ")) > width {
		primary = primary[:len(primary)-1]
	}
	actions := append(primary, "? help")
	for _, action := range []string{"Esc back", "q quit"} {
		if glyph.StringWidth(strings.Join(actions, " · ")+" · "+action) <= width {
			actions = append(actions, action)
		}
	}
	return actions
}

func screenFooter(help string) string {
	return strings.Join(screenFooterActions(help), " · ")
}

func screenFooterView(help string, scanning ...*bool) glyph.Component {
	fullHelp := help
	if len(scanning) > 0 {
		fullHelp = "x cancel scan  " + help
	}
	var parts []glyph.Component
	// Slots keep the compiled layout stable; Text functions remeasure on resize.
	for slot := range len(strings.Split(fullHelp, "  ")) + 3 {
		action := func() string {
			currentHelp := help
			if len(scanning) > 0 && *scanning[0] {
				currentHelp = fullHelp
			}
			actions := screenFooterActions(currentHelp)
			if slot < len(actions) {
				return actions[slot]
			}
			return ""
		}
		parts = append(parts,
			glyph.Text(func() string {
				if slot > 0 && action() != "" {
					return " · "
				}
				return ""
			}).Dim(),
			glyph.Text(func() string {
				key, _, _ := strings.Cut(action(), " ")
				return key
			}).FG(glyph.Cyan).Bold(),
			glyph.Text(func() string {
				_, description, _ := strings.Cut(action(), " ")
				if description != "" {
					return " " + description
				}
				return ""
			}).Dim(),
		)
	}
	return glyph.HBox(parts...)
}

func resizeNotice() glyph.Component {
	return glyph.VBox.Grow(1)(
		glyph.Text("Terminal too small").FG(glyph.Yellow).Bold(),
		glyph.Text(func() string {
			return fmt.Sprintf("Current %d×%d; minimum 40×12", screenSize.Width, screenSize.Height)
		}),
		glyph.Text("Resize to continue."),
		glyph.VBox.Grow(1)(),
		glyph.Text("q / Ctrl-C quit").Dim(),
	)
}

func screenFrame(title string, context any, status any, content glyph.Component, help string, scanning ...*bool) glyph.Component {
	parts := []glyph.Component{
		glyph.Text("Conficius / " + title).Bold(),
	}
	if context != nil {
		parts = append(parts, glyph.Text(func() string { return screenContext(context) }).Dim())
	}
	parts = append(parts,
		glyph.IfOrd(&screenSize.Height).Gte(16).Then(glyph.Text(func() string {
			return strings.Repeat("━", max(1, terminalWidth()-2))
		}).FG(glyph.Magenta)),
		glyph.VBox.Grow(1)(content),
	)
	if status != nil {
		parts = append(parts, glyph.HBox.Height(1)(glyph.Text(status)))
	}
	parts = append(parts,
		glyph.IfOrd(&screenSize.Height).Gte(16).Then(glyph.Text(func() string {
			return strings.Repeat("─", max(1, terminalWidth()-2))
		}).Dim()),
		screenFooterView(help, scanning...),
	)
	return glyph.VBox.Padding(1).Grow(1)(glyph.If(&screenTooSmall).Then(resizeNotice()).Else(glyph.VBox.Grow(1)(parts...)))
}

var screenHelp = map[string]string{
	"profiles":                    "↑/↓ or j/k move\nEnter choose profile\nr refresh\nEsc Home\nq / Ctrl-C quit",
	"home":                        "↑/↓ or j/k move\nEnter open area\np profiles\nr full refresh\nq / Ctrl-C quit",
	"scopes":                      "↑/↓ or j/k move\nEnter open scope\np profiles\nc update cache (confirmation)\nr full refresh\nEsc Home\nq / Ctrl-C quit",
	"results":                     "↑/↓ or j/k move\nCtrl-d / Ctrl-u page\nEnter package details\nSpace mark package\na toggle all\nu upgrade marked\nU upgrade all\np profiles\nv vulnerabilities\nx cancel scan\nc update cache (confirmation)\nr refresh\nEsc scopes\nq / Ctrl-C quit",
	"vulnerabilities":             "↑/↓ or j/k move\nCtrl-d / Ctrl-u page\nEnter evidence details (read-only)\ni toggle indirect scan\ng toggle separate graph-only scan\nx cancel scan\no outdated\nr refresh\nEsc outdated\nq / Ctrl-C quit",
	"vulnerability-detail":        "↑/↓ or j/k scroll\nCtrl-d / Ctrl-u page\nf then link label open CVE\nEsc vulnerabilities\nq / Ctrl-C quit",
	"package-detail":              "↑/↓ or j/k scroll\nCtrl-d / Ctrl-u page\nEsc package list\nq / Ctrl-C quit",
	"update-error":                "↑/↓ or j/k scroll\nEsc outdated\nq / Ctrl-C quit",
	"cache-confirm":               "y refresh APT cache only\nn / Esc cancel",
	"dirty-confirm":               "y continue to command preview\nn / Esc cancel",
	"confirm":                     "y apply upgrades\nn / Esc cancel\nq / Ctrl-C quit",
	"dotfiles":                    "↑/↓ or j/k move\nEnter open area\ne environment\nl links\nr refresh\nEsc Home\nq / Ctrl-C quit",
	"dotfiles-links":              "↑/↓ or j/k move\nTab filter\nSpace mark\na toggle visible\nEnter details\nl preview\nr refresh\nEsc Dotfiles\nq / Ctrl-C quit",
	"dotfiles-environment":        "↑/↓ or j/k move\nm toggle xdg/native\nl preview package configuration relocation\nEnter details\nr refresh\nEsc Dotfiles\nq / Ctrl-C quit",
	"dotfiles-detail":             "↑/↓ or j/k scroll\nCtrl-d / Ctrl-u page\nEsc links\nq / Ctrl-C quit",
	"dotfiles-environment-detail": "↑/↓ or j/k scroll\nCtrl-d / Ctrl-u page\nEsc environment\nq / Ctrl-C quit",
	"dotfiles-failure":            "↑/↓ or j/k scroll\nCtrl-d / Ctrl-u page\nEsc links\nq / Ctrl-C quit",
	"dotfiles-preview":            "↑/↓ scroll\ny apply links\nn / Esc cancel\nq / Ctrl-C quit",
	"dotfiles-layout-preview":     "↑/↓ scroll\ny apply package configuration layout\nn / Esc cancel\nq / Ctrl-C quit",
	"dotfiles-pending":            "Transaction pending. Quit waits for completion.\nq / Ctrl-C request quit",
}

func screenHelpForPlatform(name, goos string) string {
	help := screenHelp[name]
	if !cacheRefreshAvailable(goos) {
		help = strings.ReplaceAll(help, "\nc update cache (confirmation)", "")
	}
	return help
}

func helpTextView(text *string) *glyph.TextViewC {
	viewer := styledTextView(text).WordWrap()
	layer := viewer.Layer()
	render := layer.Render
	layer.Render = func() {
		render()
		_, actions, _ := strings.Cut(*text, "\n\n")
		buffer := layer.Buffer()
		inActions := false
		for y := range buffer.Height() {
			line := buffer.GetLine(y)
			if strings.TrimSpace(line) == "" {
				inActions = true
			}
			if !inActions {
				continue
			}
			for x := range buffer.Width() {
				buffer.SetStyle(x, y, glyph.Style{Attr: glyph.AttrDim})
			}
			for _, action := range strings.Split(actions, "\n") {
				key, rest, ok := strings.Cut(action, " ")
				if !ok {
					continue
				}
				if strings.HasPrefix(rest, "or ") || strings.HasPrefix(rest, "/ ") {
					separator, tail, _ := strings.Cut(rest, " ")
					alternate, _, _ := strings.Cut(tail, " ")
					key += " " + separator + " " + alternate
				}
				if !strings.HasPrefix(line, key+" ") {
					continue
				}
				for x := range glyph.StringWidth(key) {
					buffer.SetStyle(x, y, glyph.Style{FG: glyph.Cyan, Attr: glyph.AttrBold})
				}
			}
		}
	}
	return viewer
}

func registerScreenHelp(app *glyph.App, data *model) {
	var origin, context, text string
	var title string
	viewer := helpTextView(&text)
	stop := func() {
		if data.dotfiles != nil && data.dotfiles.pending {
			data.dotfiles.quitPending = true
			return
		}
		app.Stop()
	}
	toggle := func() {
		if terminalTooSmall() || (data.dotfiles != nil && data.dotfiles.pending) {
			return
		}
		if app.CurrentView() == "help" {
			app.Go(origin)
			return
		}
		origin = app.CurrentView()
		title = strings.ReplaceAll(origin, "-", " / ")
		context = fmt.Sprintf("%s · Profile: %s", title, data.profile.Name)
		if data.active != nil {
			context += fmt.Sprintf(" · Scope: %s:%s · Path: %s", data.active.Kind, data.active.Name, data.active.Path)
		}
		if data.dotfiles != nil {
			context += " · Config: " + data.dotfiles.configPath
			if strings.HasPrefix(origin, "dotfiles") {
				context += fmt.Sprintf(" · Layout: %s", data.dotfiles.mode)
			}
		}
		text = context + "\n\n" + screenHelpForPlatform(origin, runtime.GOOS) + "\n? toggle help\nEsc return without changing selection"
		viewer.Layer().ScrollTo(0)
		app.Go("help")
	}
	app.View("help", screenFrame("Help", &title, nil, viewer, "↑/↓ scroll  Esc back  q quit")).
		Handle("<Esc>", func() { app.Go(origin) }).Handle("?", toggle).Handle("q", stop).Handle("<C-c>", stop)
	for name := range screenHelp {
		if router, ok := app.ViewRouter(name); ok {
			router.Handle("?", func(riffkey.Match) { toggle(); app.RequestRender() })
			router.Handle("<C-c>", func(riffkey.Match) { stop(); app.RequestRender() })
		}
	}
}

func registerResizeView(app *glyph.App, data *model, startView string) (string, func()) {
	origin := startView
	stop := func() {
		if data.dotfiles != nil && data.dotfiles.pending {
			data.dotfiles.quitPending = true
			return
		}
		app.Stop()
	}
	app.View("resize", resizeNotice()).Handle("q", stop).Handle("<C-c>", stop)
	route := func() {
		current := app.CurrentView()
		if terminalTooSmall() {
			if current != "resize" {
				if current != "" {
					origin = current
				}
				app.ExitJumpMode()
				app.Go("resize")
			}
		} else if current == "resize" {
			app.Go(origin)
		}
	}
	app.Input().SetKeyInterceptor(func(riffkey.Key) { route() })
	app.OnResize(func(width, height int) {
		updateScreenSize(width, height)
		route()
	})
	app.OnBeforeRender(func() {
		updateHome(data)
		route()
	})
	if terminalTooSmall() {
		startView = "resize"
	}
	return startView, route
}

func preserveListFocus[T any](list *glyph.ListC[T], items *[]T, key func(*T) string) func(func()) {
	return func(update func()) {
		selectedKey := ""
		if row := list.Selected(); row != nil {
			selectedKey = key(row)
		}
		update()
		if selectedKey != "" {
			for i := range *items {
				if key(&(*items)[i]) == selectedKey {
					list.SetIndex(i)
					return
				}
			}
		}
		list.ClampSelection()
	}
}
