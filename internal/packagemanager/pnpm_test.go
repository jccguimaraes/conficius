package packagemanager

import "testing"

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
