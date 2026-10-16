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

func registerResults(data *model, app *glyph.App) *glyph.ListC[outdatedRow] {
	resultComponent, packageList := resultView(data)
	app.View("results", resultComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { data.active = nil; app.Go("scopes") }).
		Handle("p", func() { app.Go("profiles") }).
		Handle("r", func() { refresh(data, app) }).
		Handle("v", func() { app.Go("vulnerabilities") }).
		Handle("<Enter>", func() {
			if row := packageList.Selected(); row != nil {
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
}

func showUpgradePreview(data *model, app *glyph.App, plans []packagemanager.UpdatePlan) {
	data.plans, data.preview = plans, formatPlans(plans)
	data.commands = commandRows(plans)
	packageCount := 0
	for _, plan := range plans {
		packageCount += len(plan.Packages)
	}
	data.planSummary = fmt.Sprintf("%d packages in %d commands", packageCount, len(plans))
	data.status = "Review commands before applying upgrades"
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
	if plan.Impact.MayChangeManifests {
		line += " # may change go.mod, go.sum and transitive dependencies; no automatic rollback"
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
	plans := append([]packagemanager.UpdatePlan(nil), data.plans...)
	clear(data.attempted)
	for _, plan := range plans {
		for _, pkg := range plan.Packages {
			data.attempted[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)] = pkg.Current
		}
	}
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
