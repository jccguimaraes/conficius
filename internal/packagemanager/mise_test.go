package packagemanager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/config"
)

func TestEquivalentMiseVersionsIgnoreVPrefix(t *testing.T) {
	if !equivalentMiseVersions("v2.13.2", "2.13.2") {
		t.Fatal("v-prefixed version should be equivalent")
	}
	if equivalentMiseVersions("v2.13.1", "2.13.2") {
		t.Fatal("different versions should not be equivalent")
	}
}

func TestMiseUpdatePlansKeepProjectMutationGuards(t *testing.T) {
	project, bin := t.TempDir(), t.TempDir()
	writeExecutable(t, filepath.Join(bin, "git"), "#!/bin/sh\nif [ \"$3\" = status ]; then printf ' M mise.toml'; else printf '%s\\n' \"$2\"; fi\n")
	t.Setenv("PATH", bin)
	for _, kind := range []string{"global", "project", "workspace"} {
		for _, bump := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/bump=%t", kind, bump), func(t *testing.T) {
				scope := Scope{Kind: kind}
				if kind != "global" {
					scope.Path = project
				}
				plans, err := PlanUpdates(QueryResult{Bump: bump, Packages: Packages{
					"node": {scopedTool{Tool: NewMiseTool(MiseOutdatedTool{Name: "node", Current: "20", Latest: "22"}, "/bin/mise"), scope: scope}},
				}}, nil, nil)
				if err != nil || len(plans) != 1 {
					t.Fatalf("plans = %+v, error = %v", plans, err)
				}
				global := kind == "global"
				if strings.Contains(" "+strings.Join(plans[0].Args, " ")+" ", " --global ") != global {
					t.Errorf("wrong update scope: %v", plans[0].Args)
				}
				if got := plans[0].Args[len(plans[0].Args)-1]; got != "node" {
					t.Errorf("update target = %q; want node", got)
				}
				want := MutationImpact{MayChangeManifests: kind != "global"}
				if plans[0].Impact != want {
					t.Fatalf("impact = %+v; want %+v", plans[0].Impact, want)
				}
				profile := config.Profile{Projects: []config.ProfileProject{{Path: project}}}
				if kind == "global" {
					if err := ValidateProjectMutations(plans, profile, false, false); err != nil {
						t.Fatal(err)
					}
					return
				}
				for _, allowDirty := range []bool{false, true} {
					if err := ValidateProjectMutations(plans, profile, allowDirty, true); err == nil || !strings.Contains(err.Error(), "does not allow manifest changes") {
						t.Fatalf("manifest guard bypassed: %v", err)
					}
				}
				profile.Projects[0].AllowManifestChanges = true
				if err := ValidateProjectMutations(plans, profile, false, false); !errors.Is(err, ErrDirtyProject) {
					t.Fatalf("dirty guard bypassed: %v", err)
				}
				if err := ValidateProjectMutations(plans, profile, true, false); err != nil {
					t.Fatalf("explicit dirty permission rejected: %v", err)
				}
			})
		}
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
if [ "$1" = ls ] && [ "$version" = 20.0.0 ] && [ "$MISE_CEILING_PATHS" != "$PWD" ]; then
  printf '{}'
  exit 0
fi
case "$1" in
ls) printf '{"node":[{"version":"%s","installed":true}]}' "$version" ;;
outdated) printf '{"node":{"name":"node","current":"%s","requested":"%s","latest":"25.0.0"}}' "$version" "$version" ;;
upgrade)
  case " $* " in *" --global "*) ;; *) exit 2 ;; esac
  for target do :; done
  [ "$target" = node ] || exit 3
  ;;
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
			version, flag := "24.0.0", "--local"
			if scope.Kind == "global" {
				version, flag = "20.0.0", "--global"
			}
			if len(installed.Packages) != 1 || installed.Packages[0].Version != version || installed.Packages[0].Scope != scope.Kind || installed.Packages[0].Path != scope.Path {
				t.Errorf("inventory = %+v; want node@%s in %+v", installed.Packages, version, scope)
			}
			records := Records(outdated.Packages)
			if len(records) != 1 || records[0].Current != version || records[0].Scope != scope.Kind || records[0].Path != scope.Path {
				t.Errorf("outdated = %+v; want node@%s in %+v", records, version, scope)
			}
			if scope.Kind == "global" {
				plans, err := PlanUpdates(outdated, nil, nil)
				if err != nil || len(plans) != 1 {
					t.Fatalf("plans = %+v, error = %v", plans, err)
				}
				if _, err := runOutputDir(t.Context(), plans[0].Directory, plans[0].Executable, plans[0].Args...); err != nil {
					t.Fatalf("global upgrade: %v", err)
				}
			}
			args, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			for _, command := range []string{"ls --current --installed --json " + flag, "outdated --json " + flag} {
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
