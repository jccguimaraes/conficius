package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	glyph "github.com/kungfusheep/glyph"

	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

func toggleSelected(data *model, row *outdatedRow) {
	if row == nil || data.updating {
		return
	}
	key := packageKey(row.Package.Scope, row.Package.Path, row.Package.Manager, row.Package.Name)
	if data.selected[key] {
		delete(data.selected, key)
		delete(data.bumps, key)
	} else {
		if !modeAvailable(data, key, false) {
			data.status = "Update unavailable; press b to mark Bump if available"
			return
		}
		if data.selected == nil {
			data.selected = make(map[string]bool)
		}
		data.selected[key] = true
		delete(data.bumps, key)
	}
	syncPackageRow(data, row)
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
	if data.updating {
		return
	}
	eligible, selected := 0, 0
	for _, pkg := range data.packages {
		key := packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)
		if modeAvailable(data, key, false) || modeAvailable(data, key, true) {
			eligible++
			if data.selected[key] {
				selected++
			}
		}
	}
	if len(data.selected) > 0 && selected == eligible {
		clear(data.selected)
		clear(data.bumps)
		for i := range data.rows {
			syncPackageRow(data, &data.rows[i])
		}
		return
	}
	for i := range data.rows {
		row := &data.rows[i]
		key := packageKey(row.Package.Scope, row.Package.Path, row.Package.Manager, row.Package.Name)
		if data.selected[key] {
			continue
		}
		if modeAvailable(data, key, false) {
			toggleSelected(data, row)
		} else if modeAvailable(data, key, true) {
			toggleBump(data, row)
		}
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
	Mode     string
	Manager  string
	Scope    string
	Packages string
	Command  string
}

func toggleBump(data *model, row *outdatedRow) {
	if data.updating || row == nil {
		return
	}
	key := packageKey(row.Package.Scope, row.Package.Path, row.Package.Manager, row.Package.Name)
	if !data.selected[key] {
		if !setPackageMode(data, key, true) {
			data.status = strings.Replace(data.status, "previous mode retained", "package not marked", 1)
			return
		}
		if data.selected == nil {
			data.selected = make(map[string]bool)
		}
		data.selected[key] = true
	} else {
		setPackageMode(data, key, !data.bumps[key])
	}
	syncPackageRow(data, row)
}

func toggleAllBumps(data *model) {
	if data.updating {
		return
	}
	if len(data.selected) == 0 {
		data.status = "No packages marked"
		return
	}
	bump := false
	for key := range data.selected {
		if !data.bumps[key] {
			bump = true
			break
		}
	}
	unavailable := 0
	for key := range data.selected {
		if !setPackageMode(data, key, bump) {
			unavailable++
		}
	}
	for i := range data.rows {
		syncPackageRow(data, &data.rows[i])
	}
	data.status = fmt.Sprintf("Marked mode: %s; %d unavailable retained previous mode", modeName(bump), unavailable)
	if bump && unavailable > 0 && data.bumpErr != nil {
		data.status += "; Bump availability unknown: " + data.bumpErr.Error()
	}
}

func registerResults(data *model, app *glyph.App) *glyph.ListC[outdatedRow] {
	resultComponent, packageList := resultView(data)
	app.View("results", resultComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { data.active = nil; app.Go("scopes") }).
		Handle("p", func() { app.Go("profiles") }).
		Handle("r", func() { refresh(data, app) }).
		Handle("b", func() { toggleBump(data, packageList.Selected()); app.RequestRender() }).
		Handle("B", func() { toggleAllBumps(data); app.RequestRender() }).
		Handle("x", func() { cancelSecurity(data, app) }).
		Handle("v", func() { app.Go("vulnerabilities") }).
		Handle("<Enter>", func() {
			if row := packageList.Selected(); row != nil {
				data.packageDetail = row.Package
				data.packageDetailText = packageDetails(row)
				app.Go("package-detail")
			}
		}).
		Handle("<down>", packageList.Down).
		Handle("<up>", packageList.Up).
		Handle("j", packageList.Down).
		Handle("k", packageList.Up).
		Handle("<C-d>", packageList.HalfPageDown).
		Handle("<C-u>", packageList.HalfPageUp).
		Handle("<Space>", func() { toggleSelected(data, packageList.Selected()); app.RequestRender() }).
		Handle("a", func() { toggleAll(data); app.RequestRender() }).
		Handle("u", func() { prepareUpgrade(data, app, false) }).
		Handle("U", func() { prepareUpgrade(data, app, true) })
	return packageList
}

func prepareUpgrade(data *model, app *glyph.App, all bool) {
	resetUpgradePlans(data)
	if data.packageConfigErr != nil {
		data.status = fmt.Sprintf("Package upgrades blocked: %v", data.packageConfigErr)
		app.RequestRender()
		return
	}
	plans, skipped, err := selectedPlans(data, all)
	if err == nil && len(plans) == 0 {
		data.status = "No packages marked with an available action"
		if skipped > 0 {
			data.status += fmt.Sprintf("; %d unmarked packages have no Update available", skipped)
		}
		app.RequestRender()
		return
	}
	if err == nil {
		err = validateUpgradePlans(data, plans)
	}
	if errors.Is(err, packagemanager.ErrDirtyProject) {
		data.plans = plans
		showDirtyConfirmation(data, app, err)
		return
	}
	if err != nil {
		data.status = "Package upgrade preview blocked; no packages were upgraded"
		data.updateErrorLines = strings.Split(upgradeErrorMessage(err)+"\n\nNo packages were upgraded. The entire requested batch was blocked; no packages were skipped.\n\nEsc returns to packages. Use Space to unmark unsupported or blocked rows, then u to preview marked packages. U includes every listed package, including unsupported rows.\n\nFor unsupported updates, use the native package manager outside this TUI. For project safety errors, review package configuration and Git state before retrying; TUI safety checks remain enforced.", "\n")
		app.Go("update-error")
		return
	}
	showUpgradePreview(data, app, plans)
	if skipped > 0 {
		data.status += fmt.Sprintf("; %d unmarked packages have no Update available and were not included", skipped)
	}
}

func showUpgradePreview(data *model, app *glyph.App, plans []packagemanager.UpdatePlan) {
	data.plans, data.preview = plans, formatPlans(plans)
	data.commands = commandRows(plans)
	packageCount, bumpCount := 0, 0
	for _, plan := range plans {
		packageCount += len(plan.Packages)
		if plan.Bump {
			bumpCount += len(plan.Packages)
		}
	}
	data.planSummary = fmt.Sprintf("%d packages in %d operations: %d Update, %d Bump", packageCount, len(plans), packageCount-bumpCount, bumpCount)
	data.status = "Review operations before applying upgrades"
	app.Go("confirm")
}

func commandRows(plans []packagemanager.UpdatePlan) []commandRow {
	rows := make([]commandRow, len(plans))
	for i, plan := range plans {
		scope := plan.Scope
		if plan.Directory != "" {
			scope += "  " + plan.Directory
		}
		rows[i] = commandRow{Mode: modeName(plan.Bump), Manager: plan.Manager, Scope: scope, Packages: strconv.Itoa(len(plan.Packages)) + " package(s)", Command: formatPlan(plan)}
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
	if plan.Manager == packagemanager.PM_DOCKER && plan.Executable == "" {
		var edits []string
		for _, pkg := range plan.Packages {
			edits = append(edits, fmt.Sprintf("%s [%s]: %s => %s", pkg.Manifest, pkg.Name, pkg.Current, pkg.Latest))
		}
		line = "Built-in file editing: " + strings.Join(edits, "; ")
	}
	if plan.RunAs != "" {
		method := plan.RunAsMethod
		if method == "" {
			method = "sudo"
		}
		line = "[run_as=" + plan.RunAs + " method=" + method + "] " + line
	}
	if plan.Directory != "" && plan.Executable != "" {
		line = fmt.Sprintf("(cd %s && %s)", shellQuote(plan.Directory), line)
	}
	if plan.Impact.MayChangeManifests {
		line += " # may change manifests, lockfiles and transitive dependencies; no automatic rollback"
	}
	return line
}

func shellQuote(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n'\"\\$`;&|<>()[]{}*?!") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func validateUpgradePlans(data *model, plans []packagemanager.UpdatePlan) error {
	seen := make(map[string]string)
	for _, plan := range plans {
		for _, pkg := range plan.Packages {
			key := packageKey(plan.Scope, plan.Directory, plan.Manager, pkg.Name)
			if executable, exists := seen[key]; exists && executable != plan.Executable {
				return fmt.Errorf("ambiguous %s update for %s (%s %s): executables %q and %q; disable duplicate manager installations in package configuration or repair PATH, then refresh", plan.Manager, pkg.Name, plan.Scope, plan.Directory, executable, plan.Executable)
			}
			seen[key] = plan.Executable
		}
	}
	return packagemanager.ValidateProjectMutations(plans, data.profile.Profile, data.allowDirty, false)
}

func applyUpgrades(data *model, app *glyph.App) {
	if data.packageConfigErr != nil {
		data.status = fmt.Sprintf("Package upgrades blocked: %v", data.packageConfigErr)
		app.RequestRender()
		return
	}
	if data.updating {
		return
	}
	if err := validateUpgradePlans(data, data.plans); err != nil {
		if errors.Is(err, packagemanager.ErrDirtyProject) {
			showDirtyConfirmation(data, app, err)
			return
		}
		data.status = fmt.Sprintf("Package upgrades blocked: %s", upgradeErrorMessage(err))
		app.RequestRender()
		return
	}
	data.runInteractive = true
	app.Stop()
}

func runInteractiveUpgrades(data *model) error {
	if data.packageConfigErr != nil {
		err := fmt.Errorf("package upgrades blocked: %w", data.packageConfigErr)
		data.status = fmt.Sprintf("Package upgrades blocked: %v", data.packageConfigErr)
		return err
	}
	if err := validateUpgradePlans(data, data.plans); err != nil {
		data.status = fmt.Sprintf("Package upgrades blocked: %s", upgradeErrorMessage(err))
		return err
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open controlling terminal: %w", err)
	}
	defer func() { _ = tty.Close() }()
	applyUpgradePlans(data, tty)
	return execFreshTUI(tty, data)
}

func applyUpgradePlans(data *model, tty *os.File) {
	plans := append([]packagemanager.UpdatePlan(nil), data.plans...)
	options := packagemanager.QueryOptionsFromProfile(data.profile.Profile)
	ctx := context.Background()
	clear(data.attempted)
	for _, plan := range plans {
		for _, pkg := range plan.Packages {
			key := packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)
			data.attempted[key] = pkg.Current
			if data.bumps == nil {
				data.bumps = make(map[string]bool)
			}
			data.bumps[key] = plan.Bump
		}
	}
	failed := make(map[string]string)
	var failures int
	for _, plan := range plans {
		batch := []packagemanager.UpdatePlan{plan}
		outcomes, applyErr := packagemanager.ApplyUpdatesWithOutcomes(ctx, batch, false, tty, tty, tty)
		_, verifyErr := packagemanager.VerifyUpdateOutcomes(ctx, batch, outcomes, options)
		err := errors.Join(applyErr, verifyErr)
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
}

func resetUpgradePlans(data *model) {
	data.plans, data.commands = nil, nil
	data.preview, data.planSummary, data.dirtyWarning = "", "", ""
	data.allowDirty, data.runInteractive = false, false
}

func upgradeErrorMessage(err error) string {
	var mutation *packagemanager.ProjectMutationError
	if errors.As(err, &mutation) {
		paths := strings.Join(mutation.Paths, "\n")
		if errors.Is(err, packagemanager.ErrUnversionedProject) {
			return "Projects are not in a Git repository:\n" + paths + "\nInitialize or restore Git tracking before retrying."
		}
		return "Projects have uncommitted changes:\n" + paths + "\nReview changes, then confirm this batch in the TUI."
	}
	return err.Error()
}

func showDirtyConfirmation(data *model, app *glyph.App, err error) {
	data.dirtyWarning = upgradeErrorMessage(err) + "\n\nUpdates may change manifests, lockfiles and transitive dependencies. No automatic rollback. Approval applies only to this batch; commands still require review."
	data.status = "Continue with uncommitted changes"
	app.Go("dirty-confirm")
}

func registerUpgradeConfirmations(data *model, app *glyph.App) {
	cancel := func() { resetUpgradePlans(data); app.Go("results") }
	app.View("dirty-confirm", screenFrame("Continue with uncommitted changes", packageContext(data), &data.status,
		glyph.TextBlock(&data.dirtyWarning).CharWrap(), "y continue to command preview  n/Esc cancel")).
		Handle("y", func() {
			if len(data.plans) == 0 {
				cancel()
				return
			}
			data.allowDirty = true
			if err := validateUpgradePlans(data, data.plans); err != nil {
				data.allowDirty = false
				data.status = "Package upgrades blocked: " + upgradeErrorMessage(err)
				app.RequestRender()
				return
			}
			showUpgradePreview(data, app, data.plans)
		}).Handle("n", cancel).Handle("<Esc>", cancel).Handle("q", func() { resetUpgradePlans(data); app.Stop() })
	app.View("confirm", confirmView(data)).
		Handle("q", func() { resetUpgradePlans(data); app.Stop() }).
		Handle("y", func() { applyUpgrades(data, app) }).
		Handle("n", cancel).Handle("<Esc>", cancel)
}

func modeName(bump bool) string {
	if bump {
		return "Bump"
	}
	return "Update"
}

func modeAvailable(data *model, key string, bump bool) bool {
	query := data.query
	if bump {
		query = data.bumpQuery
	}
	return containsPackage(packagemanager.Records(query.Packages), key)
}

func setPackageMode(data *model, key string, bump bool) bool {
	if !modeAvailable(data, key, bump) {
		data.status = modeName(bump) + " unavailable; previous mode retained"
		if bump && data.bumpErr != nil {
			data.status = "Bump availability unknown; previous mode retained: " + data.bumpErr.Error()
		}
		return false
	}
	if data.bumps == nil {
		data.bumps = make(map[string]bool)
	}
	data.bumps[key] = bump
	resetUpgradePlans(data)
	data.status = "Marked mode: " + modeName(bump)
	return true
}

func unionTargets(normal, bump packagemanager.QueryResult) []packagemanager.Package {
	packages := packagemanager.Records(normal.Packages)
	for _, pkg := range packagemanager.Records(bump.Packages) {
		if !containsPackage(packages, packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)) {
			packages = append(packages, pkg)
		}
	}
	return packages
}

func syncPackageRow(data *model, row *outdatedRow) {
	key := packageKey(row.Package.Scope, row.Package.Path, row.Package.Manager, row.Package.Name)
	bump := data.selected[key] && data.bumps[key]
	row.Mark, row.MarkColor = "[ ]", glyph.Color{}
	row.Availability = ""
	if !modeAvailable(data, key, false) {
		row.Availability = "Bump only"
		if data.outdatedErr != nil {
			row.Availability = "Update unknown"
		}
	}
	if data.selected[key] {
		row.Mark, row.MarkColor = "[U]", glyph.Cyan
		if bump {
			row.Mark, row.MarkColor = "[B]", glyph.Yellow
		}
	}
	query := data.query
	if bump || row.Availability == "Bump only" {
		query = data.bumpQuery
	}
	for _, pkg := range packagemanager.Records(query.Packages) {
		if packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name) == key {
			row.Package = pkg
			break
		}
	}
}

func rebuildOutdatedRows(data *model) {
	data.rows = outdatedRows(data.packages, data.selected, data.failed)
	for i := range data.rows {
		syncPackageRow(data, &data.rows[i])
	}
}

func selectedPlans(data *model, all bool) ([]packagemanager.UpdatePlan, int, error) {
	allowed := [2]map[string]bool{make(map[string]bool), make(map[string]bool)}
	skipped := 0
	for _, pkg := range data.packages {
		key := packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)
		if !data.selected[key] && !all {
			continue
		}
		bump := data.selected[key] && data.bumps[key]
		if !modeAvailable(data, key, bump) {
			if !data.selected[key] {
				skipped++
				continue
			}
			return nil, skipped, fmt.Errorf("%s unavailable for %s; mode retained; refresh to check availability", modeName(bump), pkg.Name)
		}
		group := 0
		if bump {
			group = 1
		}
		allowed[group][key] = true
	}
	var plans []packagemanager.UpdatePlan
	for group, query := range []packagemanager.QueryResult{data.query, data.bumpQuery} {
		query.Bump = group == 1
		query.Packages = packagemanager.Filter(query.Packages, func(pkg packagemanager.Package) bool {
			return allowed[group][packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)]
		})
		part, err := packagemanager.PlanUpdates(query, nil, nil)
		if err != nil {
			return nil, skipped, err
		}
		plans = append(plans, part...)
	}
	return plans, skipped, nil
}
