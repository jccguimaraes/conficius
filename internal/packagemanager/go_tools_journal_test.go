package packagemanager

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadGoToolJournalReadsValidatedImports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go-tools.json")
	contents := `{
  "schema_version": 1,
  "imports": [
    {"module": "golang.org/x/tools/gopls", "version": "v0.20.0", "commands": ["gopls"]}
  ]
}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	journal, err := ReadGoToolJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	want := GoToolJournal{SchemaVersion: goToolJournalSchemaVersion, Imports: []GoToolImport{{Module: "golang.org/x/tools/gopls", Version: "v0.20.0", Commands: []string{"gopls"}}}}
	if !reflect.DeepEqual(journal, want) {
		t.Fatalf("journal = %#v; want %#v", journal, want)
	}
}

func TestReadGoToolJournalRejectsInvalidOrAmbiguousOwnership(t *testing.T) {
	for _, contents := range []string{
		`{"schema_version":2,"imports":[]}`,
		`{"schema_version":1,"imports":[{"module":"gopls","version":"v0.20.0","commands":["gopls"]}]}`,
		`{"schema_version":1,"imports":[{"module":"golang.org/x/tools/gopls","version":"v0.20.0","commands":["gopls"]},{"module":"github.com/example/gopls","version":"v1.0.0","commands":["gopls"]}]}`,
		`{"schema_version":1,"imports":[{"module":"golang.org/x/tools/gopls","version":"v0.20.0","commands":["gopls"]},{"module":"golang.org/x/tools/gopls","version":"v0.21.0","commands":["gopls2"]}]}`,
	} {
		path := filepath.Join(t.TempDir(), "go-tools.json")
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadGoToolJournal(path); err == nil {
			t.Fatalf("invalid journal accepted: %s", contents)
		}
	}
}

func TestWriteGoToolJournalRoundTripsValidatedImports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "go-tools.json")
	journal := GoToolJournal{SchemaVersion: goToolJournalSchemaVersion, Imports: []GoToolImport{{Module: "golang.org/x/tools/gopls", Version: "v0.20.0", Commands: []string{"gopls"}}}}
	if err := WriteGoToolJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	got, err := ReadGoToolJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, journal) {
		t.Fatalf("journal = %#v; want %#v", got, journal)
	}
}
