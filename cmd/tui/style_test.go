package main

import (
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/config"
	glyph "github.com/kungfusheep/glyph"
	"github.com/kungfusheep/riffkey"
)

func TestHelpTextViewStylesKeysAcrossResize(t *testing.T) {
	text := "results · Profile: test\n\n↑/↓ or j/k move\nEnter choose profile\nn / Esc cancel\nq / Ctrl-C quit\nEsc return without changing selection"
	viewer := helpTextView(&text)
	template := glyph.Build(glyph.VBox.Grow(1)(viewer))
	for _, width := range []int{80, 24, 40, 80} {
		buffer := glyph.NewBuffer(width, 8)
		template.Execute(buffer, int16(width), 8)
		content := viewer.Layer().Buffer()
		for _, row := range []struct {
			y   int
			key string
		}{{2, "↑/↓ or j/k"}, {3, "Enter"}, {4, "n / Esc"}, {5, "q / Ctrl-C"}, {6, "Esc"}} {
			for x := range glyph.StringWidth(row.key) {
				style := content.Get(x, row.y).Style
				if style.FG != glyph.Cyan || style.Attr != glyph.AttrBold || style.BG.Mode != glyph.ColorDefault {
					t.Fatalf("width %d key %q style: %+v", width, row.key, style)
				}
			}
			if style := content.Get(glyph.StringWidth(row.key)+1, row.y).Style; style.Attr != glyph.AttrDim || style.FG.Mode != glyph.ColorDefault {
				t.Fatalf("width %d description style: %+v", width, style)
			}
		}
		if content.Get(0, 0).Style.Attr&glyph.AttrDim != 0 {
			t.Fatal("context dimmed with help descriptions")
		}
	}
}

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
	for _, size := range [][2]int{{40, 12}, {80, 24}, {80, 15}, {80, 16}, {40, 12}} {
		width, height := size[0], size[1]
		updateScreenSize(width, height)
		buffer := glyph.NewBuffer(width, height)
		template.Execute(buffer, int16(width), int16(height))
		contentRow, statusRow := 3, height-3
		if height >= 16 {
			contentRow, statusRow = 4, height-4
		}
		for y := range height {
			for x := range width {
				cell := buffer.Get(x, y)
				if (y == 0 || y == height-1 || x == 0 || x == width-1) && cell.Rune != 0 && cell.Rune != ' ' {
					t.Fatalf("%dx%d edge not blank at %d,%d: %+v", width, height, x, y, cell)
				}
				if y == 1 && cell.Style.BG.Mode != glyph.ColorDefault {
					t.Fatalf("header overrides terminal background at column %d", x)
				}
				if height < 16 && (cell.Rune == '━' || cell.Rune == '─') {
					t.Fatalf("%dx%d separator visible at %d,%d", width, height, x, y)
				}
			}
		}
		if height >= 16 {
			for x := 1; x < width-1; x++ {
				if cell := buffer.Get(x, 3); cell.Rune != '━' || cell.Style.FG != glyph.Magenta {
					t.Fatalf("header rule missing at column %d: %+v", x, cell)
				}
				if cell := buffer.Get(x, height-3); cell.Rune != '─' || cell.Style.Attr&glyph.AttrDim == 0 {
					t.Fatalf("footer rule missing at column %d: %+v", x, cell)
				}
			}
		}
		if buffer.Get(1, 1).Style.FG.Mode != glyph.ColorDefault {
			t.Fatal("header must use terminal foreground")
		}
		if !strings.HasPrefix(buffer.GetLine(1), " Conficius / Links") || !strings.HasPrefix(buffer.GetLine(contentRow), " content") {
			t.Fatal("header or content misplaced")
		}
		if !strings.Contains(buffer.GetLine(height-2), "Space mark · u preview · ? help") {
			t.Fatalf("footer not pinned: %q", buffer.GetLine(height-2))
		}
		if !strings.Contains(buffer.GetLine(2), "dotfiles.toml") {
			t.Fatalf("context lost trailing path: %q", buffer.GetLine(2))
		}
		if !strings.HasPrefix(buffer.GetLine(statusRow), " "+status) || buffer.Get(1, statusRow).Style.Attr&glyph.AttrDim != 0 {
			t.Fatal("error status misplaced or dimmed")
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
			if !strings.HasPrefix(buffer.GetLine(2), " content") || !strings.Contains(buffer.GetLine(9), "Ready") || strings.Contains(homeRenderOutput(buffer, 12), "─") || glyph.StringWidth(line) > width-2 {
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
			for row := range 12 {
				if row == 10 {
					continue
				}
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

func TestDetailTextViewHierarchy(t *testing.T) {
	for _, test := range []struct {
		name, text, heading, label, value, warning, failure, path string
		title                                                     bool
	}{
		{"package", "tool\n\nCurrent: 1.0   Latest: 2.0\n\nPath: /project/tool\n\nError\nupdate failed", "tool", "Current:", "1.0", "", "update failed", "/project/tool", true},
		{"evidence", "Package\nnode\n\nSource\nEUVD\n\nStatus\nvulnerable\n\nCVSS\n8.1\n\nDescription\nplain prose\n\nPath\n/project/node\n\nCVE/advisory\nCVE-2026-1", "Package", "", "plain prose", "8.1", "vulnerable", "/project/node", false},
		{"link", "Tool: shell\n\nSource: /repo/shell\n\nState: Missing\n\nTarget: /home/shell", "Tool:", "Source:", "shell", "Missing", "", "/repo/shell", false},
		{"environment", "Mode: xdg\n\nConfig root: /home/config\n\nState: warning\n\nFinding: config missing", "", "Mode:", "xdg", "warning", "", "/home/config", false},
		{"failure", "Failure\nlink failed\n\nAction: replace\n\nPath: /home/shell\n\nRollback incomplete: permission denied\n\nAffected paths:\n/home/other", "Failure", "Action:", "replace", "", "link failed", "/home/other", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			viewer := detailTextView(&test.text, test.title)
			buffer := glyph.NewBuffer(80, 80)
			glyph.Build(glyph.VBox.Grow(1)(viewer)).Execute(buffer, 80, 80)
			style := func(text string) glyph.Style {
				t.Helper()
				for y := range buffer.Height() {
					if x := strings.Index(buffer.GetLine(y), text); x >= 0 {
						return buffer.Get(x, y).Style
					}
				}
				t.Fatalf("missing %q", text)
				return glyph.Style{}
			}
			if test.heading != "" {
				if got := style(test.heading); !got.FG.Equal(glyph.Cyan) || got.Attr&glyph.AttrBold == 0 {
					t.Fatalf("heading style = %#v", got)
				}
			}
			if test.label != "" && style(test.label).Attr&glyph.AttrDim == 0 {
				t.Fatal("label not dim")
			}
			if got := style(test.value); got.Attr != 0 || !got.FG.Equal(glyph.Color{}) {
				t.Fatalf("value style = %#v", got)
			}
			if test.warning != "" && !style(test.warning).FG.Equal(glyph.Yellow) {
				t.Fatal("warning not yellow")
			}
			if test.failure != "" && !style(test.failure).FG.Equal(glyph.Red) {
				t.Fatal("failure not red")
			}
			if style(test.path).Attr&glyph.AttrDim == 0 {
				t.Fatal("path not dim")
			}
			for y := range buffer.Height() {
				for x := range buffer.Width() {
					cell := buffer.Get(x, y)
					if cell.Style.FG.Mode == glyph.ColorRGB || !cell.Style.BG.Equal(glyph.Color{}) {
						t.Fatal("non-native palette")
					}
				}
			}
		})
	}
}

func TestDetailTextViewResizeAndNavigation(t *testing.T) {
	text := "Package\nnode\n\nPath\n/" + strings.Repeat("long/path/", 100) + "tail\n\nSource ID\nEUVD-1"
	viewer := detailTextView(&text, false)
	app := glyph.NewApp()
	content := glyph.VBox.Grow(1)(viewer)
	app.View("detail", content)
	app.Go("detail")
	template := glyph.Build(content)
	render := func(width int16) {
		template.Execute(glyph.NewBuffer(int(width), 8), width, 8)
	}
	render(40)
	for _, key := range []string{"j", "<down>", "<C-d>"} {
		before := viewer.Layer().ScrollY()
		app.Input().Dispatch(riffkey.ParsePattern(key)[0])
		render(40)
		if viewer.Layer().ScrollY() <= before {
			t.Fatalf("%s did not scroll", key)
		}
	}
	for _, width := range []int16{20, 60, 40} {
		before := viewer.Layer().ScrollY()
		render(width)
		if viewer.Layer().ScrollY() != before {
			t.Fatalf("width %d reset scroll", width)
		}
		buffer := viewer.Layer().Buffer()
		var path strings.Builder
		for y := 4; y < buffer.Height()-3; y++ {
			path.WriteString(strings.TrimRight(buffer.GetLine(y), " "))
			if buffer.Get(0, y).Style.Attr&glyph.AttrDim == 0 {
				t.Fatalf("width %d path row %d not dim", width, y)
			}
		}
		if !strings.Contains(path.String(), strings.Repeat("long/path/", 100)+"tail") {
			t.Fatalf("width %d lost path", width)
		}
	}
	for _, key := range []string{"k", "<up>", "<C-u>"} {
		before := viewer.Layer().ScrollY()
		app.Input().Dispatch(riffkey.ParsePattern(key)[0])
		render(40)
		if viewer.Layer().ScrollY() >= before {
			t.Fatalf("%s did not scroll", key)
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
