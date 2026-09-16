package security

import "testing"

func TestSort(t *testing.T) {
	items := []Evidence{{Status: StatusClean}, {Status: StatusError}, {Status: StatusVulnerable, Source: SourceOSV}, {Status: StatusVulnerable, Source: SourceEUVD}}
	Sort(items)
	want := []string{SourceEUVD, SourceOSV, "", ""}
	for i := range want {
		if items[i].Source != want[i] {
			t.Fatalf("items = %#v", items)
		}
	}
}
