package packagemanager

import "testing"

func TestGoToolImportValidatesCanonicalModuleVersionAndCommands(t *testing.T) {
	valid := GoToolImport{
		Module:   "golang.org/x/tools/gopls",
		Version:  "v0.20.0",
		Commands: []string{"gopls"},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid import: %v", err)
	}

	for _, imported := range []GoToolImport{
		{Module: "gopls", Version: "v0.20.0", Commands: []string{"gopls"}},
		{Module: "golang.org/x/tools/gopls", Version: "1.20.0", Commands: []string{"gopls"}},
		{Module: "golang.org/x/tools/gopls", Version: "v0.20.0"},
		{Module: "golang.org/x/tools/gopls", Version: "v0.20.0", Commands: []string{"../gopls"}},
		{Module: "golang.org/x/tools/gopls", Version: "v0.20.0", Commands: []string{"gopls", "gopls"}},
	} {
		if err := imported.Validate(); err == nil {
			t.Fatalf("invalid import accepted: %#v", imported)
		}
	}
}
