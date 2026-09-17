package security

import "testing"

func TestSortGroupsPackageVersionsByHighestSeverity(t *testing.T) {
	items := []Evidence{
		{Manager: "mise", Package: "node", Version: "22", Advisory: "CVE-node-medium", Score: 5},
		{Manager: "brew", Package: "openssl", Version: "3", Advisory: "CVE-openssl-high", Score: 8},
		{Manager: "mise", Package: "node", Version: "22", Advisory: "CVE-node-critical", Score: 9.8},
		{Manager: "brew", Package: "openssl", Version: "3", Advisory: "CVE-openssl-low", Score: 2},
	}
	Sort(items)
	want := []string{"CVE-node-critical", "CVE-node-medium", "CVE-openssl-high", "CVE-openssl-low"}
	for i := range want {
		if items[i].Advisory != want[i] {
			t.Fatalf("items = %#v", items)
		}
	}
}

func TestSortUsesStatusAndSourceForEqualSeverity(t *testing.T) {
	items := []Evidence{
		{Manager: "mise", Package: "node", Version: "22", Status: StatusClean},
		{Manager: "mise", Package: "node", Version: "22", Status: StatusError},
		{Manager: "mise", Package: "node", Version: "22", Status: StatusVulnerable, Source: SourceOSV},
		{Manager: "mise", Package: "node", Version: "22", Status: StatusVulnerable, Source: SourceEUVD},
	}
	Sort(items)
	want := []string{SourceEUVD, SourceOSV, "", ""}
	for i := range want {
		if items[i].Source != want[i] {
			t.Fatalf("items = %#v", items)
		}
	}
}
