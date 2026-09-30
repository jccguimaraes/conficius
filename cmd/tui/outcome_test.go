package main

import (
	"errors"
	"testing"

	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

func TestReconcileUpgradeOutcomes(t *testing.T) {
	upgradedKey := packageKey("global", "", "brew", "done")
	unchangedKey := packageKey("global", "", "mise", "same")
	failedKey := packageKey("global", "", "brew", "failed")
	data := &model{
		attempted: map[string]string{upgradedKey: "1", unchangedKey: "1", failedKey: "1"},
		failed:    map[string]string{failedKey: "installer failed"}, selected: map[string]bool{},
	}
	packages := []packagemanager.Package{{Manager: "mise", Name: "same", Current: "1", Scope: "global"}, {Manager: "brew", Name: "failed", Current: "1", Scope: "global"}}
	upgraded, unchanged, failed := reconcileUpgradeOutcomes(data, packages)
	if upgraded != 1 || unchanged != 1 || failed != 1 {
		t.Fatalf("counts = %d, %d, %d", upgraded, unchanged, failed)
	}
	if data.failed[unchangedKey] != "No version change after command completed" || !data.selected[unchangedKey] {
		t.Fatalf("data = %#v", data)
	}
	if len(data.attempted) != 0 {
		t.Fatalf("attempted = %#v", data.attempted)
	}
}

func TestVulnerabilityStatusIncludesScanFailure(t *testing.T) {
	got := vulnerabilityStatus("1 vulnerable", nil, errors.New("OSV scan: timeout"))
	want := "1 vulnerable; vulnerability scan failed: OSV scan: timeout"
	if got != want {
		t.Fatalf("status = %q; want %q", got, want)
	}
}

func TestVulnerabilityStatusKeepsInspectionFailure(t *testing.T) {
	got := vulnerabilityStatus("1 vulnerable", errors.New("mise: unavailable"), errors.New("OSV scan: timeout"))
	want := "1 vulnerable; Package inspection failed. Run `conficius packages doctor`.; vulnerability scan failed: OSV scan: timeout"
	if got != want {
		t.Fatalf("status = %q; want %q", got, want)
	}
}

func TestRefreshStatusIncludesUpgradeOutcome(t *testing.T) {
	got := refreshStatus(2, 1, 3, 1, 1)
	want := "3 upgraded, 1 unchanged, 1 failed; 2 outdated; scanning vulnerabilities..."
	if got != want {
		t.Fatalf("status = %q", got)
	}
}
