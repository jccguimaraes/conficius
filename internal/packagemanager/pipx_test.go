package packagemanager

import (
	"reflect"
	"testing"
)

func TestParsePipxOutdatedReadsVersions(t *testing.T) {
	output := []byte(`{"pipx_result_version":"1","status":"success","data":{"packages_checked":1,"packages":[{"environment":"black","package":"black","version":"24.2.0","latest_version":"25.1.0","pinned":false,"injected":false}],"skipped":[]},"errors":[],"exit_code":0}`)
	packages, err := parsePipxOutdated(output, "/usr/local/bin/pipx")
	if err != nil {
		t.Fatal(err)
	}
	records := Records(packages)
	if len(records) != 1 || records[0].Manager != PM_PIPX || records[0].Name != "black" || records[0].Current != "24.2.0" || records[0].Latest != "25.1.0" || records[0].Executable != "/usr/local/bin/pipx" {
		t.Fatalf("records = %#v", records)
	}
}

func TestParsePipxOutdatedRejectsChangedOutput(t *testing.T) {
	if _, err := parsePipxOutdated([]byte(`{"status":"success","data":{}}`), "pipx"); err == nil {
		t.Fatal("parse succeeded")
	}
}

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
