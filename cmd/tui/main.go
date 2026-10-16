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
	packageConfigPath string
	packageConfigErr  error
	dotfiles          *dotfileState
	homeCards         []homeCard
	wisdom            string
	showWisdom        bool
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
	profileOverride   string
	profiles          []string
	profileRows       []profileRow
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
	if err := runSession(data, startView); err != nil {
		return err
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
	if startView == "results" {
		refresh(data, app)
	} else {
		refreshLocal(data, app)
	}
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
	errorComponent, errorList := updateErrorView(data)
	app.View("update-error", errorComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", func() { app.Go("results") }).
		Handle("<down>", errorList.Down).
		Handle("<up>", errorList.Up)
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
		details := glyph.TextView(&data.packageDetailText).WordWrap().Grow(1).BindNav("j", "k").BindNav("<down>", "<up>").BindPageNav("<C-d>", "<C-u>")
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
	data.generation++
	clear(data.selected)
	data.plans, data.commands = nil, nil
	data.preview, data.planSummary = "", ""
	data.packages, data.rows = nil, nil
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
	ctx, cancel := context.WithCancel(context.Background())
	data.cancel = cancel
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
		if ctx.Err() != nil {
			return
		}
		apply(func() {
			if ctx.Err() != nil || !isCurrentGeneration(data.generation, generation) {
				return
			}
			data.localLoading, data.localChecked, data.localErr = false, true, inventoryErr
			data.installedCount = len(inventory.Packages)
			if !full {
				data.loading = false
			}
			data.status = appendCollisionStatus(appendManagerStatus(fmt.Sprintf("%d installed; outdated unknown; security unknown (network pending)", len(inventory.Packages)), inventory.Managers), inventory.Commands)
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
		scanPackages := inventory.Packages
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
			data.status = appendCollisionStatus(appendManagerStatus(refreshStatus(len(packages), upgraded, unchanged, failed), result.Managers), inventory.Commands)
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
