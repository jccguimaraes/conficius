package euvd

import (
	"context"
	"errors"
	"testing"

	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

type pathQueryStub struct {
	items []Vulnerability
	err   error
}

func (client pathQueryStub) Query(context.Context, Identity, string) ([]Vulnerability, error) {
	return client.items, client.err
}

func TestScanPreservesInventoryIdentity(t *testing.T) {
	for _, tc := range []struct {
		status string
		name   string
		client pathQueryStub
	}{
		{StatusUnmapped, "unmapped-tool", pathQueryStub{}},
		{StatusClean, "node", pathQueryStub{}},
		{StatusError, "node", pathQueryStub{err: errors.New("offline")}},
		{StatusVulnerable, "node", pathQueryStub{items: []Vulnerability{{ID: "EUVD-1", Products: []ProductEntry{productEntry("Node", "nodejs", "before 23.0.0")}}}}},
		{StatusUnknown, "node", pathQueryStub{items: []Vulnerability{{ID: "EUVD-1", Products: []ProductEntry{productEntry("Node", "nodejs", ">=20")}}}}},
	} {
		t.Run(tc.status, func(t *testing.T) {
			installed := []packagemanager.InstalledPackage{
				{Manager: "mise", Name: tc.name, Version: "22.10.0", Scope: "project", Path: "/project"},
				{Manager: "mise", Name: tc.name, Version: "22.10.0", Scope: "global", Path: "/global"},
			}
			findings, err := Scan(context.Background(), installed, nil, nil, tc.client)
			if !errors.Is(err, tc.client.err) {
				t.Fatalf("unexpected scan error: %v", err)
			}
			if len(findings) != len(installed) {
				t.Fatalf("findings = %+v", findings)
			}
			for i, finding := range findings {
				pkg := installed[i]
				if finding.Manager != pkg.Manager || finding.Package != pkg.Name || finding.Version != pkg.Version || finding.Scope != pkg.Scope || finding.Path != pkg.Path || finding.Status != tc.status {
					t.Fatalf("lost identity or status: %+v, inventory: %+v", finding, pkg)
				}
			}
		})
	}
}

type stub struct{}

func (stub) Query(_ context.Context, identity Identity, version string) ([]Vulnerability, error) {
	if identity.Product == "Node" && version == "22.10.0" {
		return []Vulnerability{{ID: "EUVD-1", Aliases: "CVE-2026-1\n", BaseScore: 8.1}}, nil
	}
	return nil, nil
}

func TestScanEmitsUnmappedAndClean(t *testing.T) {
	installed := []packagemanager.InstalledPackage{{Manager: "mise", Name: "bat", Version: "1.0"}, {Manager: "mise", Name: "node", Version: "99.0"}}
	findings, err := Scan(context.Background(), installed, nil, nil, stub{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 || findings[0].Status != StatusUnmapped || findings[1].Status != StatusClean {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestEUVDUpgradeNoteDistinguishesAffectedAndUnconfirmedVersions(t *testing.T) {
	identity := Identity{Product: "Node", Vendor: "nodejs"}
	item := Vulnerability{Products: []ProductEntry{productEntry("Node", "nodejs", "before 23.0.0")}}
	if got := euvdUpgradeNote(item, identity, "22.22.4"); got != "Latest available version is still affected; it does not fix this CVE." {
		t.Fatalf("affected note = %q", got)
	}
	if got := euvdUpgradeNote(item, identity, "23.0.0"); got != "Latest available version is not listed as affected by EUVD." {
		t.Fatalf("unaffected note = %q", got)
	}
	if got := euvdUpgradeNote(item, identity, ""); got != "No newer version found; no confirmed fix available." {
		t.Fatalf("missing note = %q", got)
	}
}

func productEntry(name, vendor, version string) ProductEntry {
	var entry ProductEntry
	entry.Product.Name, entry.Product.Vendor.Name, entry.ProductVersion = name, vendor, version
	return entry
}

func TestScanUsesManagerLatestAsUpgradeTarget(t *testing.T) {
	installed := []packagemanager.InstalledPackage{{Manager: "mise", Name: "node", Version: "22.10.0", Scope: "project", Path: "/repo"}}
	outdated := []packagemanager.Package{{Manager: "mise", Name: "node", Current: "22.10.0", Latest: "22.22.4", Scope: "project", Path: "/repo"}}
	findings, err := Scan(context.Background(), installed, outdated, nil, stub{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Advisory != "CVE-2026-1" || findings[0].UpgradeTo != "22.22.4" || findings[0].UpgradeNote == "" {
		t.Fatalf("findings = %#v", findings)
	}
}

type duplicateRuleStub struct{}

func (duplicateRuleStub) Query(_ context.Context, identity Identity, _ string) ([]Vulnerability, error) {
	item := Vulnerability{ID: "EUVD-1", Aliases: "CVE-1", Products: []ProductEntry{
		productEntry(identity.Product, identity.Vendor, ">=20"),
		productEntry(identity.Product, identity.Vendor, ">=20"),
		productEntry(identity.Product, identity.Vendor, "20.*"),
	}}
	return []Vulnerability{item}, nil
}

func TestScanDeduplicatesVulnerabilityPackageAndRetainsUnsupportedRules(t *testing.T) {
	installed := []packagemanager.InstalledPackage{{Manager: "mise", Name: "node", Version: "20.1.0"}}
	findings, err := Scan(context.Background(), installed, nil, nil, duplicateRuleStub{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Status != StatusUnknown || len(findings[0].Rules) != 2 {
		t.Fatalf("findings = %#v", findings)
	}
}
