package euvd

import (
	"context"
	"testing"

	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/jccguimaraes/conficius/internal/security"
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
	if len(findings) != 2 || findings[0].Status != security.StatusUnmapped || findings[1].Status != security.StatusClean {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestScanUsesManagerLatestAsUpgradeTarget(t *testing.T) {
	installed := []packagemanager.InstalledPackage{{Manager: "mise", Name: "node", Version: "22.10.0", Scope: "project", Path: "/repo"}}
	outdated := []packagemanager.Package{{Manager: "mise", Name: "node", Current: "22.10.0", Latest: "22.22.4", Scope: "project", Path: "/repo"}}
	findings, err := Scan(context.Background(), installed, outdated, nil, stub{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Advisory != "CVE-2026-1" || findings[0].UpgradeTo != "22.22.4" {
		t.Fatalf("findings = %#v", findings)
	}
}
