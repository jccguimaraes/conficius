package packagemanager

import (
	"reflect"
	"testing"
)

func TestParseUVToolsReadsInventoryAndOutdatedVersions(t *testing.T) {
	output := []byte("black v24.2.0 [latest: 25.1.0]\n- black\n- blackd\nruff v0.9.0\n- ruff\n")
	installed, outdated, err := parseUVTools(output, "/usr/local/bin/uv")
	if err != nil {
		t.Fatal(err)
	}
	wantInstalled := []InstalledPackage{
		{Manager: PM_UV, Name: "black", Version: "24.2.0", Role: RoleApplication},
		{Manager: PM_UV, Name: "ruff", Version: "0.9.0", Role: RoleApplication},
	}
	if !reflect.DeepEqual(installed, wantInstalled) {
		t.Fatalf("installed = %#v; want %#v", installed, wantInstalled)
	}
	records := Records(outdated)
	if len(records) != 1 || records[0].Name != "black" || records[0].Current != "24.2.0" || records[0].Latest != "25.1.0" {
		t.Fatalf("outdated = %#v", records)
	}
}

func TestParseUVToolsRejectsChangedOutput(t *testing.T) {
	if _, _, err := parseUVTools([]byte("black 24.2.0 -> 25.1.0"), "uv"); err == nil {
		t.Fatal("parse succeeded")
	}
}
