package main

import (
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/config"
	glyph "github.com/kungfusheep/glyph"
	"github.com/kungfusheep/riffkey"
)

func TestScreenFrameResize(t *testing.T) {
	previous := screenSize
	t.Cleanup(func() { updateScreenSize(previous.Width, previous.Height) })
	updateScreenSize(80, 24)
	context, status := "Profile: test", "Ready"
	template := glyph.Build(screenFrame("Packages", &context, &status, glyph.Text("Existing content"), "q quit"))
	render := func(width, height int) string {
		updateScreenSize(width, height)
		buffer := glyph.NewBuffer(width, height)
		template.Execute(buffer, int16(width), int16(height))
		var lines []string
		for row := range height {
			lines = append(lines, buffer.GetLine(row))
		}
		return strings.Join(lines, "\n")
	}
	text := render(80, 24)
	for _, want := range []string{"Conficius / Packages", context, status, "Existing content", "? help"} {
		if !strings.Contains(text, want) {
			t.Fatalf("frame missing %q: %s", want, text)
		}
	}
	for _, size := range [][2]int{{39, 12}, {40, 11}} {
		text = render(size[0], size[1])
		if !strings.Contains(text, "Terminal too small") || !strings.Contains(text, "Ctrl-C quit") || strings.Contains(text, "Existing content") {
			t.Fatalf("wrong minimum-size notice: %s", text)
		}
	}
	if text = render(40, 12); !strings.Contains(text, "Existing content") || terminalWidth() != 40 {
		t.Fatalf("frame did not recover: %s", text)
	}
}

func TestScreenFrameChrome(t *testing.T) {
	previous := screenSize
	t.Cleanup(func() { updateScreenSize(previous.Width, previous.Height) })
	context := "Profile: test · Config: /very/long/shared/config/path/dotfiles.toml"
	status := "Error: failed"
	template := glyph.Build(screenFrame("Links", func() string { return context }, &status, glyph.Text("content"), "Space mark  u preview  ? help"))
	for _, width := range []int{40, 80} {
		updateScreenSize(width, 12)
		buffer := glyph.NewBuffer(width, 12)
		template.Execute(buffer, int16(width), 12)
		if buffer.Get(0, 0).Rune != '╭' || buffer.Get(width-1, 11).Rune != '╯' {
			t.Fatal("frame did not fill terminal")
		}
		if !strings.Contains(buffer.GetLine(10), "Space mark · u preview · ? help") {
			t.Fatalf("footer not pinned: %q", buffer.GetLine(10))
		}
		if !strings.Contains(buffer.GetLine(2), "dotfiles.toml") {
			t.Fatalf("context lost trailing path: %q", buffer.GetLine(2))
		}
		if buffer.Get(1, 9).Style.Attr&glyph.AttrDim != 0 {
			t.Fatal("error status dimmed")
		}
		if glyph.StringWidth(screenFooter("Space mark  u preview  ? help")) > width-4 {
			t.Fatal("footer exceeds usable width")
		}
	}
	context = "Config: changed.toml"
	buffer := glyph.NewBuffer(80, 12)
	template.Execute(buffer, 80, 12)
	if !strings.Contains(buffer.GetLine(2), "changed.toml") {
		t.Fatal("context binder did not update")
	}
}

func TestScreenFooterRendering(t *testing.T) {
	previous := screenSize
	t.Cleanup(func() { updateScreenSize(previous.Width, previous.Height) })
	for _, help := range []string{
		"↑/↓ move  Enter details  Space mark  p profiles  r refresh",
		"↑/↓/j/k scroll  Ctrl-d/u page  y apply  n/Esc cancel",
		"r/q  n/Esc cancel",
		"q quit",
		"",
		strings.Repeat("x", 120) + " action",
	} {
		updateScreenSize(80, 12)
		template := glyph.Build(screenFrame("Footer", nil, "Ready", glyph.Text("content"), help))
		for _, width := range []int{40, 80, 120, 40, 120, 80} {
			updateScreenSize(width, 12)
			buffer := glyph.NewBuffer(width, 12)
			template.Execute(buffer, int16(width), 12)
			footer := screenFooter(help)
			line := buffer.GetLine(10)
			if !strings.Contains(line, footer) || !strings.Contains(line, "? help") || glyph.StringWidth(footer) > width-4 {
				t.Fatalf("width %d help %q: footer %q, want %q", width, help, line, footer)
			}
			if !strings.Contains(buffer.GetLine(9), "Ready") || buffer.Get(width-1, 10).Rune != '│' || buffer.Get(width-1, 11).Rune != '╯' {
				t.Fatalf("footer wrapped or overflowed at width %d: %q", width, line)
			}
			start := strings.Index(line, footer)
			x := glyph.StringWidth(line[:start])
			check := func(text string, key bool) {
				t.Helper()
				for _, r := range text {
					style := buffer.Get(x, 10).Style
					if key {
						if style.FG != glyph.Cyan || style.Attr&glyph.AttrBold == 0 || style.Attr&glyph.AttrDim != 0 {
							t.Fatalf("width %d key %q: style %+v", width, text, style)
						}
					} else if style.FG.Mode != glyph.ColorDefault || style.Attr&glyph.AttrDim == 0 || style.Attr&glyph.AttrBold != 0 {
						t.Fatalf("width %d description/separator %q: style %+v", width, text, style)
					}
					if style.BG.Mode != glyph.ColorDefault {
						t.Fatalf("footer background not terminal default: %+v", style)
					}
					x += glyph.RuneWidth(r)
				}
			}
			for i, action := range screenFooterActions(help) {
				if i > 0 {
					check(" · ", false)
				}
				key, description, _ := strings.Cut(action, " ")
				check(key, true)
				if description != "" {
					check(" "+description, false)
				}
			}
			for row := range 10 {
				if strings.Contains(buffer.GetLine(row), "? help") {
					t.Fatal("footer escaped single pinned row")
				}
			}
		}
	}
}

func TestResizeViewRouting(t *testing.T) {
	previous := screenSize
	t.Cleanup(func() { updateScreenSize(previous.Width, previous.Height) })
	updateScreenSize(80, 24)
	app := glyph.NewApp()
	var actions int
	app.View("origin", glyph.Text("content")).Handle("y", func() { actions++ })
	data := &model{dotfiles: &dotfileState{pending: true}}
	registerScreenHelp(app, data)
	start, route := registerResizeView(app, data, "origin")
	if start != "origin" {
		t.Fatalf("start = %q", start)
	}
	app.Go(start)
	// Model the active jump router without requiring a running terminal.
	jumpRouter := riffkey.NewRouter()
	jumpRouter.Handle("q", func(riffkey.Match) { actions++ })
	app.Input().Push(jumpRouter)
	app.JumpMode().Activate()
	updateScreenSize(39, 12)
	for _, key := range []riffkey.Key{{Rune: 'y'}, {Rune: '?'}, {Rune: 'j'}} {
		app.Input().Dispatch(key)
	}
	if app.JumpModeActive() {
		t.Fatal("resize left jump mode active")
	}
	if actions != 0 || app.CurrentView() != "resize" {
		t.Fatal("undersized view allowed action or help")
	}
	app.Input().Dispatch(riffkey.Key{Rune: 'q'})
	if !data.dotfiles.quitPending {
		t.Fatal("resize quit did not defer pending transaction")
	}
	updateScreenSize(40, 12)
	route()
	if app.CurrentView() != "origin" {
		t.Fatalf("restore = %q", app.CurrentView())
	}
	app.Input().Dispatch(riffkey.Key{Rune: '?'})
	if app.CurrentView() != "origin" {
		t.Fatal("pending transaction allowed help")
	}
	updateScreenSize(40, 11)
	start, _ = registerResizeView(app, data, "origin")
	if start != "resize" {
		t.Fatal("undersized startup did not select resize view")
	}
}

func TestScreenHelpDispatch(t *testing.T) {
	previous := screenSize
	t.Cleanup(func() { updateScreenSize(previous.Width, previous.Height) })
	updateScreenSize(80, 24)
	question, err := riffkey.NewReader(strings.NewReader("?")).ReadKey()
	if err != nil || question != (riffkey.Key{Rune: '?'}) {
		t.Fatalf("literal ? decoded as %+v: %v", question, err)
	}
	app := glyph.NewApp()
	data := &model{
		profile:  config.ResolvedProfile{Name: "test"},
		active:   &config.Scope{Kind: "project", Name: "demo", Path: "/tmp/demo"},
		dotfiles: &dotfileState{project: &config.Project{}, configPath: "/tmp/config.toml"},
		rows:     make([]outdatedRow, 3), findings: make([]vulnerabilityRow, 3),
	}
	registerHome(data, app)
	registerDotfiles(data.dotfiles, app)
	results, packages := resultView(data)
	app.View("results", results)
	findings, vulnerabilities := vulnerabilityView(data)
	app.View("vulnerabilities", findings)
	text := strings.Repeat("detail line\n", 80)
	viewer := styledTextView(&text)
	app.View("package-detail", screenFrame("Details", "test", nil, viewer, "Esc back"))
	registerScreenHelp(app, data)
	packages.SetIndex(1)
	vulnerabilities.SetIndex(2)
	for _, origin := range []string{"home", "results", "vulnerabilities", "dotfiles", "package-detail"} {
		t.Run(origin, func(t *testing.T) {
			app.Go(origin)
			app.RenderNow()
			if origin == "package-detail" {
				viewer.Layer().ScrollTo(15)
			}
			for _, back := range []riffkey.Key{{Special: riffkey.SpecialEscape}, {Rune: '?'}} {
				if !app.Input().Dispatch(question) || app.CurrentView() != "help" {
					t.Fatalf("? from %s: current view = %q, want help", origin, app.CurrentView())
				}
				app.RenderNow()
				size := app.Size()
				output := homeRenderOutput(app.Screen().FrontBuffer(), size.Height)
				for _, want := range []string{"Conficius / Help", "Profile: test", "Scope: project:demo", "/tmp/demo", "/tmp/config.toml", strings.Split(screenHelp[origin], "\n")[0]} {
					if !strings.Contains(output, want) {
						t.Fatalf("help from %s missing %q: %s", origin, want, output)
					}
				}
				if !app.Input().Dispatch(back) || app.CurrentView() != origin {
					t.Fatalf("back from help: current view = %q, want %s", app.CurrentView(), origin)
				}
				app.RenderNow()
				if packages.Index() != 1 || vulnerabilities.Index() != 2 || (origin == "package-detail" && viewer.Layer().ScrollY() != 15) {
					t.Fatal("help changed origin selection or scroll")
				}
			}
		})
	}
}

func TestScreenHelpGuardsAndCtrlC(t *testing.T) {
	previous := screenSize
	t.Cleanup(func() { updateScreenSize(previous.Width, previous.Height) })
	app := glyph.NewApp()
	data := &model{dotfiles: &dotfileState{pending: true}}
	for name := range screenHelp {
		app.View(name, glyph.Text(name))
	}
	registerScreenHelp(app, data)
	for name := range screenHelp {
		app.Go(name)
		updateScreenSize(80, 24)
		app.Input().Dispatch(riffkey.Key{Rune: '?'})
		if app.CurrentView() != name {
			t.Fatalf("pending %s allowed help", name)
		}
		data.dotfiles.quitPending = false
		if !app.Input().Dispatch(riffkey.ParsePattern("<C-c>")[0]) || !data.dotfiles.quitPending {
			t.Fatalf("Ctrl-C from %s did not defer pending quit", name)
		}
		data.dotfiles.pending = false
		updateScreenSize(39, 12)
		app.Input().Dispatch(riffkey.Key{Rune: '?'})
		if app.CurrentView() != name {
			t.Fatalf("undersized %s allowed help", name)
		}
		data.dotfiles.pending = true
	}
}

func TestPreserveListFocus(t *testing.T) {
	items := []string{"first", "selected", "last"}
	list := styleList(glyph.List(&items))
	list.SetIndex(1)
	preserve := preserveListFocus(list, &items, func(item *string) string { return *item })
	preserve(func() { items = []string{"selected", "first"} })
	if list.Index() != 0 || *list.Selected() != "selected" {
		t.Fatal("refresh lost selected identity")
	}
	preserve(func() { items = nil })
	if list.Index() != 0 || list.Selected() != nil {
		t.Fatal("empty refresh left invalid selection")
	}
}

func TestStyledTextViewPreservesScroll(t *testing.T) {
	for _, wordWrap := range []bool{false, true} {
		text := strings.Repeat("long detail line with words to reflow\n", 80)
		viewer := styledTextView(&text)
		if wordWrap {
			viewer.WordWrap()
		}
		template := glyph.Build(viewer)
		render := func(width int16) {
			template.Execute(glyph.NewBuffer(int(width), 8), width, 8)
		}
		render(40)
		viewer.Layer().ScrollTo(15)
		for _, width := range []int16{20, 60, 40} {
			render(width)
			if got := viewer.Layer().ScrollY(); got != 15 {
				t.Fatalf("word wrap %t, width %d: scroll = %d, want 15", wordWrap, width, got)
			}
		}
		viewer.Layer().ScrollTo(viewer.Layer().MaxScroll())
		render(80)
		if got := viewer.Layer().ScrollY(); got != viewer.Layer().MaxScroll() {
			t.Fatalf("reflow did not clamp scroll: %d", got)
		}
		text = strings.Repeat("new document\n", 80)
		render(20)
		if got := viewer.Layer().ScrollY(); got != 0 {
			t.Fatalf("new document retained scroll: %d", got)
		}
	}
}

func TestStyleListKeepsList(t *testing.T) {
	items := []string{"row"}
	list := glyph.List(&items)
	if styleList(list) != list {
		t.Fatal("styling replaced list")
	}
	assertRenderedContains(t, list, "› row")
}
