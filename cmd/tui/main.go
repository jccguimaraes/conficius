package main

import (
	"context"
	"errors"
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
	profile          config.ResolvedProfile
	scopes           []config.Scope
	active           *config.Scope
	packages         []packagemanager.Package
	rows             []outdatedRow
	query            packagemanager.QueryResult
	selected         map[string]bool
	failed           map[string]string
	attempted        map[string]string
	plans            []packagemanager.UpdatePlan
	preview          string
	commands         []commandRow
	planSummary      string
	updateErrorLines []string
	updating         bool
	runInteractive   bool
	drift            []config.PackageState
	findings         []vulnerabilityRow
	detail           vulnerabilityRow
	mappings         []config.VulnerabilityMapping
	euvdMaps         []config.EUVDMapping
	osvClient        vulnerability.Queryer
	euvdClient       euvd.Queryer
	status           string
	loading          bool
	cancel           context.CancelFunc
	generation       uint64
}

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	project, err := config.LoadDiscovered("")
	if err != nil && !errors.Is(err, config.ErrNotFound) {
		return err
	}
	if project == nil {
		project = &config.Project{Version: config.CurrentVersion}
	}
	profile, err := project.ResolveProfile("", false)
	if err != nil {
		return err
	}
	scopes, err := config.ResolveScopes(project, profile, false, nil, nil)
	if err != nil {
		return err
	}

	data := &model{
		profile: profile, scopes: scopes, mappings: project.Vulnerabilities.Mappings, euvdMaps: project.Vulnerabilities.EUVD,
		osvClient:  vulnerability.NewClient(http.DefaultClient, os.Getenv("CONFICIUS_OSV_URL")),
		euvdClient: euvd.NewClient(http.DefaultClient, os.Getenv("CONFICIUS_EUVD_URL")),
		status:     "Loading...", selected: make(map[string]bool), failed: make(map[string]string), attempted: make(map[string]string),
	}
	startView := "scopes"
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
	app.JumpKey("f")
	scopeComponent, scopeList := scopeView(data, app)
	app.View("scopes", scopeComponent).
		Handle("q", app.Stop).
		Handle("<Esc>", app.Stop).
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
			if row := packageList.Selected(); row != nil && row.Failed {
				data.updateErrorLines = strings.Split(row.Error, "\n")
				app.Go("update-error")
			}
		}).
		Handle("<down>", packageList.Down).
		Handle("<up>", packageList.Up).
		Handle("j", packageList.Down).
		Handle("k", packageList.Up).
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
		Handle("<C-u>", driftList.HalfPageUp)
	app.View("confirm", confirmView(data)).
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
	refresh(data, app)
	defer func() {
		if data.cancel != nil {
			data.cancel()
		}
	}()
	return app.RunFrom(startView)
}

func scopeView(data *model, app *glyph.App) (glyph.Component, *glyph.ListC[config.Scope]) {
	list := glyph.List(&data.scopes).
		Render(func(scope *config.Scope) glyph.Component {
			return glyph.Textf(&scope.Kind, ":", &scope.Name, "  ", &scope.Path)
		}).
		BindNav("j", "k").
		Handle("<Enter>", func(scope *config.Scope) {
			selectScope(data, scope)
			refresh(data, app)
			app.Go("results")
		})
	return glyph.VBox.Grow(1).Border(glyph.BorderRounded).Title("Conficius "+version)(
		glyph.Text("Profile: "+data.profile.Name).Bold(),
		glyph.Text("Scopes").Underline(),
		list,
		glyph.Text(&data.status).Dim(),
		glyph.Text("↑/↓ or j/k move  Enter open  r refresh  q quit").Dim(),
	), list
}

func resultView(data *model) (glyph.Component, *glyph.ListC[outdatedRow]) {
	list := glyph.List(&data.rows).
		Render(func(row *outdatedRow) glyph.Component {
			color := glyph.White
			if row.Failed {
				color = glyph.Red
			}
			return glyph.HBox.Gap(2)(
				glyph.Text(&row.Mark).Width(3).FG(color),
				glyph.Text(&row.Package.Manager).Width(8).FG(color),
				glyph.Text(&row.Package.Name).Width(26).FG(color),
				glyph.Text(&row.Package.Current).Width(16).FG(color),
				glyph.Text(&row.Package.Latest).Width(16).FG(func() glyph.Color {
					if row.Failed {
						return glyph.Red
					}
					return glyph.Green
				}()),
				glyph.Text(&row.Package.Scope).Width(10).FG(color),
				glyph.Text(&row.Error).Width(30).FG(glyph.Red),
			)
		}).
		Marker("› ").MarkerStyle(glyph.Style{FG: glyph.Cyan}).
		SelectedStyle(glyph.Style{BG: glyph.RGB(40, 48, 60), Attr: glyph.AttrBold}).
		MaxVisible(1000)
	header := glyph.HBox.Gap(2)(
		glyph.Text("Mark").Width(3).Bold(), glyph.Text("Manager").Width(8).Bold(), glyph.Text("Tool").Width(26).Bold(),
		glyph.Text("Current").Width(16).Bold(), glyph.Text("Latest").Width(16).Bold(), glyph.Text("Scope").Width(10).Bold(),
	)
	return glyph.VBox.Grow(1).Border(glyph.BorderRounded).Title("Outdated packages")(
		header,
		loadingContent(&data.loading, list),
		glyph.Text(&data.status).Dim(),
		glyph.Text("↑/↓ or j/k move  Enter error details  Space mark  a mark all  u upgrade marked  U upgrade all  d drift  v vulnerabilities  Esc back  r refresh  q quit").Dim(),
	), list
}

func updateErrorView(data *model) (glyph.Component, *glyph.ListC[string]) {
	list := glyph.List(&data.updateErrorLines).Render(func(line *string) glyph.Component {
		return glyph.Text(line).FG(glyph.Red)
	})
	return glyph.VBox.Grow(1).Border(glyph.BorderRounded).Title("Update error")(
		list,
		glyph.Text("↑/↓ scroll  Esc back  q quit").Dim(),
	), list
}

func confirmView(data *model) glyph.Component {
	list := glyph.List(&data.commands).Render(func(row *commandRow) glyph.Component {
		return glyph.VBox(
			glyph.Textf(&row.Manager, "  ", &row.Scope, "  ", &row.Packages),
			glyph.Text(&row.Command).FG(glyph.Cyan),
		)
	}).Marker("").MaxVisible(100)
	return glyph.VBox.Grow(1).Border(glyph.BorderRounded).Title("Confirm upgrades")(
		glyph.Text(&data.planSummary).Bold(), loadingContent(&data.updating, list),
		glyph.Text("y apply  n/Esc cancel").Dim(),
	)
}

func driftView(data *model) (glyph.Component, *glyph.ListC[config.PackageState]) {
	list := glyph.List(&data.drift).
		Render(func(row *config.PackageState) glyph.Component {
			return glyph.HBox.Gap(2)(
				glyph.Text(&row.Manager).Width(8),
				glyph.Text(&row.Name).Width(26),
				glyph.Text(&row.Installed).Width(16),
				glyph.Text(&row.DesiredVersion).Width(16).FG(glyph.Green),
				glyph.Text(&row.Status).Width(14).FG(func() glyph.Color { return driftStatusColor(row.Status) }),
				glyph.Text(&row.Scope).Width(10),
			)
		}).
		Marker("› ").MarkerStyle(glyph.Style{FG: glyph.Cyan}).
		SelectedStyle(glyph.Style{BG: glyph.RGB(40, 48, 60), Attr: glyph.AttrBold})
	header := glyph.HBox.Gap(2)(
		glyph.Text("Manager").Width(8).Bold(), glyph.Text("Tool").Width(26).Bold(),
		glyph.Text("Installed").Width(16).Bold(), glyph.Text("Desired").Width(16).Bold(),
		glyph.Text("Status").Width(14).Bold(), glyph.Text("Scope").Width(10).Bold(),
	)
	return glyph.VBox.Grow(1).Border(glyph.BorderRounded).Title("Profile drift")(
		header,
		loadingContent(&data.loading, list),
		glyph.Text(&data.status).Dim(),
		glyph.Text("↑/↓ or j/k move  Ctrl-d/u page  o outdated  v vulnerabilities  Esc back  r refresh  q quit").Dim(),
	), list
}

func vulnerabilityView(data *model) (glyph.Component, *glyph.ListC[vulnerabilityRow]) {
	list := glyph.List(&data.findings).
		Render(func(row *vulnerabilityRow) glyph.Component {
			return glyph.HBox.Gap(1)(
				glyph.Text(&row.Source).Width(6), glyph.Text(&row.Manager).Width(8), glyph.Text(&row.Package).Width(22),
				glyph.Text(&row.Current).Width(14), glyph.Text(&row.Status).Width(12), glyph.Text(&row.Score).Width(6).FG(severityColor(row.Score)), glyph.Text(&row.Advisory).Width(22).FG(severityColor(row.Score)),
				glyph.Text(&row.RuleCount).Width(5), glyph.Text(&row.UpgradeTo).Width(14).FG(glyph.Green),
			)
		}).Marker("› ").MarkerStyle(glyph.Style{FG: glyph.Cyan}).
		SelectedStyle(glyph.Style{BG: glyph.RGB(40, 48, 60), Attr: glyph.AttrBold})
	header := glyph.HBox.Gap(1)(
		glyph.Text("Source").Width(6).Bold(), glyph.Text("Manager").Width(8).Bold(), glyph.Text("Tool").Width(22).Bold(),
		glyph.Text("Current").Width(14).Bold(), glyph.Text("Status").Width(12).Bold(), glyph.Text("CVSS").Width(6).Bold(), glyph.Text("CVE / Advisory").Width(22).Bold(),
		glyph.Text("Rules").Width(5).Bold(), glyph.Text("Upgrade To").Width(14).Bold(),
	)
	return glyph.VBox.Grow(1).Border(glyph.BorderRounded).Title("Vulnerabilities")(
		header, loadingContent(&data.loading, list),
		glyph.Text(&data.status).Dim(),
		glyph.Text("↑/↓ or j/k move  Ctrl-d/u page  Enter details  o outdated  d drift  Esc back  r refresh  q quit").Dim(),
	), list
}

func vulnerabilityDetailView(data *model, app *glyph.App) glyph.Component {
	row := &data.detail
	return glyph.VBox.Grow(1).Border(glyph.BorderRounded).Title("Vulnerability detail")(
		glyph.Textf("Source: ", &row.Source),
		glyph.HBox(glyph.Text("CVE/advisory: "), glyph.Jump(glyph.Text(&row.Advisory).FG(glyph.Cyan).Underline(), func() {
			if err := openURL(row.CVELink); err != nil {
				data.status = err.Error()
				app.RequestRender()
			}
		})),
		glyph.Textf("Source ID: ", &row.SourceID),
		glyph.HBox(glyph.Text("CVSS: "), glyph.Text(&row.Score)),
		glyph.Text("Unsupported rules: "+strings.Join(row.Rules, ", ")),
		glyph.Text("Aliases: "+strings.Join(row.Aliases, ", ")),
		glyph.Textf("Upgrade target: ", &row.UpgradeTo),
		glyph.Textf("Upgrade note: ", &row.UpgradeNote),
		glyph.Textf("Description: ", &row.Description),
		glyph.Textf("Reference: ", &row.Reference),
		glyph.Textf("Reason/error: ", &row.Reason),
		glyph.Text(&data.status).FG(glyph.Red),
		glyph.Space().Grow(1),
		glyph.Text("f then link label open CVE  Esc back  q quit").Dim(),
	)
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
	if data.cancel != nil {
		data.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	data.cancel = cancel
	data.generation++
	generation := data.generation
	data.loading = true
	data.status = ""
	scopeSelection := selectedScopes(data)
	profile := data.profile.Profile
	mappings := append([]config.VulnerabilityMapping(nil), data.mappings...)
	euvdMaps := append([]config.EUVDMapping(nil), data.euvdMaps...)
	app.RequestRender()
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
		go func() {
			result, err := packagemanager.QueryOutdatedScopesWithOptions(ctx, nil, scopes, options)
			outdatedCh <- outdatedResult{result, err}
		}()
		go func() {
			result, err := packagemanager.QueryInstalledScopesWithOptions(ctx, nil, scopes, options)
			inventoryCh <- inventoryResult{result, err}
		}()
		outdatedData, inventoryData := <-outdatedCh, <-inventoryCh
		result, outdatedErr := outdatedData.result, outdatedData.err
		inventory, inventoryErr := inventoryData.result, inventoryData.err
		if includesGlobal(scopeSelection) {
			managerPackages, managerErr := packagemanager.ManagerPackages(ctx, inventory.Managers)
			inventory.Packages = append(inventory.Packages, managerPackages...)
			managerUpdates, managerUpdateErr := packagemanager.ManagerUpdates(ctx, inventory.Managers, managerPackages)
			packagemanager.MergePackages(result.Packages, managerUpdates)
			inventoryErr = errors.Join(inventoryErr, managerErr, managerUpdateErr)
		}
		securityTargets := packagemanager.Records(result.Packages)
		packages := append([]packagemanager.Package(nil), securityTargets...)
		installed := make([]config.InstalledVersion, len(inventory.Packages))
		for i, pkg := range inventory.Packages {
			installed[i] = config.InstalledVersion{Manager: pkg.Manager, Name: pkg.Name, Scope: pkg.Scope, Path: pkg.Path, Version: pkg.Version}
		}
		selected, drift := config.EvaluatePackages(profile, installed)
		packages = filterOutdated(packages, selected)
		scanPackages := inventory.Packages
		if profile.SecurityScope == "selected" {
			scanPackages = filterInstalled(inventory.Packages, selected)
		}
		if ctx.Err() != nil {
			return
		}
		err := errors.Join(outdatedErr, inventoryErr)
		app.Apply(func() {
			if !isCurrentGeneration(data.generation, generation) {
				return
			}
			data.loading = false
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
		})
		report, scanErr := security.Scan(ctx, scanPackages, security.Options{
			Outdated: securityTargets, OSVMappings: mappings, EUVDMappings: euvdMaps,
			OSVClient: data.osvClient, EUVDClient: data.euvdClient,
		})
		if ctx.Err() != nil {
			return
		}
		app.Apply(func() {
			if !isCurrentGeneration(data.generation, generation) {
				return
			}
			data.findings = vulnerabilityRows(report.Evidence)
			data.status = vulnerabilityStatus(vulnerabilitySummary(len(packages), len(drift), report.Summary, report.Evidence), err, scanErr)
			if len(data.failed) > 0 {
				data.status += fmt.Sprintf("; %d failed updates", len(data.failed))
			}
			data.status = appendCollisionStatus(appendManagerStatus(data.status, result.Managers), inventory.Commands)
		})
	}()
}

func loadingContent(loading *bool, content glyph.Component) glyph.Component {
	return glyph.VBox.Grow(1)(glyph.If(loading).
		Then(glyph.HBox.Gap(1)(glyph.Spinner().FG(glyph.Cyan), glyph.Text("Loading...").FG(glyph.Cyan))).
		Else(content))
}

func severityColor(score float64) glyph.Color {
	switch {
	case score >= 9:
		return glyph.Red
	case score >= 7:
		return glyph.RGB(255, 140, 0)
	case score >= 4:
		return glyph.Yellow
	case score > 0:
		return glyph.Blue
	default:
		return glyph.White
	}
}

func driftStatusColor(status string) glyph.Color {
	switch status {
	case config.DriftBehind, config.DriftMissing:
		return glyph.Red
	case config.DriftUnsupported:
		return glyph.Yellow
	default:
		return glyph.Green
	}
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
