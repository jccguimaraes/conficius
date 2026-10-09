package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jccguimaraes/conficius/internal/audit"
	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/dotfiles"
	"github.com/jccguimaraes/conficius/internal/layout"
	"github.com/kungfusheep/glyph"
)

func dotfileTestState(t *testing.T) *dotfileState {
	t.Helper()
	root, home := t.TempDir(), t.TempDir()
	for _, name := range []string{"source", "old"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	state := newDotfileState(&config.Project{Root: root}, nil, "config.toml")
	state.options.HomeDir = home
	return state
}

func dotfileTestEntry(state *dotfileState, name string) dotfiles.Entry {
	return dotfiles.Entry{Name: name, Source: filepath.Join(state.options.ConfigDir, "source"), Target: filepath.Join(state.options.HomeDir, name)}
}

func TestDotfileClassification(t *testing.T) {
	state := dotfileTestState(t)
	for _, tc := range []struct {
		name, destination, label string
		regular                  bool
		group                    int
	}{
		{name: "missing", label: "Missing", group: 0},
		{name: "safe", destination: filepath.Join(state.options.ConfigDir, "old"), label: "Wrong link · safe", group: 0},
		{name: "unsafe", destination: filepath.Join(t.TempDir(), "outside"), label: "Unsafe link · blocked", group: 1},
		{name: "regular", regular: true, label: "Regular conflict · blocked", group: 1},
		{name: "healthy", destination: filepath.Join(state.options.ConfigDir, "source"), label: "Healthy", group: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := dotfileTestEntry(state, tc.name)
			if tc.regular {
				if err := os.WriteFile(entry.Target, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if tc.destination != "" {
				if err := os.Symlink(tc.destination, entry.Target); err != nil {
					t.Fatal(err)
				}
			}
			row := state.inspect(entry)
			if row.group != tc.group || row.label != tc.label {
				t.Fatalf("row = %+v", row)
			}
			state.toggle(&row)
			if state.marks[dotfileKey(entry)] != (tc.group == 0) {
				t.Fatal("non-actionable mark accepted or actionable mark rejected")
			}
		})
	}
	entry := dotfileTestEntry(state, "excluded")
	entry.Platforms = []string{"other-platform"}
	if row := state.inspect(entry); row.group != 3 {
		t.Fatalf("excluded = %+v", row)
	}
}

func TestDotfileFiltersSortAndMarks(t *testing.T) {
	state := dotfileTestState(t)
	for _, tc := range []struct {
		name  string
		group int
	}{{"healthy", 2}, {"blocked", 1}, {"z", 0}, {"excluded", 3}, {"a", 0}} {
		row := state.inspect(dotfileTestEntry(state, tc.name))
		row.group = tc.group
		state.rows = append(state.rows, row)
	}
	state.selection = 99
	state.rebuild()
	for i, name := range []string{"a", "z", "blocked", "healthy", "excluded"} {
		if state.visible[i].entry.Name != name {
			t.Fatalf("sorted = %+v", state.visible)
		}
	}
	if state.selection != 4 {
		t.Fatal("selection not clamped")
	}
	state.filter = 1
	state.rebuild()
	state.toggleAll()
	if len(state.marks) != 2 {
		t.Fatal("visible actionable not marked")
	}
	state.filter = 2
	state.rebuild()
	state.toggleAll()
	if len(state.marks) != 2 || len(state.visible) != 1 {
		t.Fatal("filter lost marks")
	}
	state.filter = 1
	state.rebuild()
	state.toggleAll()
	if len(state.marks) != 0 {
		t.Fatal("toggle all did not clear")
	}
	state.filter = 4
	state.rebuild()
	if len(state.visible) != 1 || state.visible[0].group != 3 {
		t.Fatal("nonapplicable filter")
	}
	state.rows = nil
	state.rebuild()
	if state.selection != 0 {
		t.Fatal("empty selection")
	}
}

func TestDotfilePreviewAndApply(t *testing.T) {
	state := dotfileTestState(t)
	for _, name := range []string{"create", "replace"} {
		entry := dotfileTestEntry(state, name)
		if name == "replace" {
			if err := os.Symlink(filepath.Join(state.options.ConfigDir, "old"), entry.Target); err != nil {
				t.Fatal(err)
			}
		}
		state.rows = append(state.rows, state.inspect(entry))
		state.marks[dotfileKey(entry)] = true
	}
	state.filter = 2
	state.rebuild()
	if err := state.preparePreview(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.previewLines[0], "2 hidden") {
		t.Fatal(state.previewLines)
	}
	for _, entry := range state.previewEntries {
		if entry.Name == "create" {
			if _, err := os.Lstat(entry.Target); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("preview mutated filesystem")
			}
		}
	}
	created, replaced, err := state.applyPreview()
	if err != nil || created != 1 || replaced != 1 {
		t.Fatalf("apply = %d %d %v", created, replaced, err)
	}
	for _, entry := range state.previewEntries {
		if row := state.inspect(entry); row.group != 2 {
			t.Fatalf("not healthy: %+v", row)
		}
	}
}

func TestDotfileStalePreviewAbortsAll(t *testing.T) {
	for _, change := range []string{"source", "target", "destination", "parent"} {
		t.Run(change, func(t *testing.T) {
			state := dotfileTestState(t)
			first, second := dotfileTestEntry(state, "a"), dotfileTestEntry(state, "z")
			if change == "destination" {
				if err := os.Symlink(filepath.Join(state.options.ConfigDir, "old"), second.Target); err != nil {
					t.Fatal(err)
				}
			}
			if change == "parent" {
				second.Target = filepath.Join(state.options.HomeDir, "nested", "z")
			}
			for _, entry := range []dotfiles.Entry{first, second} {
				state.rows = append(state.rows, state.inspect(entry))
				state.marks[dotfileKey(entry)] = true
			}
			if err := state.preparePreview(); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "source":
				if err := os.Rename(first.Source, first.Source+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(first.Source, []byte("source"), 0600); err != nil {
					t.Fatal(err)
				}
			case "target":
				if err := os.WriteFile(second.Target, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "destination":
				if err := os.Remove(second.Target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(state.options.ConfigDir, "missing"), second.Target); err != nil {
					t.Fatal(err)
				}
			case "parent":
				if err := os.Symlink(t.TempDir(), filepath.Dir(second.Target)); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := state.applyPreview(); err == nil {
				t.Fatal("stale preview accepted")
			}
			if _, err := os.Lstat(first.Target); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("partial apply on stale preview")
			}
		})
	}
}

func TestDotfileReadonlyRefreshAndRegistration(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	state := newDotfileState(nil, config.ErrNotFound, "missing.toml")
	state.filter = 3
	state.marks["old"] = true
	state.refresh(nil)
	if len(state.marks) != 0 || state.filter != 3 || !strings.Contains(state.status, "conficius init") {
		t.Fatalf("refresh = %+v", state)
	}
	if len(state.reports) != 2 || len(state.reports[layout.ModeXDG].Findings) < 2 {
		t.Fatal("environment modes or zsh missing")
	}
	if err := state.preparePreview(); err == nil || !strings.Contains(err.Error(), "no links marked") {
		t.Fatal(err)
	}
	state.configPath = filepath.Join(t.TempDir(), "conficius.toml")
	if err := os.WriteFile(state.configPath, []byte("version = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state.refresh(nil)
	if !strings.Contains(state.status, "[tools.zsh]") {
		t.Fatal("empty mapping example missing")
	}
}

func TestDotfileFailureDetails(t *testing.T) {
	err := &dotfiles.ApplyError{Action: "symlink", Path: "target", Cause: errors.New("failed"), RollbackAttempted: true, RollbackError: errors.New("remove failed"), AffectedPaths: []string{"target", "parent"}}
	text := dotfileFailureDetails(errors.Join(errors.New("transaction"), err))
	for _, expected := range []string{"Action: symlink", "Path: target", "Rollback attempted: true", "Rollback incomplete", "Filesystem may be partially changed.", "target\nparent"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in %s", expected, text)
		}
	}
}

func TestDotfileLinksRenderer(t *testing.T) {
	state := dotfileTestState(t)
	for _, name := range []string{"alpha", "beta", "excluded"} {
		entry := dotfileTestEntry(state, name)
		if name == "excluded" {
			entry.Platforms = []string{"other-platform"}
		}
		state.rows = append(state.rows, state.inspect(entry))
	}
	state.rebuild()
	list := dotfileLinksList(state)
	for _, want := range []string{"alpha", "beta", "Missing", "excluded", "Not applicable"} {
		assertRenderedContains(t, list, want)
	}
	template := glyph.Build(list)
	state.toggle(&state.visible[0])
	buffer := glyph.NewBuffer(160, 20)
	template.Execute(buffer, 160, 20)
	if !strings.Contains(buffer.GetLine(0), "[x]") {
		t.Fatal("compiled renderer lost live mark")
	}
	for y, dim := range []bool{false, false, true} {
		line := buffer.GetLine(y)
		x := strings.Index(line, state.visible[y].entry.Name)
		if x < 0 || (buffer.Get(x, y).Style.Attr&glyph.AttrDim != 0) != dim {
			t.Fatalf("row %d dim=%t: %q", y, dim, line)
		}
	}
	state.selection = 1
	state.filter = 1
	state.rebuild()
	buffer = glyph.NewBuffer(40, 8)
	template.Execute(buffer, 40, 8)
	if state.selection != 1 || !state.marks[dotfileKey(state.visible[0].entry)] || !strings.Contains(buffer.GetLine(1), "beta") || !strings.Contains(buffer.GetLine(1), "Missing") {
		t.Fatalf("compact render lost state: %q", buffer.GetLine(1))
	}
}

func TestDotfileEnvironmentRenderer(t *testing.T) {
	state := &dotfileState{mode: layout.ModeXDG, reports: map[layout.Mode]audit.Report{
		layout.ModeXDG: {Findings: []audit.Finding{
			{Severity: audit.SeverityOK, Message: "first finding"},
			{Severity: audit.SeverityWarning, Message: "second finding"},
			{Severity: audit.SeverityError, Message: "third finding"},
		}},
	}}
	state.environmentSummary()
	if !strings.Contains(state.environment, "1 warnings · 1 errors") {
		t.Fatal(state.environment)
	}
	list := dotfileEnvironmentList(state)
	for _, want := range []string{"ok first finding", "warning second finding", "error third finding"} {
		assertRenderedContains(t, list, want)
	}
	template := glyph.Build(list)
	state.findings = []audit.Finding{{Severity: audit.SeverityError, Message: "changed finding"}}
	buffer := glyph.NewBuffer(160, 20)
	template.Execute(buffer, 160, 20)
	if !strings.Contains(buffer.GetLine(0), "error changed finding") {
		t.Fatal(buffer.GetLine(0))
	}
}

func TestDotfileRefreshReloadsConfig(t *testing.T) {
	state := dotfileTestState(t)
	t.Setenv("HOME", state.options.HomeDir)
	state.configPath = filepath.Join(state.options.ConfigDir, "conficius.toml")
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(state.configPath, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	state.refresh(nil)
	if state.configErr == nil || state.project != nil {
		t.Fatal("missing config not recorded")
	}
	write("version = 1\n[tools.zsh]\nlinks = [{ source = \"source\", target = \"$HOME/.zshrc\" }]\n")
	state.refresh(nil)
	if state.configErr != nil || state.project == nil || len(state.rows) != 1 || state.visible[0].basename != ".zshrc" {
		t.Fatalf("recovery = %+v", state)
	}
	state.marks[dotfileKey(state.rows[0].entry)] = true
	write("version = 1\n")
	state.refresh(nil)
	if len(state.rows) != 0 || len(state.marks) != 0 || state.configErr != nil || !strings.Contains(state.status, "$HOME/.zshrc") {
		t.Fatalf("reload = %+v", state)
	}
	write("invalid = true\n")
	state.refresh(nil)
	if state.project != nil || state.configErr == nil || state.options.ConfigDir != "" {
		t.Fatal("invalid config retained old project")
	}
}

func TestDotfileRefreshDiscoversConfig(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	state := newDotfileState(nil, config.ErrNotFound, "")
	state.refresh(nil)
	if !errors.Is(state.configErr, config.ErrNotFound) {
		t.Fatal(state.configErr)
	}
	path := filepath.Join(root, config.FileName)
	if err := os.WriteFile(path, []byte("version = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state.refresh(nil)
	if state.project == nil || state.configErr != nil || state.configPath != path {
		t.Fatalf("discovery = %+v", state)
	}
}

func TestDotfileRebuildKeepsIdentity(t *testing.T) {
	state := dotfileTestState(t)
	for _, name := range []string{"b", "c"} {
		state.rows = append(state.rows, state.inspect(dotfileTestEntry(state, name)))
	}
	state.rebuild()
	state.selection = 1
	state.rows = append(state.rows, state.inspect(dotfileTestEntry(state, "a")))
	state.rebuild()
	if state.selection != 2 || state.visible[state.selection].entry.Name != "c" {
		t.Fatal("selection identity lost after reorder")
	}
}

func TestDotfileAsyncRefreshGeneration(t *testing.T) {
	state := dotfileTestState(t)
	state.configPath = filepath.Join(state.options.ConfigDir, "conficius.toml")
	if err := os.WriteFile(state.configPath, []byte("version = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app := glyph.NewApp()
	registerDotfiles(state, app)
	if !state.refreshing || state.project == nil || state.generation != 1 {
		t.Fatal("registration did not schedule refresh")
	}
	state.configPath = filepath.Join(t.TempDir(), "missing.toml")
	state.refresh(app)
	state.mode = layout.ModeOSNative
	state.marks["old"] = true
	for deadline := time.Now().Add(3 * time.Second); state.refreshing && time.Now().Before(deadline); {
		app.RenderNow()
		time.Sleep(time.Millisecond)
	}
	if state.refreshing || state.generation != 2 || state.configErr == nil || state.project != nil || state.mode != layout.ModeOSNative || len(state.marks) != 0 {
		t.Fatalf("async result = %+v", state)
	}
	// A pending transaction rejects refresh before incrementing generation.
	state.pending = true
	state.refresh(app)
	if state.generation != 2 {
		t.Fatal("refresh raced pending transaction")
	}
	// Drain older completion too: it must not resurrect valid configuration.
	time.Sleep(20 * time.Millisecond)
	app.RenderNow()
	if state.project != nil || state.configErr == nil {
		t.Fatal("old refresh overwrote latest generation")
	}
}

func TestDotfileWorkerSnapshotIsolation(t *testing.T) {
	state := dotfileTestState(t)
	state.rows = []dotfileRow{state.inspect(dotfileTestEntry(state, "create"))}
	state.marks[dotfileKey(state.rows[0].entry)] = true
	if err := state.preparePreview(); err != nil {
		t.Fatal(err)
	}
	work := state.workerSnapshot()
	done := make(chan error, 1)
	go func() {
		_, _, err := work.applyPreview()
		work.load()
		done <- err
	}()
	for range 100 {
		state.mode = layout.ModeOSNative
		state.marks = map[string]bool{}
		state.previewEntries, state.previewStamps = nil, nil
		state.rebuild()
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	state.publish(work)
	if state.mode != layout.ModeOSNative || len(state.marks) != 0 {
		t.Fatal("publication overwrote live mode or marks")
	}
}
