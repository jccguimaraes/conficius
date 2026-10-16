package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/kungfusheep/glyph"
	"github.com/kungfusheep/riffkey"
)

func TestPackageConfigErrorBlocksUpgradeEntryPoints(t *testing.T) {
	configErr := errors.New("invalid package configuration")
	for _, entry := range []string{"marked", "all", "apply", "interactive"} {
		t.Run(entry, func(t *testing.T) {
			pkg := packagemanager.Package{Manager: "mise", Name: "node", Scope: "global", Current: "1"}
			key := packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)
			data := &model{
				packageConfigErr: configErr,
				packages:         []packagemanager.Package{pkg},
				selected:         map[string]bool{key: true},
				attempted:        map[string]string{key: "1"},
				plans:            []packagemanager.UpdatePlan{{Executable: "/must-not-run", Packages: []packagemanager.Package{pkg}}},
			}
			app := glyph.NewApp()
			app.View("results", glyph.Text("results"))
			app.View("confirm", glyph.Text("confirm"))
			app.Go("results")
			switch entry {
			case "marked", "all":
				prepareUpgrade(data, app, entry == "all")
			case "apply":
				applyUpgrades(data, app)
			case "interactive":
				if err := runInteractiveUpgrades(data); !errors.Is(err, configErr) {
					t.Fatalf("interactive error = %v", err)
				}
			}
			if !strings.Contains(data.status, "Package upgrades blocked:") || !strings.Contains(data.status, configErr.Error()) {
				t.Fatalf("status = %q", data.status)
			}
			if data.runInteractive || app.CurrentView() != "results" || !data.selected[key] || data.attempted[key] != "1" {
				t.Fatalf("blocked upgrade changed execution state: %+v", data)
			}
		})
	}
}

func TestToggleSelectedMarksCurrentPackage(t *testing.T) {
	data := &model{selected: map[string]bool{}}
	row := &outdatedRow{Package: packagemanager.Package{Manager: "mise", Name: "node", Scope: "global"}, Mark: "[ ]"}
	toggleSelected(data, row)
	if !data.selected[packageKey("global", "", "mise", "node")] || row.Mark != "[x]" {
		t.Fatal("package not selected")
	}
	toggleSelected(data, row)
	if len(data.selected) != 0 {
		t.Fatal("package not unselected")
	}
}

func TestFormatPlansShowsExactCommandAndDirectory(t *testing.T) {
	got := formatPlans([]packagemanager.UpdatePlan{{Executable: "/usr/bin/mise", Directory: "/repo", Args: []string{"upgrade", "node"}}})
	if !strings.Contains(got, "(cd /repo && /usr/bin/mise upgrade node)") {
		t.Fatalf("formatPlans = %q", got)
	}
}

func TestUpgradeKeyDispatch(t *testing.T) {
	bin, project := t.TempDir(), t.TempDir()
	for name, body := range map[string]string{
		"mise": `case "$1" in
--version) echo '2026.1.0' ;;
outdated) echo '{"node":{"current":"1","latest":"2","requested":"1"}}' ;;
*) exit 99 ;;
esac`,
		"go": `case "$1" in
version) echo 'go version go1.26.8' ;;
list) echo '{"Path":"example.com/dependency","Version":"v1.0.0","Update":{"Path":"example.com/dependency","Version":"v1.1.0"}}' ;;
*) exit 99 ;;
esac`,
		"apt": `case "$1" in
--version) echo 'apt 3.0.0' ;;
*) exit 99 ;;
esac`,
		"git":        "exit 0",
		"dpkg-query": "printf 'curl\\t1\\tinstalled\\n'",
		"apt-get":    "echo 'Inst curl [1] (2 Debian:stable [amd64])'",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/app\n\ngo 1.26.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	for _, scenario := range []struct {
		name, manager, scope string
	}{
		{"supported", "mise", "global"},
		{"go-project", "go", "project"},
		{"go-project-allowed", "go", "project"},
		{"apt-global", "apt", "global"},
		{"manifest-guard", "mise", "project"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			scope := packagemanager.Scope{Kind: scenario.scope}
			if scope.Kind == "project" {
				scope.Path = project
				if scenario.manager == "mise" {
					project := t.TempDir()
					scope.Path = project
					if err := os.WriteFile(filepath.Join(project, "mise.toml"), []byte("[tools]\nnode = '1'\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			query, err := packagemanager.QueryOutdatedScopes(context.Background(), []string{scenario.manager}, []packagemanager.Scope{scope})
			if err != nil {
				t.Fatal(err)
			}
			if scenario.name == "go-project" || scenario.name == "apt-global" {
				supported, err := packagemanager.QueryOutdatedScopes(context.Background(), []string{"mise"}, []packagemanager.Scope{{Kind: "global"}})
				if err != nil {
					t.Fatal(err)
				}
				packagemanager.MergePackages(query.Packages, supported.Packages)
			}
			for _, key := range []rune{'u', 'U'} {
				t.Run(string(key), func(t *testing.T) {
					data := &model{query: query, packages: packagemanager.Records(query.Packages), selected: map[string]bool{}}
					if scenario.name == "go-project-allowed" {
						data.profile.Profile.Projects = []config.ProfileProject{{Path: project, AllowManifestChanges: true}}
					}
					if len(data.packages) == 0 {
						t.Fatal("fake query returned no packages")
					}
					for _, pkg := range data.packages {
						data.selected[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)] = true
					}
					data.rows = outdatedRows(data.packages, data.selected, nil)
					app := glyph.NewApp()
					registerResults(data, app)
					app.View("confirm", confirmView(data))
					errorView, _ := updateErrorView(data)
					app.View("update-error", errorView)
					app.Go("results")
					if !app.Input().Dispatch(riffkey.Key{Rune: key}) {
						t.Fatal("key not dispatched")
					}
					if scenario.name == "supported" || scenario.name == "go-project-allowed" {
						if app.CurrentView() != "confirm" || len(data.plans) != 1 || data.planSummary != "1 packages in 1 commands" {
							t.Fatalf("view=%s plans=%v status=%q", app.CurrentView(), data.plans, data.status)
						}
						if scenario.name == "go-project-allowed" && (!strings.Contains(data.preview, "get example.com/dependency@v1.1.0") || !strings.Contains(data.preview, "go.mod, go.sum and transitive dependencies")) {
							t.Fatal(data.preview)
						}
						return
					}
					if app.CurrentView() != "update-error" || len(data.updateErrorLines) == 0 || !strings.Contains(data.status, "blocked") || len(data.plans) != 0 || data.runInteractive {
						t.Fatalf("view=%s status=%q details=%v plans=%v", app.CurrentView(), data.status, data.updateErrorLines, data.plans)
					}
					message := strings.Join(data.updateErrorLines, "\n")
					for _, want := range []string{"No packages were upgraded", "Space", "u"} {
						if !strings.Contains(message, want) {
							t.Errorf("error lacks actionable %q: %s", want, message)
						}
					}
					if (scenario.name == "manifest-guard" || scenario.name == "go-project") && !strings.Contains(message, "does not allow manifest changes") {
						t.Fatal(message)
					}
					if scenario.name == "apt-global" && !strings.Contains(message, "updates are unsupported") {
						t.Fatal(message)
					}
					assertRenderedContains(t, errorView, "No packages were upgraded")
					if scenario.name != "manifest-guard" {
						clear(data.selected)
						for _, pkg := range data.packages {
							if pkg.Manager == "mise" {
								data.selected[packageKey(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)] = true
							}
						}
						app.Go("results")
						if !app.Input().Dispatch(riffkey.Key{Rune: 'u'}) || app.CurrentView() != "confirm" || len(data.plans) != 1 || data.plans[0].Manager != "mise" {
							t.Fatalf("supported-only marked retry failed: view=%s plans=%v status=%q", app.CurrentView(), data.plans, data.status)
						}
					}
				})
			}
		})
	}
}

func TestGoCompilerAmbiguityBlocksUpgrade(t *testing.T) {
	pkg := packagemanager.Package{Manager: packagemanager.PM_GO, Name: "example.com/tool", Scope: "global"}
	plans := []packagemanager.UpdatePlan{
		{Manager: packagemanager.PM_GO, Executable: "/mise/shims/go", Scope: "global", Packages: []packagemanager.Package{pkg}},
		{Manager: packagemanager.PM_GO, Executable: "/other/go/bin/go", Scope: "global", Packages: []packagemanager.Package{pkg}},
	}
	if err := validateUpgradePlans(&model{}, plans); err == nil || !strings.Contains(err.Error(), "ambiguous go update") {
		t.Fatalf("different compiler plans must be blocked: %v", err)
	}
	if err := validateUpgradePlans(&model{}, plans[:1]); err != nil {
		t.Fatalf("single compiler plan blocked: %v", err)
	}
}

func dirtyUpgradeFixture(t *testing.T) (*model, *glyph.App) {
	t.Helper()
	bin, project := t.TempDir(), t.TempDir()
	for name, body := range map[string]string{
		"go": `case "$1" in
version) echo 'go version go1.26.8' ;;
list) echo '{"Path":"example.com/dependency","Version":"v1.0.0","Update":{"Path":"example.com/dependency","Version":"v1.1.0"}}' ;;
*) exit 99 ;;
esac`,
		"git": `case "$3" in
rev-parse) [ "$UNVERSIONED_GIT" != 1 ] || exit 1; printf '%s\n' "$2" ;;
status) [ "$DIRTY_GIT" != 1 ] || echo ' M go.mod' ;;
esac`,
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/app\n\ngo 1.26.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("DIRTY_GIT", "1")
	t.Setenv("UNVERSIONED_GIT", "0")
	query, err := packagemanager.QueryOutdatedScopes(context.Background(), []string{"go"}, []packagemanager.Scope{{Kind: "project", Path: project}})
	if err != nil {
		t.Fatal(err)
	}
	data := &model{query: query, packages: packagemanager.Records(query.Packages), selected: map[string]bool{}}
	data.profile.Profile.Projects = []config.ProfileProject{{Path: project, AllowManifestChanges: true}}
	app := glyph.NewApp()
	app.View("results", glyph.Text("results"))
	component, _ := updateErrorView(data)
	app.View("update-error", component)
	registerUpgradeConfirmations(data, app)
	app.Go("results")
	return data, app
}

func TestDirtyUpgradeConfirmation(t *testing.T) {
	for _, cancel := range []riffkey.Key{{Rune: 'n'}, {Special: riffkey.SpecialEscape}} {
		t.Run(cancel.String(), func(t *testing.T) {
			data, app := dirtyUpgradeFixture(t)
			prepareUpgrade(data, app, true)
			if app.CurrentView() != "dirty-confirm" || data.allowDirty || data.runInteractive {
				t.Fatalf("unsafe dirty preview: %+v", data)
			}
			if !strings.Contains(data.dirtyWarning, data.plans[0].Directory) || !strings.Contains(data.dirtyWarning, "No automatic rollback") {
				t.Fatal(data.dirtyWarning)
			}
			app.Input().Dispatch(cancel)
			if app.CurrentView() != "results" || data.allowDirty || len(data.plans) != 0 {
				t.Fatal("cancel retained batch")
			}
			prepareUpgrade(data, app, true)
			app.Input().Dispatch(riffkey.Key{Rune: 'y'})
			if app.CurrentView() != "confirm" || !data.allowDirty || data.runInteractive || !strings.Contains(data.preview, "get example.com/dependency@v1.1.0") {
				t.Fatal("approval did not open command preview")
			}
			if err := validateUpgradePlans(data, data.plans); err != nil {
				t.Fatal(err)
			}
			app.Input().Dispatch(riffkey.Key{Rune: 'y'})
			if !data.runInteractive {
				t.Fatal("approved batch blocked")
			}
		})
	}
}

func TestDirtyUpgradeStillRequiresPermissions(t *testing.T) {
	data, app := dirtyUpgradeFixture(t)
	prepareUpgrade(data, app, true)
	data.profile.Profile.Projects[0].AllowManifestChanges = false
	app.Input().Dispatch(riffkey.Key{Rune: 'y'})
	if data.allowDirty || data.runInteractive || !strings.Contains(data.status, "does not allow manifest changes") {
		t.Fatal(data.status)
	}
	data.allowDirty = true
	if err := runInteractiveUpgrades(data); err == nil || !strings.Contains(err.Error(), "does not allow manifest changes") {
		t.Fatalf("execution validation: %v", err)
	}
	data.profile.Profile.Projects[0].AllowManifestChanges = true
	t.Setenv("UNVERSIONED_GIT", "1")
	prepareUpgrade(data, app, true)
	message := strings.Join(data.updateErrorLines, "\n")
	if app.CurrentView() != "update-error" || strings.Contains(message, "--allow-") || !strings.Contains(message, "Initialize or restore Git tracking") {
		t.Fatal(message)
	}
}

func TestNewlyDirtyUpgradeRequiresConfirmation(t *testing.T) {
	data, app := dirtyUpgradeFixture(t)
	t.Setenv("DIRTY_GIT", "0")
	prepareUpgrade(data, app, true)
	if app.CurrentView() != "confirm" || data.allowDirty {
		t.Fatal("clean preview failed")
	}
	t.Setenv("DIRTY_GIT", "1")
	app.Input().Dispatch(riffkey.Key{Rune: 'y'})
	if app.CurrentView() != "dirty-confirm" || data.runInteractive || data.allowDirty {
		t.Fatal("new dirt bypassed confirmation")
	}
	if err := runInteractiveUpgrades(data); !errors.Is(err, packagemanager.ErrDirtyProject) {
		t.Fatalf("interactive validation: %v", err)
	}
	app.Input().Dispatch(riffkey.Key{Rune: 'y'})
	if app.CurrentView() != "confirm" || !data.allowDirty || data.runInteractive {
		t.Fatal("dirty approval skipped command review")
	}
}

func TestDirtyOverrideDoesNotLeak(t *testing.T) {
	for _, reset := range []string{"new-batch", "profile", "refresh", "cancel", "reset", "reload"} {
		t.Run(reset, func(t *testing.T) {
			data, app := dirtyUpgradeFixture(t)
			prepareUpgrade(data, app, true)
			app.Input().Dispatch(riffkey.Key{Rune: 'y'})
			if !data.allowDirty {
				t.Fatal("missing approval")
			}
			switch reset {
			case "new-batch":
				prepareUpgrade(data, app, true)
			case "profile":
				switchProfile(data, "other")
			case "refresh":
				data.packageConfigPath = filepath.Join(t.TempDir(), "invalid.yaml")
				if err := os.WriteFile(data.packageConfigPath, []byte("[invalid"), 0o600); err != nil {
					t.Fatal(err)
				}
				refresh(data, app)
			case "cancel":
				app.Input().Dispatch(riffkey.Key{Rune: 'n'})
			case "reset":
				resetUpgradePlans(data)
			case "reload":
				_ = reloadPackageConfig(data)
			}
			if data.allowDirty || data.runInteractive {
				t.Fatal("approval leaked across " + reset)
			}
			if reset == "new-batch" && app.CurrentView() != "dirty-confirm" {
				t.Fatal("new batch reused approval")
			}
		})
	}
}
