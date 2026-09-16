package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	glyph "github.com/kungfusheep/glyph"

	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

func toggleSelected(data *model, row *outdatedRow) {
	if row == nil {
		return
	}
	pkg := &row.Package
	key := packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)
	if data.selected[key] {
		delete(data.selected, key)
		row.Mark = "[ ]"
	} else {
		data.selected[key] = true
		row.Mark = "[x]"
	}
}

func outdatedRows(packages []packagemanager.Package, selected map[string]bool, failed map[string]string) []outdatedRow {
	rows := make([]outdatedRow, len(packages))
	for i, pkg := range packages {
		mark := "[ ]"
		if selected[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)] {
			mark = "[x]"
		}
		err := failed[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)]
		rows[i] = outdatedRow{Package: pkg, Mark: mark, Failed: err != "", Error: err}
	}
	return rows
}

func toggleAll(data *model) {
	if len(data.packages) > 0 && len(data.selected) == len(data.packages) {
		clear(data.selected)
		for i := range data.rows {
			data.rows[i].Mark = "[ ]"
		}
		return
	}
	for i := range data.rows {
		pkg := &data.rows[i].Package
		data.selected[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)] = true
		data.rows[i].Mark = "[x]"
	}
}

func containsPackage(packages []packagemanager.Package, key string) bool {
	for _, pkg := range packages {
		if packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name) == key {
			return true
		}
	}
	return false
}

type commandRow struct {
	Manager  string
	Scope    string
	Packages string
	Command  string
}

func prepareUpgrade(data *model, app *glyph.App, all bool) {
	allowed := make(map[string]bool)
	for _, pkg := range data.packages {
		key := packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)
		if all || data.selected[key] {
			allowed[key] = true
		}
	}
	if len(allowed) == 0 {
		data.status = "No packages marked"
		app.RequestRender()
		return
	}
	query := data.query
	query.Packages = packagemanager.Filter(query.Packages, func(pkg packagemanager.Package) bool {
		return allowed[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)]
	})
	plans, err := packagemanager.PlanUpdates(query, nil, nil)
	if err != nil {
		data.status = err.Error()
		app.RequestRender()
		return
	}
	data.plans, data.preview = plans, formatPlans(plans)
	data.commands = commandRows(plans)
	packageCount := 0
	for _, plan := range plans {
		packageCount += len(plan.Packages)
	}
	data.planSummary = fmt.Sprintf("%d packages in %d commands", packageCount, len(plans))
	app.Go("confirm")
}

func commandRows(plans []packagemanager.UpdatePlan) []commandRow {
	rows := make([]commandRow, len(plans))
	for i, plan := range plans {
		scope := plan.Scope
		if plan.Directory != "" {
			scope += "  " + plan.Directory
		}
		rows[i] = commandRow{Manager: plan.Manager, Scope: scope, Packages: strconv.Itoa(len(plan.Packages)) + " package(s)", Command: formatPlan(plan)}
	}
	return rows
}

func formatPlans(plans []packagemanager.UpdatePlan) string {
	var lines []string
	for _, plan := range plans {
		lines = append(lines, formatPlan(plan))
	}
	return strings.Join(lines, "\n")
}

func formatPlan(plan packagemanager.UpdatePlan) string {
	command := append([]string{plan.Executable}, plan.Args...)
	for i := range command {
		command[i] = shellQuote(command[i])
	}
	line := strings.Join(command, " ")
	if plan.Directory != "" {
		line = fmt.Sprintf("(cd %s && %s)", shellQuote(plan.Directory), line)
	}
	return line
}

func shellQuote(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n'\"\\$`;&|<>()[]{}*?!") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func applyUpgrades(data *model, app *glyph.App) {
	if data.updating {
		return
	}
	data.runInteractive = true
	app.Stop()
}

func runInteractiveUpgrades(data *model) error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open controlling terminal: %w", err)
	}
	defer func() { _ = tty.Close() }()
	plans := append([]packagemanager.UpdatePlan(nil), data.plans...)
	failed := make(map[string]string)
	var failures int
	for _, plan := range plans {
		err := packagemanager.ApplyUpdates(context.Background(), []packagemanager.UpdatePlan{plan}, tty, tty, tty)
		if err == nil {
			continue
		}
		failures++
		for _, pkg := range plan.Packages {
			failed[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)] = err.Error()
		}
	}
	clear(data.failed)
	for key := range data.selected {
		if failed[key] == "" {
			delete(data.selected, key)
		}
	}
	for key, message := range failed {
		data.failed[key] = message
	}
	if failures > 0 {
		data.status = fmt.Sprintf("%d command(s) failed", failures)
	} else {
		clear(data.selected)
		data.status = "Upgrades applied"
	}
	return execFreshTUI(tty, data)
}
