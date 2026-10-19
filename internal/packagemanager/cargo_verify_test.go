package packagemanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCargoVerifyProjectVersions(t *testing.T) {
	old := cargoProjectEntry("demo", "1.0.0", cargoProjectRegistry)
	target := cargoProjectEntry("demo", "2.0.0", cargoProjectRegistry)
	for _, tc := range []struct {
		name, entries, config string
		wantErr               bool
	}{
		{"target", target, "", false},
		{"unchanged", old, cargoProjectTargetAbsent, true},
		{"wrong target", cargoProjectEntry("demo", "3.0.0", cargoProjectRegistry), "", true},
		{"transitive old retained", old + target, cargoProjectTargetAbsent, false},
		{"preexisting target ambiguous", old + target, "", true},
		{"wrong source", cargoProjectEntry("demo", "2.0.0", "git+https://example.test/demo"), "", true},
		{"different package", cargoProjectEntry("other", "2.0.0", cargoProjectRegistry), "", true},
		{"invalid lock", "[", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := cargoProjectFixture(t, tc.entries)
			plan := UpdatePlan{Manager: PM_CARGO, Scope: "project", Directory: root, Packages: []Package{{Manager: PM_CARGO, Name: "demo", Current: "1.0.0", Latest: "2.0.0", Config: tc.config}}}
			if err := cargoVerifyUpdate(plan); (err != nil) != tc.wantErr {
				t.Fatalf("verify = %v, want error %v", err, tc.wantErr)
			}
		})
	}
}

func TestCargoVerifyProjectMissingAndSymlinkLock(t *testing.T) {
	root := t.TempDir()
	plan := UpdatePlan{Manager: PM_CARGO, Scope: "workspace", Directory: root, Packages: []Package{{Manager: PM_CARGO, Name: "demo", Current: "1.0.0", Latest: "2.0.0"}}}
	if err := cargoVerifyUpdate(plan); err == nil {
		t.Fatal("accepted missing lock")
	}
	target := filepath.Join(t.TempDir(), "Cargo.lock")
	cargoGlobalTestWrite(t, target, cargoProjectEntry("demo", "2.0.0", cargoProjectRegistry))
	if err := os.Symlink(target, filepath.Join(root, "Cargo.lock")); err != nil {
		t.Fatal(err)
	}
	if err := cargoVerifyUpdate(plan); err == nil {
		t.Fatal("accepted symlink lock")
	}
}

func TestCargoVerifyGlobalTracking(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(string) string
		wantErr bool
	}{
		{"updated", func(s string) string { return s }, false},
		{"unchanged", func(s string) string { return strings.Replace(s, "2.0.0", "1.0.0", 1) }, true},
		{"newer not exact", func(s string) string { return strings.Replace(s, "2.0.0", "3.0.0", 1) }, true},
		{"missing", func(string) string { return `{"installs":{}}` }, true},
		{"malformed", func(string) string { return "{" }, true},
		{"bins changed", func(s string) string { return strings.Replace(s, `"demo","helper"`, `"demo"`, 1) }, true},
		{"features changed", func(s string) string { return strings.Replace(s, `"tls","demo/extra"`, `"tls"`, 1) }, true},
		{"all features changed", func(s string) string { return strings.Replace(s, `"all_features":true`, `"all_features":false`, 1) }, true},
		{"defaults changed", func(s string) string {
			return strings.Replace(s, `"no_default_features":true`, `"no_default_features":false`, 1)
		}, true},
		{"profile changed", func(s string) string { return strings.Replace(s, `"release"`, `"dev"`, 1) }, true},
		{"target changed", func(s string) string { return strings.Replace(s, `"aarch64-apple-darwin"`, `"x86_64-apple-darwin"`, 1) }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".crates2.json")
			cargoGlobalTestOwned(t, root, cargoGlobalFixture)
			cargoGlobalTestWrite(t, path, cargoGlobalFixture)
			installed, err := cargoGlobalTracking(root)
			if err != nil {
				t.Fatal(err)
			}
			pkg := installed["demo"]
			pkg.Latest = "2.0.0"
			updated := strings.Replace(cargoGlobalFixture, "1.0.0", "2.0.0", 1)
			cargoGlobalTestOwned(t, root, updated)
			cargoGlobalTestWrite(t, path, tc.mutate(updated))
			t.Setenv("CARGO_HOME", t.TempDir())
			t.Setenv("CARGO_INSTALL_ROOT", t.TempDir())
			if err := cargoVerifyUpdate(UpdatePlan{Manager: PM_CARGO, Scope: "global", Packages: []Package{pkg}}); (err != nil) != tc.wantErr {
				t.Fatalf("verify = %v, want error %v", err, tc.wantErr)
			}
		})
	}
}

func TestCargoProjectTargetPresenceEvidence(t *testing.T) {
	for _, preexisting := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "preexisting"}[preexisting], func(t *testing.T) {
			entries := cargoProjectEntry("demo", "1.0.0", cargoProjectRegistry)
			if preexisting {
				entries += cargoProjectEntry("demo", "2.0.0", cargoProjectRegistry)
			}
			root := cargoProjectFixture(t, entries)
			executable, _ := cargoProjectFake(t, root, `case "$*" in
*demo@1.0.0*) printf '%s\n' 'Locking 1 package to latest compatible versions' 'Updating demo v1.0.0 -> v2.0.0' 'warning: not updating lockfile due to dry run' >&2;;
*demo@2.0.0*) printf '%s\n' 'Locking 0 packages to latest compatible versions' 'warning: not updating lockfile due to dry run' >&2;;
*) exit 2;;
esac`)
			packages := make(Packages)
			if err := cargoProjectOutdated(t.Context(), executable, Scope{Kind: "project", Path: root}, packages); err != nil {
				t.Fatal(err)
			}
			records := Records(packages)
			if len(records) != 1 || (records[0].Config == cargoProjectTargetAbsent) == preexisting {
				t.Fatalf("target evidence = %#v", records)
			}
		})
	}
}
