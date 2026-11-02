package packagemanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEquivalentMiseVersionsIgnoreVPrefix(t *testing.T) {
	if !equivalentMiseVersions("v2.13.2", "2.13.2") {
		t.Fatal("v-prefixed version should be equivalent")
	}
	if equivalentMiseVersions("v2.13.1", "2.13.2") {
		t.Fatal("different versions should not be equivalent")
	}
}

func TestMiseQueriesIsolateGlobalFromLaunchProject(t *testing.T) {
	bin, project := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "mise.toml"), []byte("[tools]\nnode = '24'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(bin, "args")
	writeExecutable(t, filepath.Join(bin, "mise"), `#!/bin/sh
[ "$1" = "--version" ] && { printf '2026.10.24'; exit 0; }
printf '%s|%s\n' "$PWD" "$*" >> "$ARGS_LOG"
version=24.0.0
case " $* " in *" --global "*) version=20.0.0 ;; esac
case "$1" in
ls) printf '{"node":[{"version":"%s","installed":true}]}' "$version" ;;
outdated) printf '{"node":{"name":"node","current":"%s","requested":"%s","latest":"25.0.0"}}' "$version" "$version" ;;
*) exit 1 ;;
esac
`)
	t.Setenv("PATH", bin)
	t.Setenv("ARGS_LOG", log)
	t.Chdir(project)
	for _, scope := range []Scope{{Kind: "global"}, {Kind: "project", Path: project}, {Kind: "workspace", Path: project}} {
		t.Run(scope.Kind, func(t *testing.T) {
			installed, err := QueryInstalledScopes(t.Context(), []string{PM_MISE}, []Scope{scope})
			if err != nil {
				t.Fatal(err)
			}
			outdated, err := QueryOutdatedScopes(t.Context(), []string{PM_MISE}, []Scope{scope})
			if err != nil {
				t.Fatal(err)
			}
			version, flag, bump := "24.0.0", "--local", " --bump"
			if scope.Kind == "global" {
				version, flag, bump = "20.0.0", "--global", ""
			}
			if len(installed.Packages) != 1 || installed.Packages[0].Version != version || installed.Packages[0].Scope != scope.Kind || installed.Packages[0].Path != scope.Path {
				t.Errorf("inventory = %+v; want node@%s in %+v", installed.Packages, version, scope)
			}
			records := Records(outdated.Packages)
			if len(records) != 1 || records[0].Current != version || records[0].Scope != scope.Kind || records[0].Path != scope.Path {
				t.Errorf("outdated = %+v; want node@%s in %+v", records, version, scope)
			}
			args, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			for _, command := range []string{"ls --current --installed --json " + flag, "outdated --json " + flag + bump} {
				if !strings.Contains(string(args), project+"|"+command+"\n") {
					t.Errorf("missing %q in calls:\n%s", command, args)
				}
			}
		})
	}
}

func TestMiseLegacyListOutdatedKeepsCurrentConfig(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "mise")
	writeExecutable(t, executable, `#!/bin/sh
[ "$*" = "outdated --json --bump" ] || { printf 'unexpected args: %s' "$*" >&2; exit 1; }
printf '{"node":{"name":"node","current":"24.0.0","latest":"25.0.0"}}'
`)
	packages := make(Packages)
	if err := (Mise{}).ListOutdated(t.Context(), executable, packages); err != nil {
		t.Fatal(err)
	}
	if records := Records(packages); len(records) != 1 || records[0].Current != "24.0.0" {
		t.Fatalf("legacy outdated = %+v", records)
	}
}
