package packagemanager

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCargoGlobalUpdatePipeline(t *testing.T) {
	home := cargoGlobalTestHome(t)
	tracking := filepath.Join(home, ".crates2.json")
	fixture := strings.TrimSuffix(cargoGlobalFixture, "}}}") + "}," + strings.TrimPrefix(strings.ReplaceAll(strings.ReplaceAll(cargoGlobalFixture, "demo", "other"), "helper", "other-helper"), `{"installs":{`)
	cargoGlobalTestOwned(t, home, fixture)
	cargoGlobalTestWrite(t, tracking, fixture)
	updated := strings.ReplaceAll(fixture, "1.0.0", "2.0.0")
	firstUpdated := strings.Replace(fixture, "demo 1.0.0", "demo 2.0.0", 1)
	owned, err := os.ReadFile(filepath.Join(home, ".crates.toml"))
	if err != nil {
		t.Fatal(err)
	}
	executable, capture := filepath.Join(t.TempDir(), "cargo"), filepath.Join(t.TempDir(), "args")
	writeExecutable(t, executable, fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
case "$*" in
'--version') printf 'cargo 1.99.0\n';;
'info demo --registry crates-io --color never') printf 'demo\nversion: 2.0.0\n';;
'info other --registry crates-io --color never') printf 'other\nversion: 2.0.0\n';;
'install demo '*) printf '%%s' %q > %q; printf '%%b' %q > %q;;
install*) printf '%%s' %q > %q; printf '%%b' %q > %q;;
*) exit 20;;
esac
`, capture, firstUpdated, tracking, strings.Replace(string(owned), "demo 1.0.0", "demo 2.0.0", 1), filepath.Join(home, ".crates.toml"), updated, tracking, strings.ReplaceAll(string(owned), "1.0.0", "2.0.0"), filepath.Join(home, ".crates.toml")))
	options := QueryOptions{Executables: map[string]string{PM_CARGO: executable}}
	query, err := QueryOutdatedScopesWithOptions(t.Context(), []string{PM_CARGO}, []Scope{{Kind: "global"}}, options)
	if err != nil {
		t.Fatal(err)
	}
	query.Packages = Filter(query.Packages, func(pkg Package) bool { return pkg.Manager == PM_CARGO })
	if records := Records(query.Packages); len(records) != 2 || records[0].Config == "" || records[1].Config == "" {
		t.Fatalf("records = %#v", records)
	}
	plans, err := PlanUpdates(query, nil, nil)
	if err != nil || len(plans) != 2 {
		t.Fatalf("plans = %#v, %v", plans, err)
	}
	for _, plan := range plans {
		if len(plan.Packages) != 1 || !reflect.DeepEqual(plan.Args, cargoGlobalUpdateArgs(plan.Packages)) {
			t.Fatalf("unsafe plan = %#v", plan)
		}
		changed := plan
		changed.Packages = append([]Package(nil), plan.Packages...)
		changed.Packages[0].Config = strings.Replace(changed.Packages[0].Config, `"release"`, `"dev"`, 1)
		if updatePlanHash(changed) == plan.StateHash {
			t.Fatal("install options absent from state hash")
		}
	}
	outcomes, err := ApplyUpdatesWithOutcomes(t.Context(), plans, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	outcomes, err = VerifyUpdateOutcomes(t.Context(), plans, outcomes, options)
	if err != nil || len(outcomes) != 2 {
		t.Fatalf("verify = %#v, %v", outcomes, err)
	}
	for _, outcome := range outcomes {
		if outcome.Status != "completed" {
			t.Fatalf("outcome = %#v", outcome)
		}
	}
	after, err := os.ReadFile(capture)
	if err != nil || string(after) != string(before) {
		t.Fatalf("verification issued command: %q, %v", after, err)
	}
}

func TestCargoProjectUpdatePipeline(t *testing.T) {
	for _, kind := range []string{"project", "workspace"} {
		for _, noChange := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/no-change=%v", kind, noChange), func(t *testing.T) {
				original := cargoProjectEntry("demo", "1.0.0", cargoProjectRegistry) + cargoProjectEntry("other", "1.0.0", cargoProjectRegistry)
				root := cargoProjectFixture(t, original)
				updated := strings.Replace(original, `version = "1.0.0"`, `version = "2.0.0"`, 1)
				if noChange {
					updated = original
				}
				executable, capture := cargoProjectFake(t, root, fmt.Sprintf(`case "$*" in
'--version') printf 'cargo 1.99.0\n';;
'update --color never demo@1.0.0 --dry-run') printf '%%s\n' 'Locking 1 package to latest compatible versions' 'Updating demo v1.0.0 -> v2.0.0' 'warning: not updating lockfile due to dry run' >&2;;
'update --color never other@1.0.0 --dry-run') printf '%%s\n' 'Locking 1 package to latest compatible versions' 'Updating other v1.0.0 -> v2.0.0' 'warning: not updating lockfile due to dry run' >&2;;
'update --color never demo@1.0.0 --precise 2.0.0') printf '%%b' %q > Cargo.lock;;
*) exit 21;;
esac`, updated))
				options := QueryOptions{Executables: map[string]string{PM_CARGO: executable}, IncludeTransitive: true}
				query, err := QueryOutdatedScopesWithOptions(t.Context(), []string{PM_CARGO}, []Scope{{Kind: kind, Path: root}}, options)
				if err != nil {
					t.Fatal(err)
				}
				query.Packages = Filter(query.Packages, func(pkg Package) bool { return pkg.Name == "demo" })
				records := Records(query.Packages)
				if len(records) != 1 || records[0].Config != cargoProjectTargetAbsent || records[0].Manifest != filepath.Join(root, "Cargo.toml") {
					t.Fatalf("records = %#v", records)
				}
				plans, err := PlanUpdates(query, nil, nil)
				if err != nil || len(plans) != 1 {
					t.Fatalf("plans = %#v, %v", plans, err)
				}
				plan := plans[0]
				if plan.Directory != root || strings.Join(plan.Args, " ") != "update --color never demo@1.0.0 --precise 2.0.0" || !plan.Impact.MayChangeManifests {
					t.Fatalf("plan = %#v", plan)
				}
				outcomes, err := ApplyUpdatesWithOutcomes(t.Context(), plans, false, nil, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				if !noChange && !reflect.DeepEqual(outcomes[0].ChangedFiles, []string{"Cargo.lock"}) {
					t.Fatalf("changed = %#v", outcomes)
				}
				before, err := os.ReadFile(capture)
				if err != nil {
					t.Fatal(err)
				}
				outcomes, err = VerifyUpdateOutcomes(t.Context(), plans, outcomes, options)
				if (err != nil) != noChange || (outcomes[0].Status == "verification-failed") != noChange {
					t.Fatalf("verify = %#v, %v", outcomes, err)
				}
				after, err := os.ReadFile(capture)
				if err != nil || string(after) != string(before) {
					t.Fatalf("verification issued command: %q, %v", after, err)
				}
			})
		}
	}
}

func TestCargoGlobalUpdateRejectsStaleMetadata(t *testing.T) {
	for _, change := range []string{"features", "bins", "version", "removed package"} {
		t.Run(change, func(t *testing.T) {
			home := cargoGlobalTestHome(t)
			tracking := filepath.Join(home, ".crates2.json")
			cargoGlobalTestOwned(t, home, cargoGlobalFixture)
			cargoGlobalTestWrite(t, tracking, cargoGlobalFixture)
			executable, capture := filepath.Join(t.TempDir(), "cargo"), filepath.Join(t.TempDir(), "args")
			writeExecutable(t, executable, fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\nexit 29\n", capture))
			installed, err := cargoGlobalTracking(home)
			if err != nil {
				t.Fatal(err)
			}
			pkg := installed["demo"]
			pkg.Latest, pkg.Executable = "2.0.0", executable
			plans, err := PlanUpdates(QueryResult{Packages: Packages{"demo": {recordTool{pkg}}}}, nil, nil)
			if err != nil || len(plans) != 1 {
				t.Fatalf("plans = %#v, %v", plans, err)
			}
			contents := cargoGlobalFixture
			switch change {
			case "features":
				contents = strings.Replace(contents, `["tls","demo/extra"]`, `["tls"]`, 1)
			case "bins":
				contents = strings.Replace(contents, `["demo","helper"]`, `["demo"]`, 1)
			case "version":
				contents = strings.Replace(contents, "1.0.0", "1.1.0", 1)
			case "removed package":
				contents = `{"installs":{}}`
			}
			cargoGlobalTestOwned(t, home, contents)
			cargoGlobalTestWrite(t, tracking, contents)
			outcomes, err := ApplyUpdatesWithOutcomes(t.Context(), plans, false, nil, nil, nil)
			if err == nil || !strings.Contains(err.Error(), "stale") || len(outcomes) != 0 {
				t.Fatalf("stale apply = %#v, %v", outcomes, err)
			}
			commands, err := os.ReadFile(capture)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			for _, command := range strings.Split(string(commands), "\n") {
				if strings.HasPrefix(command, "install ") {
					t.Fatalf("stale plan invoked install: %s", commands)
				}
			}
		})
	}
}

func TestCargoProjectUpdateRejectsStaleLock(t *testing.T) {
	for _, change := range []string{"lock content", "same-content lock symlink", "config content"} {
		t.Run(change, func(t *testing.T) {
			root := cargoProjectFixture(t, cargoProjectEntry("demo", "1.0.0", cargoProjectRegistry))
			t.Setenv("CARGO_HOME", t.TempDir())
			config := filepath.Join(root, ".cargo", "config.toml")
			cargoProjectStateWrite(t, config, "[net]\noffline = false\n")
			executable, capture := cargoProjectFake(t, root, "exit 29")
			pkg := Package{Manager: PM_CARGO, Name: "demo", Current: "1.0.0", Latest: "2.0.0", Scope: "project", Path: root, Executable: executable, Config: cargoProjectTargetAbsent}
			plans, err := PlanUpdates(QueryResult{Packages: Packages{"demo": {recordTool{pkg}}}}, nil, nil)
			if err != nil || len(plans) != 1 {
				t.Fatalf("plans = %#v, %v", plans, err)
			}
			if plans[0].StateHash != updatePlanHash(plans[0]) {
				t.Fatal("fresh plan hash does not match")
			}
			lock := filepath.Join(root, "Cargo.lock")
			switch change {
			case "lock content":
				cargoGlobalTestWrite(t, lock, cargoProjectEntry("demo", "1.1.0", cargoProjectRegistry))
			case "same-content lock symlink":
				contents, err := os.ReadFile(lock)
				if err != nil {
					t.Fatal(err)
				}
				other := filepath.Join(t.TempDir(), "Cargo.lock")
				cargoGlobalTestWrite(t, other, string(contents))
				if err := os.Remove(lock); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, lock); err != nil {
					t.Fatal(err)
				}
			case "config content":
				cargoProjectStateWrite(t, config, "[net]\noffline = true\n")
			}
			if plans[0].StateHash == updatePlanHash(plans[0]) {
				t.Fatalf("plan hash did not bind %s", change)
			}
			outcomes, err := ApplyUpdatesWithOutcomes(t.Context(), plans, false, nil, nil, nil)
			if err == nil || !strings.Contains(err.Error(), "stale") || len(outcomes) != 0 {
				t.Fatalf("stale apply = %#v, %v", outcomes, err)
			}
			commands, err := os.ReadFile(capture)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if strings.Contains(string(commands), "|update ") {
				t.Fatalf("stale plan invoked update: %s", commands)
			}
		})
	}
}

func TestCargoProjectUpdateRevalidatesBeforeEachMutation(t *testing.T) {
	for _, bestEffort := range []bool{false, true} {
		t.Run(fmt.Sprintf("best-effort=%v", bestEffort), func(t *testing.T) {
			original := cargoProjectEntry("demo", "1.0.0", cargoProjectRegistry) + cargoProjectEntry("other", "1.0.0", cargoProjectRegistry)
			root := cargoProjectFixture(t, original)
			// First update also changes the second target, invalidating its approved preview.
			updated := strings.ReplaceAll(original, `version = "1.0.0"`, `version = "2.0.0"`)
			executable, capture := cargoProjectFake(t, root, fmt.Sprintf(`case "$*" in
'update --color never demo@1.0.0 --precise 2.0.0') printf '%%b' %q > Cargo.lock;;
*) exit 21;;
esac`, updated))
			packages := make(Packages)
			for _, name := range []string{"demo", "other"} {
				pkg := Package{Manager: PM_CARGO, Name: name, Current: "1.0.0", Latest: "2.0.0", Scope: "project", Path: root, Executable: executable, Config: cargoProjectTargetAbsent}
				packages[name] = []Tool{recordTool{pkg}}
			}
			plans, err := PlanUpdates(QueryResult{Packages: packages}, nil, nil)
			if err != nil || len(plans) != 2 {
				t.Fatalf("plans = %#v, %v", plans, err)
			}
			approved := plans[1].StateHash
			outcomes, err := ApplyUpdatesWithOutcomes(t.Context(), plans, bestEffort, nil, nil, nil)
			if err == nil || !strings.Contains(err.Error(), "refresh preview after prior changes") || len(outcomes) != 2 {
				t.Fatalf("apply = %#v, %v", outcomes, err)
			}
			if outcomes[0].Status != "completed" || !reflect.DeepEqual(outcomes[0].ChangedFiles, []string{"Cargo.lock"}) || outcomes[1].Status != "failed" || outcomes[1].StateHash != approved || plans[1].StateHash != approved {
				t.Fatalf("partial apply = %#v", outcomes)
			}
			lock, err := os.ReadFile(filepath.Join(root, "Cargo.lock"))
			if err != nil || string(lock) != updated {
				t.Fatalf("first mutation not preserved: %q, %v", lock, err)
			}
			commands, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(commands), "|update ") != 1 || strings.Contains(string(commands), "|update --color never other@") {
				t.Fatalf("stale second command invoked: %s", commands)
			}
		})
	}
}

func TestCargoGlobalUpdateRevalidatesBeforeEachMutation(t *testing.T) {
	home := cargoGlobalTestHome(t)
	tracking := filepath.Join(home, ".crates2.json")
	fixture := strings.TrimSuffix(cargoGlobalFixture, "}}}") + "}," + strings.TrimPrefix(strings.ReplaceAll(strings.ReplaceAll(cargoGlobalFixture, "demo", "other"), "helper", "other-helper"), `{"installs":{`)
	cargoGlobalTestOwned(t, home, fixture)
	cargoGlobalTestWrite(t, tracking, fixture)
	updated := strings.ReplaceAll(fixture, "1.0.0", "2.0.0")
	owned, err := os.ReadFile(filepath.Join(home, ".crates.toml"))
	if err != nil {
		t.Fatal(err)
	}
	executable, capture := filepath.Join(t.TempDir(), "cargo"), filepath.Join(t.TempDir(), "args")
	writeExecutable(t, executable, fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
case "$*" in
'install demo '*) printf '%%s' %q > %q; printf '%%b' %q > %q;;
*) exit 21;;
esac
`, capture, updated, tracking, strings.ReplaceAll(string(owned), "1.0.0", "2.0.0"), filepath.Join(home, ".crates.toml")))
	installed, err := cargoGlobalTracking(home)
	if err != nil {
		t.Fatal(err)
	}
	packages := make(Packages)
	for name, pkg := range installed {
		pkg.Latest, pkg.Executable = "2.0.0", executable
		packages[name] = []Tool{recordTool{pkg}}
	}
	plans, err := PlanUpdates(QueryResult{Packages: packages}, nil, nil)
	if err != nil || len(plans) != 2 {
		t.Fatalf("plans = %#v, %v", plans, err)
	}
	outcomes, err := ApplyUpdatesWithOutcomes(t.Context(), plans, false, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "stale") || len(outcomes) != 2 || outcomes[0].Status != "completed" || outcomes[1].Status != "failed" {
		t.Fatalf("partial apply = %#v, %v", outcomes, err)
	}
	commands, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(commands), "install ") != 1 || strings.Contains(string(commands), "install other ") {
		t.Fatalf("stale second command invoked: %s", commands)
	}
}
