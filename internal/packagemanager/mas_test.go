package packagemanager

import (
	"reflect"
	"testing"
)

func TestParseMASInstalledSupportsJSONArray(t *testing.T) {
	output := []byte(`[{"adamID":497799835,"name":"Xcode","version":"27.0"},{"adamID":0,"name":"invalid","version":"1"}]`)
	packages, err := parseMASInstalled(output)
	if err != nil {
		t.Fatal(err)
	}
	want := []InstalledPackage{{Manager: PM_MAS, Name: "497799835", Version: "27.0", Role: RoleApplication}}
	if !reflect.DeepEqual(packages, want) {
		t.Fatalf("packages = %#v; want %#v", packages, want)
	}
}

func TestParseMASInstalledSupportsOneJSONObject(t *testing.T) {
	output := []byte(`{"adamID":497799835,"name":"Xcode","version":"27.0"}`)
	packages, err := parseMASInstalled(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || packages[0].Name != "497799835" {
		t.Fatalf("packages = %#v", packages)
	}
}

func TestParseMASInstalledRejectsChangedOutput(t *testing.T) {
	if _, err := parseMASInstalled([]byte(`not json`)); err == nil {
		t.Fatal("parse succeeded")
	}
}
