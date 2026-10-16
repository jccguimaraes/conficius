package packagemanager

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoInspectionUsesVersionSubcommand(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	writeExecutable(t, filepath.Join(dir, PM_GO), "#!/bin/sh\nprintf '%s' \"$*\" >\"$ARGS_FILE\"\nprintf 'go version go1.27.1'\n")
	t.Setenv("PATH", dir)
	t.Setenv("ARGS_FILE", argsFile)

	statuses, err := Inspect([]string{PM_GO})
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if statuses[0].ManagerVersion != "1.27.1" || string(args) != "version" {
		t.Fatalf("statuses = %#v, args = %q", statuses, args)
	}
}

func TestGoAdapterReadsJournaledGlobalInventory(t *testing.T) {
	dir := t.TempDir()
	state := t.TempDir()
	journalPath := filepath.Join(state, "conficius", "go-tools.json")
	if err := WriteGoToolJournal(journalPath, NewGoToolJournal([]GoToolImport{{Module: "golang.org/x/tools/gopls", Version: "v0.20.0", Commands: []string{"gopls"}}})); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(dir, PM_GO), "#!/bin/sh\nprintf 'go version go1.27.1'\n")
	t.Setenv("PATH", dir)
	t.Setenv("XDG_STATE_HOME", state)

	result, err := QueryInstalledScopes(context.Background(), []string{PM_GO}, []Scope{{Kind: "global"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Packages) != 1 || result.Packages[0].Name != "golang.org/x/tools/gopls" || result.Packages[0].Version != "v0.20.0" || result.Packages[0].Role != RoleDevelopmentTool {
		t.Fatalf("packages = %#v", result.Packages)
	}
	if len(result.Commands) != 1 || result.Commands[0] != (ExposedCommand{Package: PackageIdentity{Manager: PM_GO, Name: "golang.org/x/tools/gopls"}, Name: "gopls", Scope: "global"}) {
		t.Fatalf("commands = %#v", result.Commands)
	}
}

func TestGoAdapterQueriesJournaledModuleFreshness(t *testing.T) {
	dir := t.TempDir()
	state := t.TempDir()
	journalPath := filepath.Join(state, "conficius", "go-tools.json")
	if err := WriteGoToolJournal(journalPath, NewGoToolJournal([]GoToolImport{{Module: "golang.org/x/tools/gopls", Version: "v0.20.0", Commands: []string{"gopls"}}})); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(dir, PM_GO), `#!/bin/sh
if [ "$1" = "--version" ]; then printf 'go version go1.27.1'; else printf '{"Path":"golang.org/x/tools/gopls","Version":"v0.23.0"}'; fi
`)
	t.Setenv("PATH", dir)
	t.Setenv("XDG_STATE_HOME", state)

	result, err := QueryOutdatedScopes(context.Background(), []string{PM_GO}, []Scope{{Kind: "global"}})
	if err != nil {
		t.Fatal(err)
	}
	records := Records(result.Packages)
	if len(records) != 1 || records[0].Name != "golang.org/x/tools/gopls" || records[0].Current != "v0.20.0" || records[0].Latest != "v0.23.0" {
		t.Fatalf("records = %#v", records)
	}
}

func TestGoAdapterPlansExactLatestInstalls(t *testing.T) {
	dir := t.TempDir()
	state := t.TempDir()
	journalPath := filepath.Join(state, "conficius", "go-tools.json")
	if err := WriteGoToolJournal(journalPath, NewGoToolJournal([]GoToolImport{{Module: "golang.org/x/tools/gopls", Version: "v0.20.0", Commands: []string{"gopls"}}})); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(dir, PM_GO), `#!/bin/sh
if [ "$1" = "--version" ]; then printf 'go version go1.27.1'; else printf '{"Path":"golang.org/x/tools/gopls","Version":"v0.23.0"}'; fi
`)
	t.Setenv("PATH", dir)
	t.Setenv("XDG_STATE_HOME", state)

	result, err := QueryOutdatedScopes(context.Background(), []string{PM_GO}, []Scope{{Kind: "global"}})
	if err != nil {
		t.Fatal(err)
	}
	plans, err := PlanUpdates(result, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || len(plans[0].Args) != 2 || plans[0].Args[0] != "install" || plans[0].Args[1] != "golang.org/x/tools/gopls@v0.23.0" || len(plans[0].Recovery) != 1 || plans[0].Recovery[0].Args[1] != "golang.org/x/tools/gopls@v0.20.0" {
		t.Fatalf("plans = %#v", plans)
	}
}

func TestGoAdapterPlansOneInstallPerModule(t *testing.T) {
	result := QueryResult{Packages: Packages{
		"example.com/one": {goTool{module: "example.com/one", current: "v1.0.0", latest: "v1.1.0", executable: "/bin/go"}},
		"example.com/two": {goTool{module: "example.com/two", current: "v1.0.0", latest: "v1.1.0", executable: "/bin/go"}},
	}}
	plans, err := PlanUpdates(result, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 || len(plans[0].Packages) != 1 || len(plans[1].Packages) != 1 {
		t.Fatalf("plans = %#v", plans)
	}
}

func TestGoUpdateExecutesExactInstall(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	goExecutable := filepath.Join(dir, PM_GO)
	writeExecutable(t, goExecutable, "#!/bin/sh\nprintf '%s' \"$*\" >\"$ARGS_PATH\"\n")
	t.Setenv("ARGS_PATH", argsPath)
	plans := []UpdatePlan{{Manager: PM_GO, Executable: goExecutable, Scope: "global", StateHash: "plan", Args: []string{"install", "golang.org/x/tools/gopls@v0.23.0"}}}
	outcomes, err := ApplyUpdatesWithOutcomes(context.Background(), plans, false, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil || len(outcomes) != 1 || outcomes[0].Status != "completed" {
		t.Fatalf("outcomes = %#v, error = %v", outcomes, err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil || string(args) != "install golang.org/x/tools/gopls@v0.23.0" {
		t.Fatalf("args = %q, error = %v", args, err)
	}
}

func TestVerifyGoUpdateUpdatesJournalVersion(t *testing.T) {
	dir := t.TempDir()
	state := t.TempDir()
	journalPath := filepath.Join(state, "conficius", "go-tools.json")
	if err := WriteGoToolJournal(journalPath, NewGoToolJournal([]GoToolImport{{Module: "golang.org/x/tools/gopls", Version: "v0.20.0", Commands: []string{"gopls"}}})); err != nil {
		t.Fatal(err)
	}
	goExecutable := filepath.Join(dir, PM_GO)
	writeExecutable(t, goExecutable, "#!/bin/sh\nprintf '{\"Path\":\"golang.org/x/tools/gopls\",\"Version\":\"v0.23.0\"}'\n")
	t.Setenv("XDG_STATE_HOME", state)
	plans := []UpdatePlan{{Manager: PM_GO, Executable: goExecutable, Scope: "global", StateHash: "plan", Packages: []Package{{Manager: PM_GO, Name: "golang.org/x/tools/gopls", Current: "v0.20.0", Latest: "v0.23.0"}}}}
	outcomes, err := VerifyUpdateOutcomes(context.Background(), plans, []UpdateOutcome{{StateHash: "plan", Status: "completed"}}, QueryOptions{})
	if err != nil || outcomes[0].Status != "completed" {
		t.Fatalf("outcomes = %#v, error = %v", outcomes, err)
	}
	journal, err := ReadGoToolJournal(journalPath)
	if err != nil || journal.Imports[0].Version != "v0.23.0" {
		t.Fatalf("journal = %#v, error = %v", journal, err)
	}
}

func TestGoFreshnessClassifiesCommandAndParserFailures(t *testing.T) {
	for _, test := range []struct {
		name     string
		script   string
		category ErrorCategory
	}{
		{name: "command", script: "printf 'lookup failed' >&2; exit 2", category: ErrorCommand},
		{name: "parser", script: "printf 'changed output'", category: ErrorParser},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			state := t.TempDir()
			journalPath := filepath.Join(state, "conficius", "go-tools.json")
			if err := WriteGoToolJournal(journalPath, NewGoToolJournal([]GoToolImport{{Module: "golang.org/x/tools/gopls", Version: "v0.20.0", Commands: []string{"gopls"}}})); err != nil {
				t.Fatal(err)
			}
			writeExecutable(t, filepath.Join(dir, PM_GO), "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'go version go1.27.1'; else "+test.script+"; fi\n")
			t.Setenv("PATH", dir)
			t.Setenv("XDG_STATE_HOME", state)

			result, err := QueryOutdatedScopes(context.Background(), []string{PM_GO}, []Scope{{Kind: "global"}})
			if err == nil || len(result.Managers) != 1 || result.Managers[0].Operations[0].Category != test.category {
				t.Fatalf("result = %#v, error = %v", result, err)
			}
		})
	}
}

func TestGoAdapterTreatsMissingJournalAsEmptyInventory(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_GO), "#!/bin/sh\nprintf 'go version go1.27.1'\n")
	t.Setenv("PATH", dir)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	result, err := QueryInstalledScopes(context.Background(), []string{PM_GO}, []Scope{{Kind: "global"}})
	if err != nil || len(result.Packages) != 0 {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestGoAdapterReportsInvalidJournal(t *testing.T) {
	dir := t.TempDir()
	state := t.TempDir()
	path := filepath.Join(state, "conficius", "go-tools.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":2,"imports":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(dir, PM_GO), "#!/bin/sh\nprintf 'go version go1.27.1'\n")
	t.Setenv("PATH", dir)
	t.Setenv("XDG_STATE_HOME", state)

	result, err := QueryInstalledScopes(context.Background(), []string{PM_GO}, []Scope{{Kind: "global"}})
	if err == nil || len(result.Managers) != 1 || result.Managers[0].Operations[0].Category != ErrorParser {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestGoProjectInspection(t *testing.T) {
	for _, kind := range []string{"project", "workspace"} {
		t.Run(kind, func(t *testing.T) {
			root, bin := t.TempDir(), t.TempDir()
			manifest := filepath.Join(root, "go.mod")
			original := []byte("module example.com/main\n\ngo 1.26.0\n\ntool (\n\tgolang.org/x/tools/cmd/stringer\n\tgolang.org/x/tools/gopls\n\texample.com/tool\n)\n")
			if err := os.WriteFile(manifest, original, 0o600); err != nil {
				t.Fatal(err)
			}
			argsFile := filepath.Join(root, "args")
			t.Setenv("ARGS_FILE", argsFile)
			t.Setenv("ROOT", root)
			writeExecutable(t, filepath.Join(bin, PM_GO), `#!/bin/sh
if [ "$1" = version ]; then printf 'go version go1.26.8'; exit; fi
[ "$PWD" = "$ROOT" ] || exit 2
printf '%s\n' "$*" >> "$ARGS_FILE"
case "$*" in
'list -m -json -mod=readonly all')
  [ "$GOPROXY" = off ] && [ "$GONOPROXY" = none ] && [ "$GOSUMDB" = off ] && [ "$GOTOOLCHAIN" = local ] || exit 3 ;;
'list -m -json -mod=readonly -u all') ;;
*) exit 4 ;;
esac
printf '%s' '
{"Path":"example.com/main","Main":true}
{"Path":"example.com/other-main","Main":true}
{"Path":"go","Version":"1.26.0"}
{"Path":"toolchain","Version":"go1.26.8"}
{"Path":"example.com/direct","Version":"v1.9.0","Update":{"Path":"example.com/direct","Version":"v1.10.0"}}
{"Path":"example.com/transitive","Version":"v0.0.0-20260101000000-abcdefabcdef","Indirect":true,"Update":{"Path":"example.com/transitive","Version":"v1.0.0"}}
{"Path":"golang.org/x/tools","Version":"v0.1.0","Indirect":true,"Update":{"Path":"golang.org/x/tools","Version":"v0.2.0"}}
{"Path":"golang.org/x/tools/cmd","Version":"v0.1.0","Indirect":true,"Update":{"Path":"golang.org/x/tools/cmd","Version":"v0.2.0"}}
{"Path":"golang.org/x/tool","Version":"v0.1.0","Indirect":true,"Update":{"Path":"golang.org/x/tool","Version":"v0.2.0"}}
{"Path":"example.com/tool","Version":"v1.0.0","Indirect":true,"Update":{"Path":"example.com/tool","Version":"v1.1.0"}}
{"Path":"example.com/local","Version":"v1.0.0","Replace":{"Path":"../local"},"Update":{"Path":"example.com/local","Version":"v2.0.0"}}
{"Path":"example.com/replaced","Version":"v1.0.0","Replace":{"Path":"example.com/fork","Version":"v1.2.3"},"Update":{"Path":"example.com/replaced","Version":"v1.1.0"}}
{"Path":"example.com/older","Version":"v1.10.0","Update":{"Path":"example.com/older","Version":"v1.9.0"}}
{"Path":"example.com/equal","Version":"v1.0.0","Update":{"Path":"example.com/equal","Version":"v1.0.0"}}
'
`)
			t.Setenv("PATH", bin)
			scopes := []Scope{{Kind: kind, Path: root}}
			inventory, err := QueryInstalledScopes(context.Background(), []string{PM_GO}, scopes)
			if err != nil {
				t.Fatal(err)
			}
			if len(inventory.Packages) != 9 || len(inventory.Commands) != 0 {
				t.Fatalf("inventory = %#v", inventory)
			}
			foundFork, foundTransitive := false, false
			for _, pkg := range inventory.Packages {
				if pkg.Scope != kind || pkg.Path != root || pkg.Manifest != manifest || pkg.OSVEcosystem != "Go" || pkg.OSVName != pkg.Name || pkg.Role != RoleLibrary {
					t.Fatalf("package = %#v", pkg)
				}
				if pkg.Name == "example.com/fork" {
					foundFork = pkg.Version == "v1.2.3"
				}
				if pkg.Name == "example.com/transitive" {
					foundTransitive = pkg.Transitive && pkg.DependencyClass == "indirect"
				}
			}
			if !foundFork || !foundTransitive {
				t.Fatalf("packages = %#v", inventory.Packages)
			}
			outdated, err := QueryOutdatedScopes(context.Background(), []string{PM_GO}, scopes)
			if err != nil {
				t.Fatal(err)
			}
			records := Records(outdated.Packages)
			expected := map[string][2]string{
				"example.com/direct":     {"v1.9.0", "v1.10.0"},
				"example.com/tool":       {"v1.0.0", "v1.1.0"},
				"golang.org/x/tools":     {"v0.1.0", "v0.2.0"},
				"golang.org/x/tools/cmd": {"v0.1.0", "v0.2.0"},
			}
			if len(records) != len(expected) {
				t.Fatalf("records = %#v", records)
			}
			for _, record := range records {
				versions, ok := expected[record.Name]
				if !ok || record.Current != versions[0] || record.Latest != versions[1] || record.Scope != kind || record.Manifest != manifest {
					t.Fatalf("record = %#v", record)
				}
			}
			if plans, err := PlanUpdates(outdated, nil, nil); err == nil || !strings.Contains(err.Error(), "updates are unsupported") || len(plans) != 0 {
				t.Fatalf("plans = %#v, error = %v", plans, err)
			}
			args, err := os.ReadFile(argsFile)
			if err != nil || string(args) != "list -m -json -mod=readonly all\nlist -m -json -mod=readonly -u all\n" {
				t.Fatalf("args = %q, error = %v", args, err)
			}
			contents, err := os.ReadFile(manifest)
			if err != nil || !bytes.Equal(contents, original) {
				t.Fatalf("manifest changed: %q, error = %v", contents, err)
			}
			if _, err := os.Stat(filepath.Join(root, "go.sum")); !os.IsNotExist(err) {
				t.Fatalf("unexpected go.sum: %v", err)
			}
		})
	}
}

func TestGoProjectToolModuleSelection(t *testing.T) {
	for _, test := range []struct {
		name, manifest, want, wantError string
	}{
		{name: "longest boundary prefix", manifest: "module example.com/main\n tool golang.org/x/tools/cmd/stringer\n", want: "golang.org/x/tools/cmd"},
		{name: "module prefix", manifest: "module example.com/main\n tool golang.org/x/tools/gopls\n", want: "golang.org/x/tools"},
		{name: "exact module", manifest: "module example.com/main\n tool golang.org/x/tool\n", want: "golang.org/x/tool"},
		{name: "no boundary match", manifest: "module example.com/main\n tool golang.org/x/toolsmith/cmd\n"},
		{name: "no tools", manifest: "module example.com/main\n"},
		{name: "invalid manifest", manifest: "module example.com/main\n tool (\n", wantError: "parse Go project manifest"},
		{name: "missing manifest", wantError: "read Go project manifest"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if test.manifest != "" {
				if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(test.manifest), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			executable := filepath.Join(t.TempDir(), "go")
			writeExecutable(t, executable, `#!/bin/sh
printf '%s' '
{"Path":"golang.org/x/tool","Version":"v0.1.0","Indirect":true,"Update":{"Path":"golang.org/x/tool","Version":"v0.2.0"}}
{"Path":"golang.org/x/tools","Version":"v0.1.0","Indirect":true,"Update":{"Path":"golang.org/x/tools","Version":"v0.2.0"}}
{"Path":"golang.org/x/tools/cmd","Version":"v0.1.0","Indirect":true,"Update":{"Path":"golang.org/x/tools/cmd","Version":"v0.2.0"}}
'
`)
			packages := Packages{}
			err := (Go{}).listOutdatedScope(context.Background(), executable, Scope{Kind: "project", Path: root}, ManagerOptions{}, packages)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) || len(packages) != 0 {
					t.Fatalf("packages = %#v, error = %v", packages, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.want == "" {
				if len(packages) != 0 {
					t.Fatalf("packages = %#v", packages)
				}
			} else if len(packages) != 1 || len(packages[test.want]) != 1 {
				t.Fatalf("packages = %#v, want %s", packages, test.want)
			}
		})
	}
}

func TestGoProjectRejectsInvalidModuleStreams(t *testing.T) {
	for _, output := range []string{
		"", "null", "[]", "{}", `{`,
		`{"Path":"example.com/bad"}`,
		`{"Path":"example.com/bad","Version":"1.0.0"}`,
		`{"Path":"example.com/bad","Version":7}`,
		`{"Path":"example.com/bad","Version":"v1.0.0","Error":{"Err":"failed"}}`,
		`{"Path":"example.com/bad","Version":"v1.0.0","Update":{}}`,
		`{"Path":"example.com/bad","Version":"v1.0.0","Update":{"Path":"example.com/bad"}}`,
		`{"Path":"example.com/bad","Version":"v1.0.0","Update":{"Path":"example.com/other","Version":"v1.1.0"}}`,
		`{"Path":"example.com/bad","Version":"v1.0.0","Replace":{"Path":"example.com/fork","Version":"bad"}}`,
		`{"Path":"example.com/bad","Version":"v1.0.0","Replace":{"Path":"../local","Error":{"Err":"failed"}}}`,
		`{"Path":"example.com/bad","Version":"v1.0.0","Update":{"Path":"example.com/bad","Version":"v1.1.0","Error":{"Err":"failed"}}}`,
		`{"Path":"example.com/good","Version":"v1.0.0","Update":{"Path":"example.com/good","Version":"v1.1.0"}} {`,
	} {
		t.Run(output, func(t *testing.T) {
			root := t.TempDir()
			executable := filepath.Join(t.TempDir(), "go")
			t.Setenv("MODULE_OUTPUT", output)
			writeExecutable(t, executable, "#!/bin/sh\nprintf '%s' \"$MODULE_OUTPUT\"\n")
			scope := Scope{Kind: "project", Path: root}
			inventory, err := (Go{}).listInstalled(context.Background(), executable, scope)
			if err == nil || len(inventory) != 0 {
				t.Fatalf("inventory = %#v, error = %v", inventory, err)
			}
			packages := Packages{}
			if err := (Go{}).listOutdatedScope(context.Background(), executable, scope, ManagerOptions{}, packages); err == nil || len(packages) != 0 {
				t.Fatalf("packages = %#v, error = %v", packages, err)
			}
		})
	}
}

func TestGoProjectManifestEvidence(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"package.json", "Cargo.lock"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if hasProjectManagerManifest(Go{}, Scope{Kind: "project", Path: root}) {
		t.Fatal("unrelated manifests enabled Go")
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !hasProjectManagerManifest(Go{}, Scope{Kind: "workspace", Path: root}) {
		t.Fatal("go.mod did not enable Go")
	}
	if files := (Go{}).projectFiles(); len(files) != 1 || files[0] != "go.mod" {
		t.Fatalf("files = %v", files)
	}
}

func TestGoProjectCoexistsWithMise(t *testing.T) {
	root, bin := t.TempDir(), t.TempDir()
	for name, contents := range map[string]string{"go.mod": "module example.com/main\n", "mise.toml": "[tools]\ngo = '1.26.8'\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeExecutable(t, filepath.Join(bin, PM_GO), `#!/bin/sh
if [ "$1" = version ]; then printf 'go version go1.26.8'; else printf '%s' '{"Path":"example.com/lib","Version":"v1.0.0","Update":{"Path":"example.com/lib","Version":"v1.1.0"}}'; fi
`)
	writeExecutable(t, filepath.Join(bin, PM_MISE), `#!/bin/sh
case "$1" in
--version) printf 'mise 2026.10.0' ;;
ls) printf '%s' '{"go":[{"version":"1.26.8","installed":true}]}' ;;
outdated) printf '%s' '{"go":{"name":"go","current":"1.26.8","latest":"1.27.0","requested":"1.26.8"}}' ;;
*) exit 2 ;;
esac
`)
	t.Setenv("PATH", bin)
	managers := []string{PM_MISE, PM_GO}
	scopes := []Scope{{Kind: "project", Path: root}}
	inventory, err := QueryInstalledScopes(context.Background(), managers, scopes)
	if err != nil || len(inventory.Packages) != 2 {
		t.Fatalf("inventory = %#v, error = %v", inventory, err)
	}
	outdated, err := QueryOutdatedScopes(context.Background(), managers, scopes)
	if err != nil || len(Records(outdated.Packages)) != 2 {
		t.Fatalf("outdated = %#v, error = %v", outdated, err)
	}
	for _, manager := range managers {
		options := QueryOptions{Disabled: map[string]bool{manager: true}}
		if _, err := QueryInstalledScopesWithOptions(context.Background(), managers, scopes, options); err == nil {
			t.Fatalf("disabled native manager %s accepted", manager)
		}
	}
}

func TestGoProjectCommandFailure(t *testing.T) {
	root, bin := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(bin, PM_GO), "#!/bin/sh\nif [ \"$1\" = version ]; then printf 'go version go1.26.8'; else printf 'cache unavailable' >&2; exit 2; fi\n")
	t.Setenv("PATH", bin)
	scopes := []Scope{{Kind: "project", Path: root}}
	inventory, err := QueryInstalledScopes(context.Background(), []string{PM_GO}, scopes)
	if err == nil || len(inventory.Packages) != 0 || inventory.Managers[0].Operations[0].Category != ErrorCommand {
		t.Fatalf("inventory = %#v, error = %v", inventory, err)
	}
	outdated, err := QueryOutdatedScopes(context.Background(), []string{PM_GO}, scopes)
	if err == nil || len(outdated.Packages) != 0 || outdated.Managers[0].Operations[0].Category != ErrorCommand {
		t.Fatalf("outdated = %#v, error = %v", outdated, err)
	}
}
