package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
					if scenario.name == "supported" {
						if app.CurrentView() != "confirm" || len(data.plans) != 1 || data.planSummary != "1 packages in 1 commands" {
							t.Fatalf("view=%s plans=%v status=%q", app.CurrentView(), data.plans, data.status)
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
					if scenario.name == "manifest-guard" && !strings.Contains(message, "does not allow manifest changes") {
						t.Fatal(message)
					}
					if scenario.name != "manifest-guard" && !strings.Contains(message, "updates are unsupported") {
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
