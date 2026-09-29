package packagemanager

import "testing"

func TestParseCargoInstalledReadsInventory(t *testing.T) {
	output := []byte("cargo-edit v0.13.3:\n    cargo-add\n    cargo-rm\ndioxus-cli v0.7.10:\n    dx\n")
	packages, err := parseCargoInstalled(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 2 || packages[0].Manager != PM_CARGO || packages[0].Name != "cargo-edit" || packages[0].Version != "0.13.3" || packages[0].Role != RoleApplication || packages[1].Name != "dioxus-cli" {
		t.Fatalf("packages = %#v", packages)
	}
}

func TestParseCargoInstalledRejectsChangedOutput(t *testing.T) {
	if _, err := parseCargoInstalled([]byte("cargo-edit 0.13.3\n")); err == nil {
		t.Fatal("parse succeeded")
	}
}
