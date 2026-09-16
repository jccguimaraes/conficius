package main

import (
	"bytes"
	"context"
	"fmt"
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

func outdatedRows(packages []packagemanager.Package, selected map[string]bool) []outdatedRow {
	rows := make([]outdatedRow, len(packages))
	for i, pkg := range packages {
		mark := "[ ]"
		if selected[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)] {
			mark = "[x]"
		}
		rows[i] = outdatedRow{Package: pkg, Mark: mark}
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
	app.Go("confirm")
}

func formatPlans(plans []packagemanager.UpdatePlan) string {
	var lines []string
	for _, plan := range plans {
		command := append([]string{plan.Executable}, plan.Args...)
		for i := range command {
			command[i] = shellQuote(command[i])
		}
		line := strings.Join(command, " ")
		if plan.Directory != "" {
			line = fmt.Sprintf("(cd %s && %s)", shellQuote(plan.Directory), line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func shellQuote(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n'\"\\$`;&|<>()[]{}*?!") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func compactError(summary, output string) string {
	output = strings.TrimSpace(strings.ReplaceAll(output, "\n", " "))
	if output == "" {
		return summary
	}
	const max = 160
	if len(output) > max {
		output = output[:max-1] + "…"
	}
	return summary + ": " + output
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
		var output bytes.Buffer
		err := packagemanager.ApplyUpdates(context.Background(), plans, nil, &output, &output)
		app.Apply(func() {
			data.updating = false
			if err != nil {
				data.status = compactError("Upgrade failed: "+err.Error(), output.String())
				return
			}
			clear(data.selected)
			data.status = "Upgrades applied"
			refresh(data, app)
		})
	}()
}
