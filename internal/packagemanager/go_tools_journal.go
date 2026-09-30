package packagemanager

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const goToolJournalSchemaVersion = 1

type GoToolJournal struct {
	SchemaVersion int            `json:"schema_version"`
	Imports       []GoToolImport `json:"imports"`
}

func NewGoToolJournal(imports []GoToolImport) GoToolJournal {
	return GoToolJournal{SchemaVersion: goToolJournalSchemaVersion, Imports: imports}
}

func ReadGoToolJournal(path string) (GoToolJournal, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return GoToolJournal{}, fmt.Errorf("read Go tool journal: %w", err)
	}
	var journal GoToolJournal
	if err := json.Unmarshal(contents, &journal); err != nil {
		return GoToolJournal{}, fmt.Errorf("decode Go tool journal: %w", err)
	}
	if err := journal.Validate(); err != nil {
		return GoToolJournal{}, err
	}
	return journal, nil
}

func UpdateGoToolJournalVersions(path string, versions map[string]string) error {
	if len(versions) == 0 {
		return nil
	}
	journal, err := ReadGoToolJournal(path)
	if err != nil {
		return err
	}
	for i := range journal.Imports {
		if version, exists := versions[journal.Imports[i].Module]; exists {
			journal.Imports[i].Version = version
		}
	}
	return WriteGoToolJournal(path, journal)
}

func WriteGoToolJournal(path string, journal GoToolJournal) error {
	if err := journal.Validate(); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Go tool journal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Go tool journal directory: %w", err)
	}
	if err := os.WriteFile(path, append(contents, '\n'), 0o600); err != nil {
		return fmt.Errorf("write Go tool journal: %w", err)
	}
	return nil
}

func (journal GoToolJournal) Validate() error {
	if journal.SchemaVersion != goToolJournalSchemaVersion {
		return fmt.Errorf("unsupported Go tool journal schema version %d", journal.SchemaVersion)
	}
	modules := make(map[string]struct{}, len(journal.Imports))
	owners := make(map[string]string)
	for _, imported := range journal.Imports {
		if err := imported.Validate(); err != nil {
			return err
		}
		if _, exists := modules[imported.Module]; exists {
			return fmt.Errorf("duplicate Go tool module %q", imported.Module)
		}
		modules[imported.Module] = struct{}{}
		for _, command := range imported.Commands {
			if owner, exists := owners[command]; exists {
				return fmt.Errorf("go tool command %q belongs to both %q and %q", command, owner, imported.Module)
			}
			owners[command] = imported.Module
		}
	}
	return nil
}
