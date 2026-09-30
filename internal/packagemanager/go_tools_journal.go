package packagemanager

import (
	"encoding/json"
	"fmt"
	"os"
)

const goToolJournalSchemaVersion = 1

type GoToolJournal struct {
	SchemaVersion int            `json:"schema_version"`
	Imports       []GoToolImport `json:"imports"`
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
	if journal.SchemaVersion != goToolJournalSchemaVersion {
		return GoToolJournal{}, fmt.Errorf("unsupported Go tool journal schema version %d", journal.SchemaVersion)
	}
	owners := make(map[string]string)
	for _, imported := range journal.Imports {
		if err := imported.Validate(); err != nil {
			return GoToolJournal{}, err
		}
		for _, command := range imported.Commands {
			if owner, exists := owners[command]; exists {
				return GoToolJournal{}, fmt.Errorf("go tool command %q belongs to both %q and %q", command, owner, imported.Module)
			}
			owners[command] = imported.Module
		}
	}
	return journal, nil
}
