package packagemanager

import "testing"

func TestParsePoetryPackagesReadsInventoryAndOutdated(t *testing.T) {
	output := []byte(`[{"name":"requests","installed_status":"installed","version":"2.31.0","latest_version":"2.32.0","description":"HTTP"},{"name":"urllib3","installed_status":"installed","version":"2.2.0","latest_version":"2.2.0","description":"HTTP"}]`)
	installed, outdated, err := parsePoetryPackages(output, "/usr/local/bin/poetry")
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 2 || installed[0].Manager != PM_POETRY || installed[0].Name != "requests" || installed[0].Role != RoleLibrary {
		t.Fatalf("installed = %#v", installed)
	}
	records := Records(outdated)
	if len(records) != 1 || records[0].Name != "requests" || records[0].Current != "2.31.0" || records[0].Latest != "2.32.0" {
		t.Fatalf("outdated = %#v", records)
	}
}

func TestParsePoetryPackagesRejectsChangedOutput(t *testing.T) {
	if _, _, err := parsePoetryPackages([]byte(`{"packages":[]}`), "poetry"); err == nil {
		t.Fatal("parse succeeded")
	}
}
