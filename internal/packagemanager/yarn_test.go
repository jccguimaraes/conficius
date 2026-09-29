package packagemanager

import "testing"

func TestParseYarnInstalledReadsNDJSONInventory(t *testing.T) {
	output := []byte("{\"value\":\"lodash@npm:4.17.21\",\"children\":{\"Version\":\"4.17.21\"}}\n{\"value\":\"typescript@npm:5.7.0\",\"children\":{\"Version\":\"5.7.0\"}}\n")
	packages, err := parseYarnInstalled(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 2 || packages[0].Manager != PM_YARN || packages[0].Name != "lodash" || packages[0].Version != "4.17.21" || packages[0].Role != RoleLibrary || packages[1].Name != "typescript" {
		t.Fatalf("packages = %#v", packages)
	}
}

func TestParseYarnInstalledRejectsChangedOutput(t *testing.T) {
	if _, err := parseYarnInstalled([]byte(`{"value":"lodash","children":{}}`)); err == nil {
		t.Fatal("parse succeeded")
	}
}
