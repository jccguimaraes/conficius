package packagemanager

import (
	"sort"
	"testing"
)

func TestParsePNPMInstalledReadsDependencyClasses(t *testing.T) {
	output := []byte(`[{"name":"project","version":"1.0.0","path":"/project","dependencies":{"react":{"version":"19.0.0"}},"devDependencies":{"typescript":{"version":"5.7.0"}},"optionalDependencies":{"fsevents":{"version":"2.3.3"}}}]`)
	packages, err := parsePNPMInstalled(output)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].Name < packages[j].Name })
	if len(packages) != 3 || packages[0].Name != "fsevents" || packages[0].DependencyClass != "optional" || packages[1].Role != RoleLibrary || packages[2].Role != RoleDevelopmentTool {
		t.Fatalf("packages = %#v", packages)
	}
}

func TestParsePNPMInstalledRejectsChangedOutput(t *testing.T) {
	if _, err := parsePNPMInstalled([]byte(`{}`)); err == nil {
		t.Fatal("parse succeeded")
	}
}

func TestParsePNPMOutdatedReadsVersions(t *testing.T) {
	output := []byte(`{"is-negative":{"current":"1.0.0","latest":"2.1.0","wanted":"1.0.0","isDeprecated":false,"dependencyType":"dependencies"},"is-positive":{"current":"1.0.0","latest":"3.1.0","wanted":"2.0.0","isDeprecated":false,"dependencyType":"devDependencies"}}`)
	packages, err := parsePNPMOutdated(output, "/usr/local/bin/pnpm")
	if err != nil {
		t.Fatal(err)
	}
	records := Records(packages)
	if len(records) != 2 || records[0].Manager != PM_PNPM || records[0].Name != "is-negative" || records[0].Current != "1.0.0" || records[0].Requested != "1.0.0" || records[0].Latest != "2.1.0" || records[0].Executable != "/usr/local/bin/pnpm" {
		t.Fatalf("records = %#v", records)
	}
}

func TestParsePNPMOutdatedRejectsChangedOutput(t *testing.T) {
	if _, err := parsePNPMOutdated([]byte(`[]`), "pnpm"); err == nil {
		t.Fatal("parse succeeded")
	}
}
