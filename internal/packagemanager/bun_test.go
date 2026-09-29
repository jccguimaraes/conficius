package packagemanager

import "testing"

func TestParseBunInstalledReadsInventory(t *testing.T) {
	output := []byte("/project node_modules (3 installed)\n├── @types/bun@1.3.0\n├── react@19.0.0\n└── typescript@5.7.0\n")
	packages, err := parseBunInstalled(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 3 || packages[0].Manager != PM_BUN || packages[0].Name != "@types/bun" || packages[0].Version != "1.3.0" || packages[0].Role != RoleLibrary || packages[2].Name != "typescript" {
		t.Fatalf("packages = %#v", packages)
	}
}

func TestParseBunInstalledRejectsChangedOutput(t *testing.T) {
	if _, err := parseBunInstalled([]byte("react 19.0.0\n")); err == nil {
		t.Fatal("parse succeeded")
	}
}
