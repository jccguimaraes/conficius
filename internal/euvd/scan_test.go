package euvd

import (
	"context"
	"testing"

	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

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
