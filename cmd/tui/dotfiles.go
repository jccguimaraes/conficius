package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/jccguimaraes/conficius/internal/audit"
	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/dotfiles"
	"github.com/jccguimaraes/conficius/internal/layout"
	"github.com/jccguimaraes/conficius/internal/shell"
	"github.com/kungfusheep/glyph"
)

var dotfileFilters = []string{"All", "Actionable", "Blocked", "Healthy", "Not applicable"}

type dotfileRow struct {
	entry                                  dotfiles.Entry
	result                                 dotfiles.Result
	group                                  int
	label, reason, current, mark, basename string
	statusLabel                            string
	statusStyle                            glyph.Style
	nameStyle                              glyph.Style
	style                                  glyph.Style
}

type dotfileStamp struct {
	result                              dotfiles.Result
	source                              os.FileInfo
	resolvedSource, destination, parent string
}

type dotfileMenuItem struct {
	name      string
	summary   string
	nameColor glyph.Color
}

type dotfileState struct {
	project                                 *config.Project
	configErr                               error
	configPath                              string
	options                                 dotfiles.Options
	rows, visible                           []dotfileRow
	marks                                   map[string]bool
	filter, selection                       int
	mode                                    layout.Mode
	reports                                 map[layout.Mode]audit.Report
	environment, links, status, filterLabel string
	detailText, previewSummary              string
	menu                                    []dotfileMenuItem
	menuSelection                           int
	detail, previewLines                    []string
	findings                                []audit.Finding
	previewEntries                          []dotfiles.Entry
	previewStamps                           []dotfileStamp
	pending, quitPending                    bool
	generation                              uint64
	refreshing                              bool
}

func newDotfileState(project *config.Project, configErr error, configPath string) *dotfileState {
	if project == nil && configPath == "" && errors.Is(configErr, config.ErrNotFound) {
		return nil
	}
	if configPath == "" && project != nil {
		configPath = project.Path
	}
	home, err := os.UserHomeDir()
	if err != nil && configErr == nil {
		configErr = err
	}
	state := &dotfileState{project: project, configErr: configErr, configPath: configPath, marks: map[string]bool{}, mode: layout.ModeXDG, options: dotfiles.Options{HomeDir: home, Platform: runtime.GOOS, Force: true}}
	if project != nil {
		state.options.ConfigDir = project.Root
	}
	return state
}

func dotfileKey(entry dotfiles.Entry) string {
	return entry.Name + "\x00" + entry.Target + "\x00" + entry.Source
}

func (state *dotfileState) inspect(entry dotfiles.Entry) dotfileRow {
	row := dotfileRow{entry: entry, group: 1, label: "Blocked", reason: "Inspect configuration and retry."}
	row.result = dotfiles.Result{Name: entry.Name, Source: entry.Source, Target: entry.Target}
	if !filepath.IsAbs(row.result.Source) {
		row.result.Source = filepath.Join(state.options.ConfigDir, entry.Source)
	}
	if !filepath.IsAbs(row.result.Target) {
		row.result.Target = filepath.Join(state.options.HomeDir, entry.Target)
	}
	if len(entry.Platforms) > 0 && !slices.Contains(entry.Platforms, state.options.Platform) {
		row.group, row.label, row.reason = 3, "Not applicable", "Mapping excludes current platform."
		return row
	}
	results, err := dotfiles.Inspect([]dotfiles.Entry{entry}, state.options)
	if err != nil {
		row.reason = err.Error()
		return row
	}
	row.result = results[0]
	row.current, _ = os.Readlink(row.result.Target)
	switch row.result.State {
	case dotfiles.StateHealthy:
		row.group, row.label, row.reason = 2, "Healthy", "No action needed."
	case dotfiles.StateConflict:
		row.label, row.reason = "Regular conflict · blocked", "Move or back up existing file manually; regular files are never replaced."
	default:
		_, err = dotfiles.Link([]dotfiles.Entry{entry}, state.options)
		if err != nil {
			row.label, row.reason = "Unsafe link · blocked", err.Error()
			return row
		}
		row.group = 0
		if row.result.State == dotfiles.StateMissing {
			row.label, row.reason = "Missing", "Create link."
		} else {
			row.label, row.reason = "Wrong link · safe", "Replace repository symlink."
		}
	}
	return row
}

func (state *dotfileState) refresh(app *glyph.App) {
	if state.pending {
		return
	}
	state.generation++
	generation := state.generation
	work := state.workerSnapshot()
	if app == nil {
		work.load()
		state.publish(work)
		return
	}
	state.refreshing = true
	state.marks = map[string]bool{}
	state.rebuild()
	state.status = "Refreshing · stale rows"
	go func() {
		work.load()
		app.Apply(func() {
			if state.pending || state.generation != generation {
				return
			}
			state.publish(work)
		})
	}()
}

// workerSnapshot copies only immutable inputs; workers never read UI-bound state.
func (state *dotfileState) workerSnapshot() *dotfileState {
	options := state.options
	options.Names = slices.Clone(options.Names)
	options.Exclude = slices.Clone(options.Exclude)
	options.Preview = slices.Clone(options.Preview)
	return &dotfileState{configPath: state.configPath, options: options, mode: state.mode,
		marks: map[string]bool{}, previewEntries: slices.Clone(state.previewEntries), previewStamps: slices.Clone(state.previewStamps)}
}

func (state *dotfileState) publish(work *dotfileState) {
	state.project, state.configErr, state.configPath, state.options = work.project, work.configErr, work.configPath, work.options
	state.rows, state.reports, state.findings = work.rows, work.reports, nil
	state.environment, state.links, state.status = work.environment, work.links, work.status
	state.marks = map[string]bool{}
	state.previewEntries, state.previewStamps, state.previewLines = nil, nil, nil
	state.refreshing = false
	state.environmentSummary()
	state.rebuild()
}

func (state *dotfileState) load() {
	path, err := config.Discover(state.configPath)
	state.project, state.configErr = nil, err
	state.options.ConfigDir = ""
	if err == nil {
		state.configPath = path
		state.project, state.configErr = config.Load(path)
		if state.project != nil {
			state.options.ConfigDir = state.project.Root
		}
	}
	state.marks = map[string]bool{}
	state.previewEntries, state.previewStamps = nil, nil
	state.rows = nil
	state.reports = map[layout.Mode]audit.Report{}
	state.findings = nil
	snapshot, err := config.Current()
	if err != nil {
		state.environment = "Environment: " + err.Error()
	} else {
		for _, mode := range []layout.Mode{layout.ModeXDG, layout.ModeOSNative} {
			report := audit.Environment(snapshot, layout.Resolve(snapshot, mode))
			if snapshot.Shell.Name == shell.SHELL_ZSH {
				report.Findings = append(report.Findings, audit.Zsh(shell.ReadZsh(), report.Expected)...)
			}
			state.reports[mode] = report
		}
		state.environmentSummary()
	}
	state.status = ""
	if state.configErr != nil || state.project == nil {
		state.links = "Links: read-only; configuration unavailable"
		state.status = "Run conficius init to create conficius.toml."
		if state.configErr != nil {
			state.status = state.configErr.Error() + "\n" + state.status
		}
	} else {
		targets := map[string]int{}
		for name, tool := range state.project.Tools {
			for _, link := range tool.Links {
				row := state.inspect(dotfiles.Entry{Name: name, Source: link.Source, Target: link.Target, Platforms: link.Platforms})
				state.rows = append(state.rows, row)
				if row.group != 3 {
					targets[row.result.Target]++
				}
			}
		}
		for i := range state.rows {
			row := &state.rows[i]
			if row.group != 3 && targets[row.result.Target] > 1 {
				row.group, row.label, row.reason = 1, "Duplicate target · blocked", "Configure only one mapping per target."
			}
		}
		counts := [4]int{}
		for _, row := range state.rows {
			counts[row.group]++
		}
		state.links = fmt.Sprintf("Links: %d actionable · %d blocked · %d healthy · %d not applicable", counts[0], counts[1], counts[2], counts[3])
		if len(state.rows) == 0 {
			state.status = "No configured links. Example:\n[tools.zsh]\nlinks = [{ source = \"zsh/.zshrc\", target = \"$HOME/.zshrc\" }]"
		}
	}
	state.rebuild()
}

func (state *dotfileState) environmentSummary() {
	report, ok := state.reports[state.mode]
	if !ok {
		return
	}
	warnings, failures := 0, 0
	for _, finding := range report.Findings {
		switch finding.Severity {
		case audit.SeverityWarning:
			warnings++
		case audit.SeverityError:
			failures++
		}
	}
	state.findings = report.Findings
	state.environment = fmt.Sprintf("Environment: %s · %d findings · %d warnings · %d errors", state.mode, len(report.Findings), warnings, failures)
	state.updateMenu()
}

func (state *dotfileState) rebuild() {
	selected := ""
	if state.selection >= 0 && state.selection < len(state.visible) {
		selected = dotfileKey(state.visible[state.selection].entry)
	}
	sort.SliceStable(state.rows, func(i, j int) bool {
		a, b := state.rows[i], state.rows[j]
		if a.group != b.group {
			return a.group < b.group
		}
		if a.entry.Name != b.entry.Name {
			return a.entry.Name < b.entry.Name
		}
		if a.result.Target != b.result.Target {
			return a.result.Target < b.result.Target
		}
		return a.result.Source < b.result.Source
	})
	state.visible = nil
	for _, row := range state.rows {
		if state.filter != 0 && row.group != state.filter-1 {
			continue
		}
		row.basename = filepath.Base(row.result.Target)
		row.style = glyph.Style{}
		row.statusLabel = row.label
		row.statusStyle = glyph.Style{}
		switch row.group {
		case 0:
			row.statusStyle.FG = glyph.Yellow
		case 1:
			row.statusLabel = "Blocked"
			row.statusStyle.FG = glyph.Red
		case 2:
			row.statusStyle.FG = glyph.Green
		case 3:
			row.statusStyle = row.statusStyle.Dim()
		}
		if row.group == 3 {
			row.style = row.style.Dim()
		}
		row.mark = "[ ]"
		if state.marks[dotfileKey(row.entry)] {
			row.mark = "[x]"
		}
		if selected != "" && dotfileKey(row.entry) == selected {
			state.selection = len(state.visible)
		}
		state.visible = append(state.visible, row)
	}
	state.selection = min(max(0, state.selection), max(0, len(state.visible)-1))
	for i := range state.visible {
		state.visible[i].nameStyle = state.visible[i].style
		state.visible[i].nameStyle.FG = glyph.DefaultColor()
		if i == state.selection {
			state.visible[i].nameStyle.FG = glyph.Cyan
		}
	}
	state.filterLabel = "Filter: " + dotfileFilters[state.filter]
	state.updateMenu()
}

func (state *dotfileState) toggle(row *dotfileRow) {
	if state.pending || state.refreshing || row == nil {
		return
	}
	if row.group != 0 {
		state.status = row.reason
		return
	}
	key := dotfileKey(row.entry)
	if state.marks[key] {
		delete(state.marks, key)
	} else {
		state.marks[key] = true
	}
	state.rebuild()
}

func (state *dotfileState) toggleAll() {
	if state.pending || state.refreshing {
		return
	}
	all := true
	for _, row := range state.visible {
		if row.group == 0 && !state.marks[dotfileKey(row.entry)] {
			all = false
		}
	}
	for _, row := range state.visible {
		if row.group != 0 {
			continue
		}
		if all {
			delete(state.marks, dotfileKey(row.entry))
		} else {
			state.marks[dotfileKey(row.entry)] = true
		}
	}
	state.rebuild()
}

func (state *dotfileState) linkDetails(row dotfileRow) {
	platforms := strings.Join(row.entry.Platforms, ", ")
	if platforms == "" {
		platforms = "all"
	}
	current := row.current
	if current == "" {
		current = "not a symlink"
	}
	state.detail = []string{"Tool: " + row.entry.Name, "Source: " + row.result.Source, "Target: " + row.result.Target, "Platforms: " + platforms + " (current: " + state.options.Platform + ")", "Current: " + current, "State: " + row.label, "Action: " + row.result.Action, "Recommendation: " + row.reason}
}

func dotfileResolvedParent(path string) (string, error) {
	tail := []string{}
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) || filepath.Dir(path) == path {
			return "", err
		}
		tail = append(tail, filepath.Base(path))
		path = filepath.Dir(path)
	}
}

func dotfileSnapshot(result dotfiles.Result) (dotfileStamp, error) {
	stamp := dotfileStamp{result: result}
	var err error
	if stamp.source, err = os.Stat(result.Source); err != nil {
		return stamp, err
	}
	if stamp.resolvedSource, err = filepath.EvalSymlinks(result.Source); err != nil {
		return stamp, err
	}
	stamp.destination, err = os.Readlink(result.Target)
	if err != nil && result.State != dotfiles.StateMissing {
		return stamp, err
	}
	stamp.parent, err = dotfileResolvedParent(filepath.Dir(result.Target))
	return stamp, err
}

func sameDotfileSnapshot(a, b dotfileStamp) bool {
	left, right := a.result, b.result
	left.TargetInfo, right.TargetInfo = nil, nil
	sameTarget := a.result.TargetInfo == nil && b.result.TargetInfo == nil || a.result.TargetInfo != nil && b.result.TargetInfo != nil && os.SameFile(a.result.TargetInfo, b.result.TargetInfo)
	return left == right && sameTarget && a.resolvedSource == b.resolvedSource && a.destination == b.destination && a.parent == b.parent && os.SameFile(a.source, b.source) && a.source.Size() == b.source.Size() && a.source.ModTime().Equal(b.source.ModTime()) && a.source.Mode() == b.source.Mode()
}

func (state *dotfileState) preparePreview() error {
	state.previewEntries, state.previewStamps, state.previewLines = nil, nil, nil
	hidden := 0
	for _, row := range state.rows {
		if !state.marks[dotfileKey(row.entry)] {
			continue
		}
		if row.group != 0 {
			return errors.New("marked mapping is no longer actionable; refresh and retry")
		}
		state.previewEntries = append(state.previewEntries, row.entry)
		if state.filter != 0 && row.group != state.filter-1 {
			hidden++
		}
	}
	if len(state.previewEntries) == 0 {
		return errors.New("no links marked; Space marks actionable links")
	}
	results, err := dotfiles.Link(state.previewEntries, state.options)
	if err != nil {
		return err
	}
	state.previewSummary = fmt.Sprintf("Apply %d links (%d hidden)", len(results), hidden)
	state.previewLines = []string{fmt.Sprintf("%d marked links (%d hidden by filter)", len(results), hidden)}
	state.previewLines = append(state.previewLines, "Regular files always unchanged.")
	for _, result := range results {
		if result.State == dotfiles.StateWrongLink {
			state.previewLines = append(state.previewLines, "Replace only symlinks resolving inside repository; outside links stay unchanged.")
			break
		}
	}
	for _, result := range results {
		if result.State != dotfiles.StateMissing && result.State != dotfiles.StateWrongLink {
			return errors.New("link state changed; refresh and mark again")
		}
		stamp, err := dotfileSnapshot(result)
		if err != nil {
			return err
		}
		state.previewStamps = append(state.previewStamps, stamp)
		state.previewLines = append(state.previewLines, fmt.Sprintf("%s %s: %s => %s", result.Action, result.Name, result.Source, result.Target))
	}
	return nil
}

func (state *dotfileState) applyPreview() (int, int, error) {
	if len(state.previewStamps) == 0 {
		return 0, 0, errors.New("no preview to apply")
	}
	options := state.options
	options.Apply = false
	results, err := dotfiles.Link(state.previewEntries, options)
	if err != nil {
		return 0, 0, err
	}
	if len(results) != len(state.previewStamps) {
		return 0, 0, errors.New("preview changed; nothing applied; refresh and retry")
	}
	for i, result := range results {
		stamp, err := dotfileSnapshot(result)
		if err != nil {
			return 0, 0, err
		}
		if !sameDotfileSnapshot(stamp, state.previewStamps[i]) {
			return 0, 0, errors.New("preview changed; nothing applied; refresh and retry")
		}
	}
	options.Apply = true
	options.Preview = results
	results, err = dotfiles.Link(state.previewEntries, options)
	if err != nil {
		return 0, 0, err
	}
	created, replaced := 0, 0
	for _, result := range results {
		if result.State == dotfiles.StateMissing {
			created++
		}
		if result.State == dotfiles.StateWrongLink {
			replaced++
		}
	}
	return created, replaced, nil
}

func dotfileLinksList(state *dotfileState) *glyph.ListC[dotfileRow] {
	return styleList(glyph.List(&state.visible).Selection(&state.selection)).Render(func(row *dotfileRow) glyph.Component {
		return glyph.HBox.Gap(1)(
			glyph.Text(&row.mark).Style(&row.style).Width(3),
			glyph.HBox.WidthPct(0.22)(glyph.Text(&row.entry.Name).Style(&row.nameStyle)),
			glyph.HBox.WidthPct(0.22)(glyph.Text(&row.basename).Style(&row.nameStyle)),
			glyph.HBox.Grow(1)(glyph.Text(&row.statusLabel).Style(&row.statusStyle)),
		)
	}).OnSelect(func(selected *dotfileRow) {
		for i := range state.visible {
			state.visible[i].nameStyle.FG = glyph.DefaultColor()
		}
		selected.nameStyle.FG = glyph.Cyan
	})
}

func dotfileEnvironmentList(state *dotfileState) *glyph.ListC[audit.Finding] {
	return styleList(glyph.List(&state.findings)).Render(func(finding *audit.Finding) glyph.Component {
		severity := (*string)(&finding.Severity)
		color := glyph.If(severity).Eq(string(audit.SeverityError)).Then(glyph.Red).Else(
			glyph.If(severity).Eq(string(audit.SeverityWarning)).Then(glyph.Yellow).Else(glyph.Green),
		)
		return glyph.HBox.Gap(1)(glyph.Text(severity).FG(color), glyph.Text(&finding.Message))
	})
}

func (state *dotfileState) updateMenu() {
	state.menu = []dotfileMenuItem{{name: "Environment", summary: state.environment}, {name: "Links", summary: state.links}}
	for i := range state.menu {
		state.menu[i].nameColor = glyph.DefaultColor()
		if i == state.menuSelection {
			state.menu[i].nameColor = glyph.Cyan
		}
	}
}

func dotfileLanding(state *dotfileState) (glyph.Component, *glyph.ListC[dotfileMenuItem]) {
	state.updateMenu()
	menu := styleList(glyph.List(&state.menu).Selection(&state.menuSelection)).Render(func(item *dotfileMenuItem) glyph.Component {
		return glyph.VBox(glyph.Text(&item.name).Bold().FG(&item.nameColor), glyph.TextBlock(&item.summary), glyph.Space().Height(1))
	}).OnSelect(func(selected *dotfileMenuItem) {
		for i := range state.menu {
			state.menu[i].nameColor = glyph.DefaultColor()
		}
		selected.nameColor = glyph.Cyan
	})
	return glyph.VBox.Grow(1)(menu), menu
}

func dotfileLinksContent(state *dotfileState, list *glyph.ListC[dotfileRow]) glyph.Component {
	return glyph.VBox.Grow(1)(
		glyph.Text(&state.filterLabel),
		glyph.HBox.Gap(1)(
			glyph.Text(" ").Width(5),
			glyph.HBox.WidthPct(0.22)(glyph.Text("Name").Bold()),
			glyph.HBox.WidthPct(0.22)(glyph.Text("Target").Bold()),
			glyph.Text("State").Bold(),
		),
		glyph.HRule(), list,
	)
}

func dotfilePreviewContent(state *dotfileState, preview *glyph.ListC[string]) glyph.Component {
	return glyph.VBox.Grow(1)(glyph.Text(&state.previewSummary).Bold(), preview)
}

func registerDotfiles(state *dotfileState, app *glyph.App) {
	state.refresh(app)
	stop := func() {
		if state.pending {
			state.quitPending = true
			return
		}
		app.Stop()
	}
	frame := func(title string, content glyph.Component, help string) glyph.Component {
		context := func() string { return "Config: " + state.configPath }
		return screenFrame(title, context, &state.status, content, help)
	}
	landing, menu := dotfileLanding(state)
	app.View("dotfiles", frame("Dotfiles", landing, "Enter open  Esc Home  q quit")).
		Handle("<down>", menu.Down).Handle("<up>", menu.Up).Handle("j", menu.Down).Handle("k", menu.Up).
		Handle("<Enter>", func() {
			if selected := menu.Selected(); selected != nil {
				if selected.name == "Environment" {
					app.Go("dotfiles-environment")
				} else {
					app.Go("dotfiles-links")
				}
			}
		}).
		Handle("e", func() { app.Go("dotfiles-environment") }).Handle("l", func() { app.Go("dotfiles-links") }).
		Handle("r", func() { state.refresh(app) }).Handle("<Esc>", func() { app.Go("home") }).Handle("q", stop).Handle("<C-c>", stop)
	list := dotfileLinksList(state)
	app.View("dotfiles-links", frame("Dotfiles / Links", dotfileLinksContent(state, list), "Space mark  l preview  Esc back  q quit")).
		Handle("<down>", list.Down).Handle("<up>", list.Up).Handle("j", list.Down).Handle("k", list.Up).
		Handle("<Tab>", func() { state.filter = (state.filter + 1) % len(dotfileFilters); state.rebuild() }).
		Handle("<Space>", func() { state.toggle(list.Selected()) }).Handle("a", state.toggleAll).
		Handle("<Enter>", func() {
			if row := list.Selected(); row != nil {
				state.linkDetails(*row)
				state.detailText = strings.Join(state.detail, "\n\n")
				app.Go("dotfiles-detail")
			}
		}).
		Handle("l", func() {
			if state.pending || state.refreshing {
				return
			}
			if err := state.preparePreview(); err != nil {
				state.status = err.Error()
				return
			}
			state.status = ""
			app.Go("dotfiles-preview")
		}).
		Handle("r", func() { state.refresh(app) }).Handle("<Esc>", func() { app.Go("dotfiles") }).Handle("q", stop).Handle("<C-c>", stop)
	updateEnvironment := state.environmentSummary
	updateEnvironment()
	environmentList := dotfileEnvironmentList(state)
	app.View("dotfiles-environment", frame("Dotfiles / Environment", glyph.VBox.Grow(1)(glyph.Text(&state.environment), environmentList), "Enter details  m mode  Esc back  q quit")).
		Handle("<down>", environmentList.Down).Handle("<up>", environmentList.Up).Handle("j", environmentList.Down).Handle("k", environmentList.Up).
		Handle("m", func() {
			if state.mode == layout.ModeXDG {
				state.mode = layout.ModeOSNative
			} else {
				state.mode = layout.ModeXDG
			}
			updateEnvironment()
		}).
		Handle("r", func() { state.refresh(app); updateEnvironment() }).
		Handle("<Enter>", func() {
			if finding := environmentList.Selected(); finding != nil {
				report := state.reports[state.mode]
				state.detail = []string{"Mode: " + string(state.mode), "Config root: " + report.Expected.ConfigRoot, "State: " + string(finding.Severity), "Code: " + finding.Code, "Finding: " + finding.Message, "Recommendation: " + finding.Recommendation}
				state.detailText = strings.Join(state.detail, "\n\n")
				app.Go("dotfiles-environment-detail")
			}
		}).
		Handle("<Esc>", func() { app.Go("dotfiles") }).Handle("q", stop).Handle("<C-c>", stop)
	for name, back := range map[string]string{"dotfiles-detail": "dotfiles-links", "dotfiles-environment-detail": "dotfiles-environment", "dotfiles-failure": "dotfiles-links"} {
		lines := glyph.TextView(&state.detailText).Grow(1).BindNav("<down>", "<up>").BindNav("j", "k").BindPageNav("<C-d>", "<C-u>")
		title := "Dotfiles / Links / Details"
		if back == "dotfiles-environment" {
			title = "Dotfiles / Environment / Details"
		} else if name == "dotfiles-failure" {
			title = "Dotfiles / Links / Failure"
		}
		app.View(name, frame(title, lines, "↑/↓ scroll  Esc back  q quit")).Handle("<Esc>", func() { app.Go(back) }).Handle("q", stop).Handle("<C-c>", stop)
	}
	preview := styleList(glyph.List(&state.previewLines)).Render(func(line *string) glyph.Component { return glyph.TextBlock(line).CharWrap() })
	cancel := func() { state.previewEntries, state.previewStamps = nil, nil; app.Go("dotfiles-links") }
	app.View("dotfiles-preview", frame("Dotfiles / Links / Confirm", dotfilePreviewContent(state, preview), "y apply  n/Esc cancel  q quit")).Handle("<down>", preview.Down).Handle("<up>", preview.Up).Handle("n", cancel).Handle("<Esc>", cancel).Handle("q", stop).Handle("<C-c>", stop).
		Handle("y", func() {
			if state.pending {
				return
			}
			state.pending = true
			state.generation++
			generation := state.generation
			work := state.workerSnapshot()
			state.status = "Applying links; navigation disabled."
			app.Go("dotfiles-pending")
			go func() {
				created, replaced, err := work.applyPreview()
				work.load()
				app.Apply(func() {
					if state.generation != generation {
						return
					}
					state.pending = false
					state.publish(work)
					var failure *dotfiles.ApplyError
					if errors.As(err, &failure) {
						state.status = "Link transaction failed. Inspect details before retrying."
						state.detailText = dotfileFailureDetails(err)
						app.Go("dotfiles-failure")
					} else if err != nil {
						state.status = err.Error() + " Mark links again after reviewing refreshed list."
						app.Go("dotfiles-links")
					} else {
						state.status = fmt.Sprintf("Created %d links; replaced %d links.", created, replaced)
						app.Go("dotfiles-links")
					}
					if state.quitPending {
						app.Stop()
					}
				})
			}()
		})
	app.View("dotfiles-pending", frame("Dotfiles / Links / Applying", glyph.VBox(glyph.Text("Applying marked links").Bold(), glyph.Text("Quit waits for completion.")), "q quit when done")).Handle("q", stop).Handle("<C-c>", stop)
}

func dotfileFailureDetails(err error) string {
	lines := []string{err.Error()}
	var failure *dotfiles.ApplyError
	if errors.As(err, &failure) {
		lines = append(lines, "Action: "+failure.Action, "Path: "+failure.Path, fmt.Sprintf("Rollback attempted: %t", failure.RollbackAttempted))
		if failure.RollbackError != nil {
			lines = append(lines, "Rollback incomplete: "+failure.RollbackError.Error(), "Filesystem may be partially changed.", "Inspect affected paths before retrying; preserve backups.")
		} else if failure.RollbackAttempted {
			lines = append(lines, "Rollback completed.")
		}
		lines = append(lines, "Affected paths:", strings.Join(failure.AffectedPaths, "\n"))
	}
	return strings.Join(lines, "\n\n")
}
