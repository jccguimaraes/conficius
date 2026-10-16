package security

import (
	"context"
	"errors"
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
	report, err := Scan(t.Context(), inventory, Options{OSVClient: osvStub{}, EUVDClient: euvdStub{}})
	if err != nil || report.Summary.Vulnerable != 2 || report.Summary.Tools != 1 || report.Summary.GraphOnly != 1 {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	for _, item := range report.Evidence {
		if item.DependencyClass == "" || item.ScanScope != "current build only" {
			t.Fatalf("evidence = %#v", item)
		}
	}
}
