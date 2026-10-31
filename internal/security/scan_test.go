package security

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/jccguimaraes/conficius/internal/euvd"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/jccguimaraes/conficius/internal/vulnerability"
)

type osvStub struct{}

func (osvStub) Query(context.Context, vulnerability.Package) ([]vulnerability.Vulnerability, error) {
	return []vulnerability.Vulnerability{{ID: "GHSA-1", Aliases: []string{"CVE-1"}, Summary: "OSV summary", References: []vulnerability.Reference{{URL: "https://osv.dev/GHSA-1"}}, Severity: []vulnerability.Severity{{Type: "CVSS_V3", Score: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}}}}, nil
}

type euvdStub struct{}

func (euvdStub) Query(context.Context, euvd.Identity, string) ([]euvd.Vulnerability, error) {
	return []euvd.Vulnerability{{ID: "EUVD-1", Aliases: "CVE-1\n", BaseScore: 8.1, Products: []euvd.ProductEntry{product("Node", "nodejs", "22.10.0")}}}, nil
}

func TestMiseRequestedInventorySelection(t *testing.T) {
	for _, version := range []string{"1.2.3", "0.12.0", "2026.10.31.123"} {
		pkg := packagemanager.Package{Manager: "mise", Name: "node", Requested: version, Scope: "project", Path: "/project"}
		options := Options{Outdated: []packagemanager.Package{pkg, pkg}}
		selected := SelectInventory(nil, options)
		if len(selected) != 1 || selected[0].Version != version {
			t.Errorf("%s selected = %+v", version, selected)
			continue
		}
		if again := SelectInventory(selected, options); len(again) != 1 {
			t.Errorf("not idempotent: %+v", again)
		}
		installed := []packagemanager.InstalledPackage{{Manager: pkg.Manager, Name: pkg.Name, Scope: pkg.Scope, Path: pkg.Path, Version: "2.0.0"}}
		if got := SelectInventory(installed, options); len(got) != 1 || got[0].Version != "2.0.0" || installed[0].Version != "2.0.0" {
			t.Errorf("installed must win: %+v", got)
		}
		installed[0].Path = "/other"
		if got := SelectInventory(installed, options); len(got) != 2 {
			t.Errorf("scope identity lost: %+v", got)
		}
	}
	for _, version := range []string{"", "latest", "24", "0.12", "22.10", "^1", "~1.2.3", ">=1.2.3", "1.x", "{{version}}", "npm:node@1.2.3", "1.2.3-rc.1", "1.2.3+build.2", "1.2.3-rc.1+build.2", "1.2.3-", "1.2.3+", "1.2.3-rc..1"} {
		client := &countingOSV{}
		report, err := Scan(t.Context(), nil, Options{Outdated: []packagemanager.Package{{Manager: "mise", Name: "node", Requested: version}}, OSVClient: client, EUVDClient: euvdStub{}})
		if err != nil || client.calls.Load() != 0 || len(report.Evidence) != 0 {
			t.Errorf("unsupported requested version %q assessed: %+v, %v", version, report, err)
		}
	}
	client := &countingOSV{}
	report, err := Scan(t.Context(), nil, Options{Outdated: []packagemanager.Package{{Manager: "mise", Name: "unmapped-tool", Requested: "1.2.3"}}, OSVClient: client, EUVDClient: euvdStub{}})
	if err != nil || client.calls.Load() != 0 || len(report.Evidence) != 2 {
		t.Fatalf("unmapped tool: %+v, %v", report, err)
	}
	for _, item := range report.Evidence {
		if item.Status != StatusUnknown && item.Status != StatusUnmapped {
			t.Fatal(item)
		}
	}
}

func TestScanCombinesSourcesWithoutDeduplicating(t *testing.T) {
	inventory := []packagemanager.InstalledPackage{{Manager: "npm", Name: "node", Version: "22.10.0"}}
	report, err := Scan(context.Background(), inventory, Options{OSVClient: osvStub{}, EUVDClient: euvdStub{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Evidence) != 2 || report.Summary.Vulnerable != 2 {
		t.Fatalf("report = %#v", report)
	}
	if report.Evidence[0].Source == report.Evidence[1].Source {
		t.Fatalf("sources = %#v", report.Evidence)
	}
	for _, item := range report.Evidence {
		if item.Source == SourceOSV && (item.Description != "OSV summary" || item.Reference != "https://osv.dev/GHSA-1" || item.CVELink != "https://www.cve.org/CVERecord?id=CVE-1") {
			t.Fatalf("OSV evidence = %#v", item)
		}
		if item.Source == SourceEUVD && item.CVELink != "https://www.cve.org/CVERecord?id=CVE-1" {
			t.Fatalf("EUVD evidence = %#v", item)
		}
	}
}

type failingOSVStub struct{}

func (failingOSVStub) Query(context.Context, vulnerability.Package) ([]vulnerability.Vulnerability, error) {
	return nil, errors.New("offline")
}

func TestScanReturnsEvidenceAndSourceError(t *testing.T) {
	inventory := []packagemanager.InstalledPackage{{Manager: "npm", Name: "node", Version: "22.10.0"}}
	report, err := Scan(context.Background(), inventory, Options{OSVClient: failingOSVStub{}, EUVDClient: euvdStub{}})
	if err == nil || report.Summary.Errors != 1 || report.Summary.Vulnerable != 1 {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
}

func product(name, vendor, version string) euvd.ProductEntry {
	var entry euvd.ProductEntry
	entry.Product.Name = name
	entry.Product.Vendor.Name = vendor
	entry.ProductVersion = version
	return entry
}

func TestGoEvidenceProvenanceAndSeparateCounts(t *testing.T) {
	var inventory []packagemanager.InstalledPackage
	for _, class := range []string{"application", "test", "tool", "graph-only"} {
		inventory = append(inventory, packagemanager.InstalledPackage{Manager: "go", Name: "example.com/" + class, Version: "v1.0.0", OSVEcosystem: "Go", OSVName: "example.com/" + class, DependencyClass: class, ScanScope: "current build only"})
	}
	report, err := Scan(t.Context(), inventory, Options{IncludeGraphOnly: true, OSVClient: osvStub{}, EUVDClient: euvdStub{}})
	if err != nil || report.Summary.Vulnerable != 2 || report.Summary.Tools != 1 || report.Summary.GraphOnly != 1 {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	for _, item := range report.Evidence {
		if item.DependencyClass == "" || item.ScanScope != "current build only" {
			t.Fatalf("evidence = %#v", item)
		}
	}
}

type countingOSV struct{ calls atomic.Int32 }

func (c *countingOSV) Query(context.Context, vulnerability.Package) ([]vulnerability.Vulnerability, error) {
	c.calls.Add(1)
	return nil, nil
}

func TestGoScanSelectionQueriesOnlySelectedInventory(t *testing.T) {
	inventory := []packagemanager.InstalledPackage{
		{Manager: "go", Name: "example.com/direct", DependencyClass: "application"},
		{Manager: "go", Name: "example.com/indirect", DependencyClass: "application", Transitive: true},
		{Manager: "go", Name: "example.com/test", DependencyClass: "test", Transitive: true},
		{Manager: "go", Name: "example.com/tool", DependencyClass: "tool", Transitive: true, DeclaredTool: true},
		{Manager: "go", Name: "example.com/nested-tool", DependencyClass: "tool", Transitive: true},
		{Manager: "go", Name: "example.com/graph", DependencyClass: "graph-only", Transitive: true},
	}
	for i := range inventory {
		inventory[i].Version = "v1.0.0"
		inventory[i].OSVEcosystem = "Go"
		inventory[i].OSVName = inventory[i].Name
	}
	for _, tc := range []struct {
		name            string
		indirect, graph bool
		want            int
	}{
		{"default", false, false, 2}, {"indirect", true, false, 5}, {"graph", false, true, 3}, {"both", true, true, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &countingOSV{}
			report, err := Scan(t.Context(), inventory, Options{IncludeIndirect: tc.indirect, IncludeGraphOnly: tc.graph, OSVClient: client, EUVDClient: euvdStub{}})
			if err != nil || int(client.calls.Load()) != tc.want {
				t.Fatalf("requests = %d, err = %v", client.calls.Load(), err)
			}
			clean := 0
			for _, item := range report.Evidence {
				if item.Status == StatusClean {
					clean++
				}
			}
			if clean != tc.want {
				t.Fatalf("clean = %d; skipped must not be clean", clean)
			}
			for _, item := range report.Evidence {
				if item.Package == "example.com/tool" && (!item.Transitive || !item.DeclaredTool) {
					t.Fatal(item)
				}
			}
		})
	}
	npm := []packagemanager.InstalledPackage{{Manager: "npm", Transitive: true}}
	if len(SelectInventory(npm, Options{})) != 1 {
		t.Fatal("non-Go behavior changed")
	}
}
