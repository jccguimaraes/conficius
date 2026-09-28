package packagemanager

import (
	"reflect"
	"testing"
)

func TestParsePipxInstalledReadsSortedInventory(t *testing.T) {
	output := []byte(`{"pipx_spec_version":"0.1","venvs":{"ruff":{"metadata":{"main_package":{"package":"ruff","package_version":"0.9.0","apps":["ruff"]}}},"black":{"metadata":{"main_package":{"package":"black","package_version":"24.2.0","apps":["black","blackd"]}}}}}`)
	got, err := parsePipxInstalled(output)
	if err != nil {
		t.Fatal(err)
	}
	want := []InstalledPackage{
		{Manager: PM_PIPX, Name: "black", Version: "24.2.0", Role: RoleApplication},
		{Manager: PM_PIPX, Name: "ruff", Version: "0.9.0", Role: RoleApplication},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packages = %#v; want %#v", got, want)
	}
}

func TestParsePipxInstalledRejectsChangedOutput(t *testing.T) {
	if _, err := parsePipxInstalled([]byte(`{"venvs":[]}`)); err == nil {
		t.Fatal("parse succeeded")
	}
}
