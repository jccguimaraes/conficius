package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/kungfusheep/glyph"
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
