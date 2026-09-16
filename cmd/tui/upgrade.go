package main

import (
	"bytes"
	"context"
	"fmt"
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

func outdatedRows(packages []packagemanager.Package, selected, failed map[string]bool) []outdatedRow {
	rows := make([]outdatedRow, len(packages))
	for i, pkg := range packages {
		mark := "[ ]"
		if selected[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)] {
			mark = "[x]"
		}
		rows[i] = outdatedRow{Package: pkg, Mark: mark, Failed: failed[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)]}
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
	data.updating, data.status = true, "Applying upgrades..."
	app.Go("results")
	app.RequestRender()
	plans := append([]packagemanager.UpdatePlan(nil), data.plans...)
	go func() {
		failed := make(map[string]bool)
		var failures int
		for _, plan := range plans {
			var output bytes.Buffer
			if err := packagemanager.ApplyUpdates(context.Background(), []packagemanager.UpdatePlan{plan}, nil, &output, &output); err != nil {
				failures++
				for _, pkg := range plan.Packages {
					failed[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)] = true
				}
			}
		}
		app.Apply(func() {
			data.updating = false
			clear(data.failed)
			for key := range data.selected {
				if !failed[key] {
					delete(data.selected, key)
				}
			}
			for key := range failed {
				data.failed[key] = true
			}
			data.rows = outdatedRows(data.packages, data.selected, data.failed)
			if failures > 0 {
				data.status = fmt.Sprintf("%d command(s) failed; red rows remain marked for retry", failures)
				return
			}
			clear(data.selected)
			data.status = "Upgrades applied"
			refresh(data, app)
		})
	}()
}
