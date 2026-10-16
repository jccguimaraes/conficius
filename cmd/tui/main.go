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
	"sort"
	"strings"

	glyph "github.com/kungfusheep/glyph"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/euvd"
	"github.com/jccguimaraes/conficius/internal/layout"
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
	packageConfigPath  string
	packageConfigErr   error
	dotfiles           *dotfileState
	homeCards          []homeCard
	wisdom             string
	showWisdom         bool
	localLoading       bool
	localChecked       bool
	localErr           error
	installedCount     int
	networkPending     bool
	outdatedChecked    bool
	outdatedErr        error
	securityCancel     context.CancelFunc
	securityGeneration uint64
	securityScanning   bool
	securityChecked    bool
	securityErr        error
	securitySummary    security.Summary
	securityCount      int
	dotfileGeneration  uint64
	dotfileCancel      context.CancelFunc
	profile            config.ResolvedProfile
	profileOverride    string
	profiles           []string
	profileRows        []profileRow
	scopes             []config.Scope
	active             *config.Scope
	packages           []packagemanager.Package
	rows               []outdatedRow
	query              packagemanager.QueryResult
	selected           map[string]bool
	failed             map[string]string
	attempted          map[string]string
	plans              []packagemanager.UpdatePlan
	allowDirty         bool
	dirtyWarning       string
	preview            string
	commands           []commandRow
	planSummary        string
	packageDetailText  string
	packageFocus       func(func())
	updateErrorLines   []string
	updating           bool
	sessionStarting    bool
	runManagerRefresh  bool
	runInteractive     bool
	runCacheRefresh    bool
	cacheExecutable    string
	cacheArgs          []string
	cachePreview       string
	cacheReturnView    string
	cacheRefreshDone   bool
	cacheRefreshErr    string
	allEvidence        []security.Evidence
	includeGraphOnly   bool
	includeIndirect    bool
	inventory          []packagemanager.InstalledPackage
	findings           []vulnerabilityRow
	detail             vulnerabilityRow
	mappings           []config.VulnerabilityMapping
	euvdMaps           []config.EUVDMapping
	osvClient          vulnerability.Queryer
	euvdClient         euvd.Queryer
	status             string
	loading            bool
	cancel             context.CancelFunc
	generation         uint64
}

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	if handled, err := packagemanager.HandleManagerWorker(os.Args[1:], os.Stdout); handled {
		return err
	}
	configFlag, packageFlag, err := parseConfigFlags(os.Args[1:], os.Stdout)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	data, err := startupModel(configFlag, packageFlag)
	if err != nil {
		return err
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
	{ // Always seal worker launches before glyph, even if config changes later.
		scopes := selectedScopes(data)
		managerScopes := make([]packagemanager.Scope, len(scopes))
		for i, scope := range scopes {
			managerScopes[i] = packagemanager.Scope{Kind: scope.Kind, Path: scope.Path}
		}
		if err := packagemanager.PrepareRunAsQueries(context.Background(), managerScopes, packagemanager.QueryOptionsFromProfile(data.profile.Profile)); err != nil {
			data.status = err.Error()
		}
	}
	if err := runSession(data, startView); err != nil {
		return err
	}
	if data.runManagerRefresh {
		tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err != nil {
			return err
		}
		defer func() { _ = tty.Close() }()
		return execFreshTUI(tty, data)
	}
	if data.runCacheRefresh {
		return runInteractiveCacheRefresh(data)
	}
	if !data.runInteractive {
		return nil
	}
	return runInteractiveUpgrades(data)
}

func startupModel(configFlag, packageFlag string) (*model, error) {
	path, discoverErr := config.Discover(configFlag)
	dotfileProject, configErr := config.LoadDiscovered(configFlag)
	dotfiles := newDotfileState(dotfileProject, errors.Join(discoverErr, configErr), path)
	dotfiles.packageConfigPath = packageFlag
	data := &model{
		dotfiles: dotfiles, packageConfigPath: packageFlag, networkPending: true,
		osvClient:  vulnerability.NewClient(http.DefaultClient, os.Getenv("CONFICIUS_OSV_URL")),
		euvdClient: euvd.NewClient(http.DefaultClient, os.Getenv("CONFICIUS_EUVD_URL")),
		status:     "Loading...", selected: make(map[string]bool), failed: make(map[string]string), attempted: make(map[string]string),
	}
	_ = reloadPackageConfig(data)
	return data, nil
}

func reloadPackageConfig(data *model) (err error) {
	resetUpgradePlans(data)
	defer func() { data.packageConfigErr = err }()
	project, err := config.LoadPackages(data.packageConfigPath)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(project.Profiles)+1)
	for name := range project.Profiles {
		names = append(names, name)
	}
	if _, exists := project.Profiles["default"]; !exists {
		names = append(names, "default")
	}
	sort.Strings(names)
	data.profiles = names
	data.profileRows = nil
	for _, name := range names {
		data.profileRows = append(data.profileRows, profileRow{Name: name})
	}
	profile, err := project.ResolveProfile(data.profileOverride, false)
	if err != nil {
		return err
	}
	scopes, err := config.ResolveScopes(project, profile, false, nil, nil)
	if err != nil {
		return err
	}
	var active *config.Scope
	if data.active != nil {
		for i := range scopes {
			if scopes[i].Kind == data.active.Kind && scopes[i].Path == data.active.Path {
				active = &scopes[i]
				break
			}
		}
	}
	data.profile, data.scopes, data.active = profile, scopes, active
	data.profileRows = nil
	for _, name := range names {
		data.profileRows = append(data.profileRows, profileRow{Name: name, Active: name == profile.Name})
	}
	data.mappings, data.euvdMaps = project.Vulnerabilities.Mappings, project.Vulnerabilities.EUVD
	if data.dotfiles != nil {
		data.dotfiles.mode = layout.Mode(project.Layout)
	}
	return nil
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
	data.sessionStarting = true
	if fullRefreshOnStart(data, startView) {
		refresh(data, app)
	} else {
		refreshLocal(data, app)
	}
	data.sessionStarting = false
	if data.dotfiles != nil {
		registerDotfiles(data.dotfiles, app)
	}
	registerHome(data, app)
	profileComponent, profileList := profileView(data, app)
	app.View("profiles", profileComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { data.active = nil; app.Go("home") }).
		Handle("<down>", profileList.Down).
		Handle("<up>", profileList.Up).
		Handle("r", func() { refreshLocal(data, app) })
	scopeComponent, scopeList := scopeView(data, app)
	app.View("scopes", scopeComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { data.active = nil; updateHome(data); app.Go("home") }).
		Handle("p", func() { app.Go("profiles") }).
		Handle("<down>", scopeList.Down).
		Handle("<up>", scopeList.Up).
		Handle("r", func() { refresh(data, app) })
	packageList := registerResults(data, app)
	errorComponent, errorList := updateErrorView(data)
	app.View("update-error", errorComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("results") }).
		Handle("<down>", errorList.Down).
		Handle("<up>", errorList.Up)
	registerUpgradeConfirmations(data, app)
	vulnerabilityComponent, vulnerabilityList := vulnerabilityView(data)
	app.View("vulnerabilities", vulnerabilityComponent).
		Handle("x", func() { cancelSecurity(data, app) }).
		Handle("g", func() { toggleGraphOnly(data, func() { refreshSecurity(data, app) }) }).
		Handle("i", func() { toggleIndirect(data, func() { refreshSecurity(data, app) }) }).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("results") }).
		Handle("o", func() { app.Go("results") }).
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
	for name, back := range map[string]string{"package-detail": "results"} {
		details := detailTextView(&data.packageDetailText, true)
		app.View(name, screenFrame("Packages / Details", "Profile: "+data.profile.Name, &data.status, details, "↑/↓ scroll  Esc back  q quit")).
			Handle("<Esc>", func() { app.Go(back) }).Handle("q", app.Stop)
	}
	keepPackages := preserveListFocus(packageList, &data.rows, func(row *outdatedRow) string {
		return packageKey(row.Package.Scope, row.Package.Path, row.Package.Manager, row.Package.Name)
	})
	keepFindings := preserveListFocus(vulnerabilityList, &data.findings, func(row *vulnerabilityRow) string {
		return packageKey(row.Scope, row.Path, row.Manager, row.Package) + "\x00" + row.Source + "\x00" + row.SourceID
	})
	data.packageFocus = func(update func()) { keepPackages(func() { keepFindings(update) }) }
	defer func() { data.packageFocus = nil }()
	registerCacheRefresh(data, app)
	registerScreenHelp(app, data)
	startView, _ = registerResizeView(app, data, startView)
	defer func() {
		if data.securityCancel != nil {
			data.securityCancel()
		}
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
	if data.securityCancel != nil {
		data.securityCancel()
		data.securityCancel = nil
	}
	data.securityGeneration++
	if data.cancel != nil {
		data.cancel()
	}
	data.generation++
	clear(data.selected)
	resetUpgradePlans(data)
	data.packages, data.rows = nil, nil
	data.allEvidence = nil
	data.inventory = nil
	data.findings, data.detail = nil, vulnerabilityRow{}
	data.packageDetailText = ""
	data.securitySummary, data.securityCount = security.Summary{}, 0
	data.query = packagemanager.QueryResult{}
	data.installedCount = 0
	data.localChecked, data.outdatedChecked, data.securityChecked = false, false, false
	data.localErr, data.outdatedErr, data.securityErr = nil, nil, nil
	hadActive := data.active != nil
	if err := reloadPackageConfig(data); err != nil {
		data.loading, data.localLoading, data.securityScanning = false, false, false
		data.status = fmt.Sprintf("Package configuration error: %v", err)
		updateHome(data)
		app.RequestRender()
		return
	}
	if hadActive && data.active == nil {
		data.loading, data.localLoading, data.securityScanning, data.networkPending = false, false, false, false
		data.status = "Selected scope removed from package configuration; choose a scope to refresh"
		updateHome(data)
		app.Go("scopes")
		app.RequestRender()
		return
	}
	if !data.sessionStarting && packagemanager.HasRunAs(packagemanager.QueryOptionsFromProfile(data.profile.Profile)) {
		data.runManagerRefresh = true
		app.Stop()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	data.cancel = cancel
	securityCtx, securityCancel := context.WithCancel(ctx)
	data.securityCancel = securityCancel
	securityGeneration := data.securityGeneration
	generation := data.generation
	data.loading = true
	data.localLoading, data.localChecked, data.localErr = true, false, nil
	data.networkPending, data.outdatedChecked, data.outdatedErr = true, false, nil
	data.securityScanning, data.securityChecked, data.securityErr = false, false, nil
	data.status = "Busy: loading dependency graph; versions and security pending"
	updateHome(data)
	scopeSelection := selectedScopes(data)
	profile := data.profile.Profile
	mappings := append([]config.VulnerabilityMapping(nil), data.mappings...)
	euvdMaps := append([]config.EUVDMapping(nil), data.euvdMaps...)
	scanOptions := security.Options{IncludeIndirect: data.includeIndirect, IncludeGraphOnly: data.includeGraphOnly, OSVMappings: mappings, EUVDMappings: euvdMaps, OSVClient: data.osvClient, EUVDClient: data.euvdClient}
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
		if ctx.Err() != nil {
			return
		}
		apply(func() {
			if ctx.Err() != nil || !isCurrentGeneration(data.generation, generation) {
				return
			}
			data.localLoading, data.localChecked, data.localErr = false, true, inventoryErr
			data.installedCount = len(inventory.Packages)
			data.inventory = inventory.Packages
			if !full {
				cancel()
				data.securityCancel = nil
				data.loading = false
			}
			if full {
				data.status = fmt.Sprintf("Busy: checking installed versions; %d installed; security pending", len(inventory.Packages))
			} else {
				data.status = fmt.Sprintf("%d installed; outdated and security pending network refresh", len(inventory.Packages))
			}
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
		packages := append([]packagemanager.Package(nil), securityTargets...)
		scanPackages := security.SelectInventory(inventory.Packages, scanOptions)
		if ctx.Err() != nil {
			return
		}
		err := errors.Join(outdatedErr, inventoryErr)
		apply(func() {
			if ctx.Err() != nil || !isCurrentGeneration(data.generation, generation) {
				return
			}
			data.loading = true
			data.networkPending, data.outdatedChecked, data.outdatedErr = false, true, outdatedErr
			if !isCurrentGeneration(data.securityGeneration, securityGeneration) || securityCtx.Err() != nil {
				return
			}
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
			data.status = appendCollisionStatus(appendManagerStatus(refreshStatus(len(packages), upgraded, unchanged, failed), result.Managers), inventory.Commands)
			if err != nil {
				data.status = queryStatus(err)
			}
			data.status = fmt.Sprintf("Busy: checking security; %d selected / %d installed; %d outdated", len(scanPackages), len(inventory.Packages), len(packages))
			updateHome(data)
		})
		scanOptions.Outdated = securityTargets
		report, scanErr := security.Scan(securityCtx, scanPackages, scanOptions)
		if securityCtx.Err() != nil {
			cancel()
			return
		}
		apply(func() {
			defer cancel()
			if ctx.Err() != nil || !isCurrentGeneration(data.generation, generation) {
				return
			}
			if securityCtx.Err() != nil || !isCurrentGeneration(data.securityGeneration, securityGeneration) {
				return
			}
			data.securityCancel = nil
			data.loading = false
			data.securityScanning, data.securityChecked, data.securityErr = false, true, errors.Join(inventoryErr, scanErr)
			data.securitySummary, data.securityCount = report.Summary, len(scanPackages)
			data.allEvidence = report.Evidence
			data.findings = vulnerabilityRows(report.Evidence, data.includeGraphOnly)
			data.status = vulnerabilityStatus(vulnerabilitySummary(len(packages), report.Summary, report.Evidence), err, scanErr)
			if len(data.failed) > 0 {
				data.status += fmt.Sprintf("; %d failed updates", len(data.failed))
			}
			data.status = appendCollisionStatus(appendManagerStatus(data.status, result.Managers), inventory.Commands)
			updateHome(data)
		})
	}()
}

func vulnerabilitySummary(outdated int, summary security.Summary, evidence []security.Evidence) string {
	unknown := 0
	for _, item := range evidence {
		if item.Status == security.StatusUnknown && item.Advisory != "" && len(item.Rules) > 0 {
			unknown++
		}
	}
	return fmt.Sprintf("%d outdated; %d vulnerable, %d unknown, %d security errors", outdated,
		summary.Vulnerable, unknown, summary.Errors) + fmt.Sprintf("; %d tool, %d graph-only (g toggles)", summary.Tools, summary.GraphOnly)
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

func toggleGraphOnly(data *model, rescan func()) {
	data.includeGraphOnly = !data.includeGraphOnly
	rescan()
}

func toggleIndirect(data *model, rescan func()) {
	data.includeIndirect = !data.includeIndirect
	rescan()
}

// Scope toggles reuse inventory and freshness results; only security work restarts.
func refreshSecurity(data *model, app *glyph.App) {
	if !data.localChecked || !data.outdatedChecked {
		refresh(data, app)
		return
	}
	if data.securityCancel != nil {
		data.securityCancel()
	}
	data.securityGeneration++
	generation := data.generation
	securityGeneration := data.securityGeneration
	ctx, cancel := context.WithCancel(context.Background())
	data.securityCancel = cancel
	options := security.Options{IncludeIndirect: data.includeIndirect, IncludeGraphOnly: data.includeGraphOnly,
		Outdated: data.packages, OSVMappings: data.mappings, EUVDMappings: data.euvdMaps,
		OSVClient: data.osvClient, EUVDClient: data.euvdClient}
	inventory := security.SelectInventory(data.inventory, options)
	data.loading, data.securityScanning, data.securityChecked = true, true, false
	data.securityErr = nil
	data.status = fmt.Sprintf("Busy: checking security; %d selected / %d installed; expanded scans take time", len(inventory), data.installedCount)
	updateHome(data)
	app.RequestRender()
	go func() {
		report, err := security.Scan(ctx, inventory, options)
		app.Apply(func() {
			defer cancel()
			if ctx.Err() != nil || !isCurrentGeneration(data.generation, generation) {
				return
			}
			if !isCurrentGeneration(data.securityGeneration, securityGeneration) {
				return
			}
			data.securityCancel = nil
			data.loading, data.securityScanning, data.securityChecked = false, false, true
			data.securityErr = errors.Join(data.localErr, err)
			data.securitySummary, data.securityCount = report.Summary, len(inventory)
			data.allEvidence = report.Evidence
			data.findings = vulnerabilityRows(report.Evidence, options.IncludeGraphOnly)
			data.status = vulnerabilityStatus(vulnerabilitySummary(len(data.packages), report.Summary, report.Evidence), errors.Join(data.localErr, data.outdatedErr), err)
			updateHome(data)
		})
	}()
}

func cancelSecurity(data *model, app *glyph.App) {
	if !data.securityScanning || data.securityCancel == nil {
		if !errors.Is(data.securityErr, context.Canceled) {
			data.status = "No vulnerability scan running"
		}
		app.RequestRender()
		return
	}
	data.securityCancel()
	data.securityCancel = nil
	data.securityGeneration++
	data.securityScanning, data.securityChecked = false, false
	data.securityErr = context.Canceled
	data.loading = data.localLoading || data.networkPending
	data.status = "Vulnerability scan canceled; results incomplete"
	updateHome(data)
	app.RequestRender()
}

func vulnerabilityOrigin(item security.Evidence) string {
	if item.DeclaredTool {
		return "declared tool"
	}
	if item.Transitive {
		return "indirect"
	}
	return "direct"
}

func vulnerabilityRows(evidence []security.Evidence, includeGraphOnly ...bool) []vulnerabilityRow {
	rows := make([]vulnerabilityRow, 0, len(evidence))
	for _, item := range evidence {
		if item.DependencyClass == "graph-only" && (len(includeGraphOnly) == 0 || !includeGraphOnly[0]) {
			continue
		}
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
