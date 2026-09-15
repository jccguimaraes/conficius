package config

import "testing"

func TestEvaluatePackagesAppliesAllowlistAndDrift(t *testing.T) {
	profile := Profile{Mode: "allowlist", Packages: []PackageRule{
		{Manager: "npm", Name: "typescript", Policy: "include", DesiredVersion: "5.9.2"},
		{Manager: "npm", Name: "eslint", Policy: "include", DesiredVersion: "9.0.0"},
	}}
	installed := []InstalledVersion{
		{Manager: "npm", Name: "typescript", Scope: "global", Version: "5.8.0"},
		{Manager: "npm", Name: "prettier", Scope: "global", Version: "3.0.0"},
	}
	selected, states := EvaluatePackages(profile, installed)
	if len(selected) != 1 || selected[0].Name != "typescript" {
		t.Fatalf("selected = %#v", selected)
	}
	if len(states) != 2 || states[0].Status != DriftBehind || states[1].Status != DriftMissing {
		t.Fatalf("states = %#v", states)
	}
}

func TestEvaluatePackagesAppliesDenylist(t *testing.T) {
	profile := Profile{Mode: "denylist", Packages: []PackageRule{{Manager: "npm", Name: "skip", Policy: "exclude"}}}
	selected, states := EvaluatePackages(profile, []InstalledVersion{
		{Manager: "npm", Name: "keep", Scope: "global", Version: "1"},
		{Manager: "npm", Name: "skip", Scope: "global", Version: "1"},
	})
	if len(selected) != 1 || selected[0].Name != "keep" || len(states) != 0 {
		t.Fatalf("selected = %#v, states = %#v", selected, states)
	}
}

func TestEvaluatePackagesReportsMismatchAndUnsupported(t *testing.T) {
	profile := Profile{Mode: "allowlist", Packages: []PackageRule{
		{Manager: "npm", Name: "tool", Policy: "include", DesiredVersion: "1"},
		{Manager: "brew", Name: "wget", Policy: "include", DesiredVersion: "2"},
	}}
	_, states := EvaluatePackages(profile, []InstalledVersion{
		{Manager: "npm", Name: "tool", Scope: "global", Version: "2"},
		{Manager: "brew", Name: "wget", Scope: "global", Version: "1"},
	})
	if states[0].Status != DriftBehind || states[1].Status != DriftUnsupported {
		t.Fatalf("states = %#v", states)
	}
}
