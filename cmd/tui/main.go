package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"

	glyph "github.com/kungfusheep/glyph"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/euvd"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/jccguimaraes/conficius/internal/security"
	"github.com/jccguimaraes/conficius/internal/vulnerability"
)

var version = "dev"

type outdatedRow struct {
	Package packagemanager.Package
	Mark    string
	Failed  bool
	Error   string
}

type vulnerabilityRow struct {
	security.Evidence
	Current   string
	RuleCount string
}

type model struct {
	dotfiles          *dotfileState
	homeCards         []homeCard
	localLoading      bool
	localChecked      bool
	localErr          error
	installedCount    int
	networkPending    bool
	outdatedChecked   bool
	outdatedErr       error
	securityScanning  bool
	securityChecked   bool
	securityErr       error
	securitySummary   security.Summary
	securityCount     int
	dotfileGeneration uint64
	dotfileCancel     context.CancelFunc
	profile           config.ResolvedProfile
	scopes            []config.Scope
	active            *config.Scope
	packages          []packagemanager.Package
	rows              []outdatedRow
	query             packagemanager.QueryResult
	selected          map[string]bool
	failed            map[string]string
	attempted         map[string]string
	plans             []packagemanager.UpdatePlan
	preview           string
	commands          []commandRow
	planSummary       string
	packageDetailText string
	packageFocus      func(func())
	updateErrorLines  []string
	updating          bool
	runInteractive    bool
	drift             []config.PackageState
	findings          []vulnerabilityRow
	detail            vulnerabilityRow
	mappings          []config.VulnerabilityMapping
	euvdMaps          []config.EUVDMapping
	osvClient         vulnerability.Queryer
	euvdClient        euvd.Queryer
	status            string
	loading           bool
	cancel            context.CancelFunc
	generation        uint64
}

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	configFlag, err := parseStartupFlags(os.Args[1:], os.Stdout)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	path, discoverErr := config.Discover(configFlag)
	if path == "" {
		path = "(not found)"
	}
	project, configErr := config.LoadDiscovered(configFlag)
	configErr = errors.Join(discoverErr, configErr)
	dotfiles := newDotfileState(project, configErr, path)
	if configErr != nil || project == nil {
		project = &config.Project{Version: config.CurrentVersion}
	}
	profile, err := project.ResolveProfile("", false)
	if err != nil {
		dotfiles.configErr = err
		project = &config.Project{Version: config.CurrentVersion}
		profile, _ = project.ResolveProfile("", false)
	}
	scopes, err := config.ResolveScopes(project, profile, false, nil, nil)
	if err != nil {
		dotfiles.configErr = errors.Join(dotfiles.configErr, err)
		project = &config.Project{Version: config.CurrentVersion}
		profile, _ = project.ResolveProfile("", false)
		scopes, err = config.ResolveScopes(project, profile, false, nil, nil)
		if err != nil {
			return err
		}
	}

	data := &model{
		dotfiles: dotfiles, networkPending: true,
		profile: profile, scopes: scopes, mappings: project.Vulnerabilities.Mappings, euvdMaps: project.Vulnerabilities.EUVD,
		osvClient:  vulnerability.NewClient(http.DefaultClient, os.Getenv("CONFICIUS_OSV_URL")),
		euvdClient: euvd.NewClient(http.DefaultClient, os.Getenv("CONFICIUS_EUVD_URL")),
		status:     "Loading...", selected: make(map[string]bool), failed: make(map[string]string), attempted: make(map[string]string),
	}
	startView := "home"
	if encoded := os.Getenv(restartStateEnv); encoded != "" {
		state, decodeErr := decodeRestartState(encoded)
		if decodeErr != nil {
			return decodeErr
		}
		startView = restoreRestartState(data, state)
		_ = os.Unsetenv(restartStateEnv)
	}
	if err := runSession(data, startView); err != nil {
		return err
	}
	if !data.runInteractive {
		return nil
	}
	return runInteractiveUpgrades(data)
}

func runSession(data *model, startView string) error {
	app := glyph.NewApp()
	app.HideCursor()
	size := app.Size()
	updateScreenSize(size.Width, size.Height)
	app.Handle("f", func() {
		if !terminalTooSmall() && (data.dotfiles == nil || !data.dotfiles.pending) {
			app.EnterJumpMode()
		}
	})
	if startView == "results" {
		refresh(data, app)
	} else {
		refreshLocal(data, app)
	}
	if data.dotfiles != nil {
		registerDotfiles(data.dotfiles, app)
	}
	registerHome(data, app)
	scopeComponent, scopeList := scopeView(data, app)
	app.View("scopes", scopeComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { updateHome(data); app.Go("home") }).
		Handle("<down>", scopeList.Down).
		Handle("<up>", scopeList.Up).
		Handle("r", func() { refresh(data, app) })
	resultComponent, packageList := resultView(data)
	app.View("results", resultComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("scopes") }).
		Handle("r", func() { refresh(data, app) }).
		Handle("d", func() { app.Go("drift") }).
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
	errorComponent, errorList := updateErrorView(data)
	app.View("update-error", errorComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("results") }).
		Handle("<down>", errorList.Down).
		Handle("<up>", errorList.Up)
	driftComponent, driftList := driftView(data)
	app.View("drift", driftComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("results") }).
		Handle("o", func() { app.Go("results") }).
		Handle("v", func() { app.Go("vulnerabilities") }).
		Handle("r", func() { refresh(data, app) }).
		Handle("<down>", driftList.Down).
		Handle("<up>", driftList.Up).
		Handle("j", driftList.Down).
		Handle("k", driftList.Up).
		Handle("<C-d>", driftList.HalfPageDown).
		Handle("<C-u>", driftList.HalfPageUp).
		Handle("<Enter>", func() {
			if row := driftList.Selected(); row != nil {
				data.packageDetailText = driftDetails(row)
				app.Go("drift-detail")
			}
		})
	app.View("confirm", confirmView(data)).
		Handle("q", app.Stop).
		Handle("y", func() { applyUpgrades(data, app) }).
		Handle("n", func() { app.Go("results") }).
		Handle("<Esc>", func() { app.Go("results") })
	vulnerabilityComponent, vulnerabilityList := vulnerabilityView(data)
	app.View("vulnerabilities", vulnerabilityComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("results") }).
		Handle("o", func() { app.Go("results") }).
		Handle("d", func() { app.Go("drift") }).
		Handle("r", func() { refresh(data, app) }).
		Handle("<Enter>", func() {
			if selected := vulnerabilityList.Selected(); selected != nil {
				data.detail = *selected
				app.Go("vulnerability-detail")
			}
		}).
		Handle("<down>", vulnerabilityList.Down).
		Handle("<up>", vulnerabilityList.Up).
		Handle("j", vulnerabilityList.Down).
		Handle("k", vulnerabilityList.Up).
		Handle("<C-d>", vulnerabilityList.HalfPageDown).
		Handle("<C-u>", vulnerabilityList.HalfPageUp)
	app.View("vulnerability-detail", vulnerabilityDetailView(data, app)).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("vulnerabilities") })
	for name, back := range map[string]string{"package-detail": "results", "drift-detail": "drift"} {
		details := glyph.TextView(&data.packageDetailText).WordWrap().Grow(1).BindNav("j", "k").BindNav("<down>", "<up>").BindPageNav("<C-d>", "<C-u>")
		app.View(name, screenFrame("Packages / Details", "Profile: "+data.profile.Name, &data.status, details, "↑/↓ scroll  Esc back  q quit")).
			Handle("<Esc>", func() { app.Go(back) }).Handle("q", app.Stop)
	}
	keepPackages := preserveListFocus(packageList, &data.rows, func(row *outdatedRow) string {
		return packageKey(row.Package.Scope, row.Package.Path, row.Package.Manager, row.Package.Name)
	})
	keepDrift := preserveListFocus(driftList, &data.drift, func(row *config.PackageState) string {
		return packageKey(row.Scope, row.Path, row.Manager, row.Name)
	})
	keepFindings := preserveListFocus(vulnerabilityList, &data.findings, func(row *vulnerabilityRow) string {
		return packageKey(row.Scope, row.Path, row.Manager, row.Package) + "\x00" + row.Source + "\x00" + row.SourceID
	})
	data.packageFocus = func(update func()) { keepPackages(func() { keepDrift(func() { keepFindings(update) }) }) }
	defer func() { data.packageFocus = nil }()
	registerScreenHelp(app, data)
	startView, _ = registerResizeView(app, data, startView)
	defer func() {
		if data.dotfileCancel != nil {
			data.dotfileCancel()
		}
		if data.cancel != nil {
			data.cancel()
		}
	}()
	return app.RunFrom(startView)
}

func openURL(rawURL string) error {
	if !strings.HasPrefix(rawURL, "https://www.cve.org/CVERecord?id=CVE-") {
		return errors.New("no CVE link available")
	}
	if err := exec.Command("open", rawURL).Start(); err != nil {
		return fmt.Errorf("open CVE link: %w", err)
	}
	return nil
}

func refresh(data *model, app *glyph.App) {
	refreshPackages(data, app, true)
}

func refreshLocal(data *model, app *glyph.App) {
	refreshPackages(data, app, false)
}

func refreshPackages(data *model, app *glyph.App, full bool) {
	if data.cancel != nil {
		data.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	data.cancel = cancel
	data.generation++
	generation := data.generation
	data.loading = true
	data.localLoading, data.localChecked, data.localErr = true, false, nil
	data.networkPending, data.outdatedChecked, data.outdatedErr = true, false, nil
	data.securityScanning, data.securityChecked, data.securityErr = false, false, nil
	data.status = "Local inventory loading; outdated unknown; security unknown (network pending)"
	updateHome(data)
	scopeSelection := selectedScopes(data)
	profile := data.profile.Profile
	mappings := append([]config.VulnerabilityMapping(nil), data.mappings...)
	euvdMaps := append([]config.EUVDMapping(nil), data.euvdMaps...)
	app.RequestRender()
	apply := func(update func()) {
		app.Apply(func() {
			if data.packageFocus != nil {
				data.packageFocus(update)
			} else {
				update()
			}
		})
	}
	go func() {
		scopes := make([]packagemanager.Scope, len(scopeSelection))
		for i, scope := range scopeSelection {
			scopes[i] = packagemanager.Scope{Kind: scope.Kind, Path: scope.Path}
		}
		options := packagemanager.QueryOptionsFromProfile(profile)
		type outdatedResult struct {
			result packagemanager.QueryResult
			err    error
		}
		type inventoryResult struct {
			result packagemanager.InstalledResult
			err    error
		}
		outdatedCh, inventoryCh := make(chan outdatedResult, 1), make(chan inventoryResult, 1)
		if full {
			go func() {
				result, err := packagemanager.QueryOutdatedScopesWithOptions(ctx, nil, scopes, options)
				outdatedCh <- outdatedResult{result, err}
			}()
		}
		go func() {
			result, err := packagemanager.QueryInstalledScopesWithOptions(ctx, nil, scopes, options)
			inventoryCh <- inventoryResult{result, err}
		}()
		inventoryData := <-inventoryCh
		inventory, inventoryErr := inventoryData.result, inventoryData.err
		var managerPackages []packagemanager.InstalledPackage
		if includesGlobal(scopeSelection) {
			var managerErr error
			managerPackages, managerErr = packagemanager.ManagerPackages(ctx, inventory.Managers)
			inventory.Packages = append(inventory.Packages, managerPackages...)
			inventoryErr = errors.Join(inventoryErr, managerErr)
		}
		selected, drift := evaluateInventory(profile, inventory.Packages)
		if ctx.Err() != nil {
			return
		}
		apply(func() {
			if ctx.Err() != nil || !isCurrentGeneration(data.generation, generation) {
				return
			}
			data.localLoading, data.localChecked, data.localErr = false, true, inventoryErr
			data.installedCount, data.drift = len(inventory.Packages), drift
			if !full {
				data.loading = false
			}
			data.status = appendCollisionStatus(appendManagerStatus(fmt.Sprintf("%d installed, %d drift; outdated unknown; security unknown (network pending)", len(inventory.Packages), len(drift)), inventory.Managers), inventory.Commands)
			if inventoryErr != nil {
				data.status = queryStatus(inventoryErr) + "; outdated unknown; security unknown (network pending)"
			}
			updateHome(data)
		})
		if !full {
			return
		}
		outdatedData := <-outdatedCh
		result, outdatedErr := outdatedData.result, outdatedData.err
		if includesGlobal(scopeSelection) {
			managerUpdates, managerUpdateErr := packagemanager.ManagerUpdates(ctx, inventory.Managers, managerPackages)
			packagemanager.MergePackages(result.Packages, managerUpdates)
			outdatedErr = errors.Join(outdatedErr, managerUpdateErr)
		}
		securityTargets := packagemanager.Records(result.Packages)
		packages := filterOutdated(append([]packagemanager.Package(nil), securityTargets...), selected)
		scanPackages := inventory.Packages
		if profile.SecurityScope == "selected" {
			scanPackages = filterInstalled(inventory.Packages, selected)
		}
		if ctx.Err() != nil {
			return
		}
		err := errors.Join(outdatedErr, inventoryErr)
		apply(func() {
			if ctx.Err() != nil || !isCurrentGeneration(data.generation, generation) {
				return
			}
			data.loading = false
			data.networkPending, data.outdatedChecked, data.outdatedErr = false, true, outdatedErr
			data.securityScanning = true
			data.query = result
			data.packages = packages
			for key := range data.selected {
				if !containsPackage(packages, key) {
					delete(data.selected, key)
				}
			}
			upgraded, unchanged, failed := reconcileUpgradeOutcomes(data, packages)
			data.rows = outdatedRows(packages, data.selected, data.failed)
			data.drift = drift
			data.status = appendCollisionStatus(appendManagerStatus(refreshStatus(len(packages), len(drift), upgraded, unchanged, failed), result.Managers), inventory.Commands)
			if err != nil {
				data.status = queryStatus(err)
			}
			data.status += "; security unknown (scanning)"
			updateHome(data)
		})
		report, scanErr := security.Scan(ctx, scanPackages, security.Options{
			Outdated: securityTargets, OSVMappings: mappings, EUVDMappings: euvdMaps,
			OSVClient: data.osvClient, EUVDClient: data.euvdClient,
		})
		if ctx.Err() != nil {
			return
		}
		apply(func() {
			if ctx.Err() != nil || !isCurrentGeneration(data.generation, generation) {
				return
			}
			data.securityScanning, data.securityChecked, data.securityErr = false, true, scanErr
			data.securitySummary, data.securityCount = report.Summary, len(scanPackages)
			data.findings = vulnerabilityRows(report.Evidence)
			data.status = vulnerabilityStatus(vulnerabilitySummary(len(packages), len(drift), report.Summary, report.Evidence), err, scanErr)
			if len(data.failed) > 0 {
				data.status += fmt.Sprintf("; %d failed updates", len(data.failed))
			}
			data.status = appendCollisionStatus(appendManagerStatus(data.status, result.Managers), inventory.Commands)
			updateHome(data)
		})
	}()
}

func vulnerabilitySummary(outdated, drift int, summary security.Summary, evidence []security.Evidence) string {
	unknown := 0
	for _, item := range evidence {
		if item.Status == security.StatusUnknown && item.Advisory != "" && len(item.Rules) > 0 {
			unknown++
		}
	}
	return fmt.Sprintf("%d outdated, %d drift; %d vulnerable, %d unknown, %d security errors", outdated, drift,
		summary.Vulnerable, unknown, summary.Errors)
}

func queryStatus(err error) string {
	if err == nil {
		return ""
	}
	return "Package inspection failed. Run `conficius packages doctor`."
}

func managerStatusSummary(statuses []packagemanager.ManagerStatus) string {
	var summaries []string
	for _, status := range statuses {
		if status.Status == packagemanager.StatusAvailable || (status.Status == packagemanager.StatusUnavailable && !status.Evidence.GlobalConfig) {
			continue
		}
		summaries = append(summaries, status.Name+" "+status.Status)
	}
	return strings.Join(summaries, ", ")
}

func appendManagerStatus(summary string, statuses []packagemanager.ManagerStatus) string {
	managerSummary := managerStatusSummary(statuses)
	if managerSummary == "" {
		return summary
	}
	return summary + "; managers: " + managerSummary
}

func appendCollisionStatus(summary string, commands []packagemanager.ExposedCommand) string {
	collisions := packagemanager.CollisionCandidates(commands)
	if len(collisions) == 0 {
		return summary
	}
	names := make([]string, len(collisions))
	for i, collision := range collisions {
		names[i] = collision.Command
	}
	return fmt.Sprintf("%s; %d command collisions: %s", summary, len(collisions), strings.Join(names, ", "))
}

func vulnerabilityRows(evidence []security.Evidence) []vulnerabilityRow {
	rows := make([]vulnerabilityRow, 0, len(evidence))
	for _, item := range evidence {
		if item.Status == security.StatusClean || item.Advisory == "" || (item.Status == security.StatusUnknown && len(item.Rules) == 0) {
			continue
		}
		rows = append(rows, vulnerabilityRow{Evidence: item, Current: item.Version, RuleCount: fmt.Sprint(len(item.Rules))})
	}
	return rows
}

func selectScope(data *model, scope *config.Scope) {
	data.active = scope
}

func filterOutdated(packages []packagemanager.Package, selected []config.InstalledVersion) []packagemanager.Package {
	allowed := selectedKeys(selected)
	result := packages[:0]
	for _, pkg := range packages {
		if _, ok := allowed[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)]; ok {
			result = append(result, pkg)
		}
	}
	return result
}

func filterInstalled(packages []packagemanager.InstalledPackage, selected []config.InstalledVersion) []packagemanager.InstalledPackage {
	allowed := selectedKeys(selected)
	result := packages[:0]
	for _, pkg := range packages {
		if _, ok := allowed[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)]; ok {
			result = append(result, pkg)
		}
	}
	return result
}

func selectedKeys(packages []config.InstalledVersion) map[string]struct{} {
	keys := make(map[string]struct{}, len(packages))
	for _, pkg := range packages {
		keys[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)] = struct{}{}
	}
	return keys
}

func packageKey(scope, path, manager, name string) string {
	return scope + "\x00" + path + "\x00" + manager + "\x00" + name
}

func isCurrentGeneration(current, completed uint64) bool {
	return current == completed
}

func includesGlobal(scopes []config.Scope) bool {
	for _, scope := range scopes {
		if scope.Kind == "global" {
			return true
		}
	}
	return false
}

func selectedScopes(data *model) []config.Scope {
	if data.active != nil {
		return []config.Scope{*data.active}
	}
	return data.scopes
}
