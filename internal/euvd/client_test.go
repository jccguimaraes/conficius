package euvd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientMatchesInstalledVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("product") != "Node" || r.URL.Query().Has("vendor") {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		_, _ = fmt.Fprint(w, `{"total":4,"items":[
			{"id":"EUVD-match","aliases":"CVE-2026-1\n","baseScore":8.1,"enisaIdProduct":[{"product":{"name":"Node","vendor":{"name":"nodejs"}},"product_version":"22.0.0 ≤22.22.3"}]},
			{"id":"EUVD-wrong-vendor","enisaIdProduct":[{"product":{"name":"Node","vendor":{"name":"other"}},"product_version":"22.0.0 ≤22.22.3"}]},
			{"id":"EUVD-wrong-product","enisaIdProduct":[{"product":{"name":"Node Manager","vendor":{"name":"nodejs"}},"product_version":"22.0.0 ≤22.22.3"}]},
			{"id":"EUVD-wrong-version","enisaIdProduct":[{"product":{"name":"Node","vendor":{"name":"nodejs"}},"product_version":"23.0.0 ≤23.1.0"}]}
		]}`)
	}))
	defer server.Close()
	items, err := NewClient(server.Client(), server.URL).Query(context.Background(), Identity{Product: "Node", Vendor: "nodejs"}, "22.10.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].AdvisoryID() != "CVE-2026-1" {
		t.Fatalf("items = %#v", items)
	}
}

func TestClientReusesProductCacheAcrossVersions(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = fmt.Fprint(w, `{"total":1,"items":[{"id":"EUVD-1","enisaIdProduct":[{"product":{"name":"Node","vendor":{"name":"nodejs"}},"product_version":"before 23.0.0"}]}]}`)
	}))
	defer server.Close()
	client := NewClient(server.Client(), server.URL)
	identity := Identity{Product: "Node", Vendor: "nodejs"}
	for _, version := range []string{"22.10.0", "22.11.0"} {
		if _, err := client.Query(context.Background(), identity, version); err != nil {
			t.Fatal(err)
		}
	}
	if requests != 1 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestVersionMatchesEUVDRange(t *testing.T) {
	for _, test := range []struct {
		version, rule string
		want          bool
	}{
		{"22.10.0", "22.0.0 ≤22.22.3", true},
		{"23.0.0", "22.0.0 ≤22.22.3", false},
		{"4.17.20", "4.17.20", true},
	} {
		if got, supported := versionMatches(test.version, test.rule); got != test.want || !supported {
			t.Fatalf("versionMatches(%q, %q) = %v, %v", test.version, test.rule, got, supported)
		}
	}
}

func TestNPMCurrentVersionDoesNotMatchCVE202229244Rules(t *testing.T) {
	item := Vulnerability{Products: []ProductEntry{
		productEntryForClientTest("NPM", "npm", "7.9.0 <7.9.0*"),
		productEntryForClientTest("NPM", "npm", "8.11.0 <8.11.0"),
	}}
	if match, _ := item.Match(Identity{Product: "NPM", Vendor: "npm"}, "11.19.1"); match != NoMatch {
		t.Fatalf("match = %v", match)
	}
}

func productEntryForClientTest(name, vendor, version string) ProductEntry {
	var entry ProductEntry
	entry.Product.Name, entry.Product.Vendor.Name, entry.ProductVersion = name, vendor, version
	return entry
}

func TestVersionMatchesEUVDExclusiveRangeWithWildcard(t *testing.T) {
	for _, rule := range []string{"7.9.0 <7.9.0*", "8.11.0 <8.11.0"} {
		matched, supported := versionMatches("11.19.1", rule)
		if matched || !supported {
			t.Fatalf("versionMatches(11.19.1, %q) = %v, %v", rule, matched, supported)
		}
	}
}

func TestVersionMatchesTextualUpperBounds(t *testing.T) {
	for _, test := range []struct {
		version, rule string
		want          bool
	}{
		{"1.2.3", "before 2.0.0", true},
		{"2.1.0", "before 2.0.0", false},
		{"1.5.0", "1.0 through 2.0", true},
		{"2.1.0", "1.0 through 2.0", false},
		{"2.1.0", "<2.0.0", false},
	} {
		matched, supported := versionMatches(test.version, test.rule)
		if matched != test.want || !supported {
			t.Fatalf("versionMatches(%q, %q) = %v, %v", test.version, test.rule, matched, supported)
		}
	}
}
