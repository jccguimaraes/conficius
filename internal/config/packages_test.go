package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func packageTestHome(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	xdg, native, err := currentPackageLocations()
	if err != nil {
		t.Fatal(err)
	}
	return xdg, native
}

func writePackageTest(t *testing.T, path, data string) {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	mustWrite(t, path, data)
}

func TestLoadPackagesDefaultsAndIsolation(t *testing.T) {
	xdg, _ := packageTestHome(t)
	// Old global dotfile configuration must not leak into package configuration.
	writePackageTest(t, filepath.Join(filepath.Dir(xdg), FileName), "not valid TOML")
	project, err := LoadPackages("")
	if err != nil || project.Version != CurrentVersion || project.Layout != "xdg" || len(project.Profiles) != 0 || project.Path != "" {
		t.Fatalf("default = %#v, %v", project, err)
	}
	if _, err := LoadPackages(xdg); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("explicit missing = %v", err)
	}
	writePackageTest(t, xdg, PackageExample)
	project, err = LoadPackages("")
	if err != nil || project.Path != xdg || len(project.Profiles) != 1 {
		t.Fatalf("loaded = %#v, %v", project, err)
	}
}

func TestPackageLocations(t *testing.T) {
	home := t.TempDir()
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			xdg, native, err := packageLocations(goos, home, "", "")
			want := filepath.Join(home, ".config", "conficius", "config.toml")
			if err != nil || xdg != want {
				t.Fatalf("xdg = %s, %v", xdg, err)
			}
			switch goos {
			case "darwin":
				want = filepath.Join(home, "Library", "Application Support", "conficius", "config.toml")
			case "windows":
				want = filepath.Join(home, "AppData", "Roaming", "conficius", "config.toml")
			}
			if native != want {
				t.Fatalf("native = %s, want %s", native, want)
			}
		})
	}
}

func TestPackageDiscoveryConflictsAndErrors(t *testing.T) {
	home := t.TempDir()
	xdg, native, _ := packageLocations("darwin", home, "", "")
	writePackageTest(t, native, PackageExample)
	if path, err := discoverPackages(xdg, native); err != nil || path != native {
		t.Fatalf("native discovery = %s, %v", path, err)
	}
	writePackageTest(t, xdg, PackageExample)
	if _, err := discoverPackages(xdg, native); err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("conflict = %v", err)
	}
	if _, err := LoadPackages(xdg); err != nil {
		t.Fatalf("explicit bypasses conflict: %v", err)
	}
	// ENOTDIR is not absence, and must propagate from Lstat.
	if _, err := discoverPackages(filepath.Join(xdg, "config.toml"), native); err == nil {
		t.Fatal("ignored Lstat error")
	}
	if err := os.Remove(xdg); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "absent"), xdg); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverPackages(xdg, native); err == nil {
		t.Fatal("dangling symlink must count toward conflict")
	}
}

func TestLoadPackagesRejectsInvalidSchema(t *testing.T) {
	xdg, _ := packageTestHome(t)
	for _, data := range []string{
		"version = 1", "version = 3", "layout = 'xdg'", "version = 2\nlayout = 'other'",
		"version = 2\n[tools]", "version = 2\nunknown = true",
		"version = 2\n[profiles.default]\nunknown = true",
		"version = 2\n[profiles.default]\nmode = 'other'",
		"version = 2\nprofile = 'bad name'",
		"version = 2\n[profiles.'bad name']\nmode = 'denylist'",
		"version = 2\n[[vulnerabilities.mappings]]\nmanager = 'npm'",
	} {
		t.Run(data, func(t *testing.T) {
			writePackageTest(t, xdg, data)
			if _, err := LoadPackages(xdg); err == nil {
				t.Fatal("invalid package config accepted")
			}
		})
	}
}

func TestPackageLayoutMovePreservesPathsAndModes(t *testing.T) {
	home := t.TempDir()
	xdg, native, _ := packageLocations("darwin", home, "", "")
	writePackageTest(t, xdg, `version = 2
profile = "work"
[profiles.work]
[profiles.work.managers.npm]
enabled = false
[[profiles.work.projects]]
name = "app"
path = "../../app"
allow_manifest_changes = true
[profiles.explicit]
[profiles.explicit.managers.npm]
enabled = true
`)
	before, err := LoadPackages(xdg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planPackageLayout("os-native", xdg, native)
	if err != nil || plan.Source != xdg || plan.Destination != native || plan.Description == "" {
		t.Fatalf("plan = %#v, %v", plan, err)
	}
	if _, err := os.Stat(native); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview wrote destination: %v", err)
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(xdg); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source remains: %v", err)
	}
	after, err := LoadPackages(native)
	if err != nil {
		t.Fatal(err)
	}
	if after.Layout != "os-native" || after.Profile != "work" || len(after.Profiles) != 2 || after.Profiles["work"].Managers["npm"].Enabled == nil || *after.Profiles["work"].Managers["npm"].Enabled || after.Profiles["explicit"].Managers["npm"].Enabled == nil || !*after.Profiles["explicit"].Managers["npm"].Enabled {
		t.Fatalf("metadata changed: %#v", after)
	}
	if after.Profiles["work"].Projects[0].Path != before.Profiles["work"].Projects[0].Path || !after.Profiles["work"].Projects[0].AllowManifestChanges {
		t.Fatal("relative paths changed meaning")
	}
	if err := plan.Apply(); err == nil {
		t.Fatal("reused stale plan accepted")
	}
}

func TestPackageLayoutCreationAndExplicitMode(t *testing.T) {
	xdg, native := packageTestHome(t)
	plan, err := PlanPackageLayout("")
	if err != nil || plan.Source != "" || plan.Destination != xdg || !strings.Contains(plan.String(), "Create") {
		t.Fatalf("creation plan = %#v, %v", plan, err)
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	project, err := LoadPackages("")
	if err != nil || project.Version != CurrentVersion || project.Layout != "xdg" {
		t.Fatalf("created = %#v, %v", project, err)
	}
	// Explicit selection updates stored layout even where both layouts share a path.
	plan, err = PlanPackageLayout("os-native")
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	project, err = LoadPackages(native)
	if err != nil || project.Layout != "os-native" {
		t.Fatalf("explicit layout = %#v, %v", project, err)
	}
	plan, err = PlanPackageLayout("")
	if err != nil || plan.Destination != native {
		t.Fatalf("stored layout ignored: %#v, %v", plan, err)
	}
	if _, err := PlanPackageLayout("invalid"); err == nil {
		t.Fatal("invalid layout accepted")
	}
}

func TestPackageLayoutRefusesOverwriteAndStaleSource(t *testing.T) {
	for _, mutation := range []string{"destination", "contents", "inode", "symlink"} {
		t.Run(mutation, func(t *testing.T) {
			home := t.TempDir()
			xdg, native, _ := packageLocations("darwin", home, "", "")
			writePackageTest(t, xdg, PackageExample)
			plan, err := planPackageLayout("os-native", xdg, native)
			if err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "destination":
				writePackageTest(t, native, "keep destination")
			case "contents":
				mustWrite(t, xdg, PackageExample+"# changed\n")
			case "inode", "symlink":
				if err := os.Rename(xdg, xdg+".old"); err != nil {
					t.Fatal(err)
				}
				if mutation == "inode" {
					mustWrite(t, xdg, PackageExample)
				} else if err := os.Symlink(xdg+".old", xdg); err != nil {
					t.Fatal(err)
				}
			}
			if err := plan.Apply(); err == nil {
				t.Fatal("unsafe apply succeeded")
			}
			if _, err := os.Lstat(xdg); err != nil {
				t.Fatalf("source lost: %v", err)
			}
			if mutation == "destination" {
				data, err := os.ReadFile(native)
				if err != nil || string(data) != "keep destination" {
					t.Fatalf("destination overwritten: %q, %v", data, err)
				}
			}
		})
	}
}

func TestPackageLayoutCreationNoOverwrite(t *testing.T) {
	xdg, _ := packageTestHome(t)
	plan, err := PlanPackageLayout("")
	if err != nil {
		t.Fatal(err)
	}
	writePackageTest(t, xdg, "keep")
	if err := plan.Apply(); err == nil {
		t.Fatal("creation overwrote existing file")
	}
	if runtime.GOOS != "windows" {
		// A symlink source is readable but never safe to relocate.
		if err := os.Remove(xdg); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(filepath.Dir(xdg), "missing"), xdg); err != nil {
			t.Fatal(err)
		}
		if _, err := PlanPackageLayout(""); err == nil {
			t.Fatal("symlink relocation accepted")
		}
	}
}

func TestPackageLayoutRevalidatesBothLocations(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, mode := range []string{"xdg", "os-native"} {
			t.Run(mode+"/"+map[bool]string{false: "create", true: "update"}[existing], func(t *testing.T) {
				home := t.TempDir()
				xdg, native, _ := packageLocations("darwin", home, "", "")
				if existing {
					writePackageTest(t, xdg, PackageExample)
				}
				plan, err := planPackageLayout(mode, xdg, native)
				if err != nil {
					t.Fatal(err)
				}
				writePackageTest(t, native, "new alternate config")
				if err := plan.Apply(); err == nil {
					t.Fatal("changed discovery accepted")
				}
				data, err := os.ReadFile(native)
				if err != nil || string(data) != "new alternate config" {
					t.Fatalf("alternate changed: %q, %v", data, err)
				}
				if existing {
					data, err = os.ReadFile(xdg)
					if err != nil || string(data) != PackageExample {
						t.Fatalf("source changed: %q, %v", data, err)
					}
				} else if _, err := os.Lstat(xdg); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("unexpected publication: %v", err)
				}
			})
		}
	}
}

func TestPackageLayoutMovedSourceVerificationAndRecovery(t *testing.T) {
	for _, occupied := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "retain"}[occupied], func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "config.toml")
			writePackageTest(t, source, PackageExample)
			plan, err := planPackageLayout("xdg", source, source)
			if err != nil {
				t.Fatal(err)
			}
			if err := plan.verifySource(source); err != nil {
				t.Fatal(err)
			}
			replacement := filepath.Join(dir, "editor")
			writePackageTest(t, replacement, "editor replacement")
			if err := os.Rename(replacement, source); err != nil {
				t.Fatal(err)
			}
			backup, err := plan.moveSource()
			if err == nil || backup == "" {
				t.Fatalf("unverified replacement accepted: %q, %v", backup, err)
			}
			if occupied {
				writePackageTest(t, source, "new writer")
			}
			err = plan.restoreSource(backup)
			if occupied && err == nil || !occupied && err != nil {
				t.Fatalf("restore error: %v", err)
			}
			want := "editor replacement"
			if occupied {
				want = "new writer"
			}
			data, err := os.ReadFile(source)
			if err != nil || string(data) != want {
				t.Fatalf("source lost: %q, %v", data, err)
			}
			data, err = os.ReadFile(backup)
			if err != nil || string(data) != "editor replacement" {
				t.Fatalf("backup lost: %q, %v", data, err)
			}
		})
	}
}

func TestPackageLayoutRetainsRecoveryBackup(t *testing.T) {
	for _, inPlace := range []bool{false, true} {
		t.Run(map[bool]string{false: "move", true: "in-place"}[inPlace], func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "config.toml")
			destination := filepath.Join(dir, "native", "config.toml")
			if inPlace {
				destination = source
			}
			writePackageTest(t, source, PackageExample)
			plan, err := planPackageLayout("os-native", source, destination)
			if err != nil {
				t.Fatal(err)
			}
			if err := plan.Apply(); err != nil {
				t.Fatal(err)
			}
			backups, err := filepath.Glob(filepath.Join(dir, ".conficius-layout-backup-*"))
			if err != nil || len(backups) != 1 {
				t.Fatalf("backups: %v, %v", backups, err)
			}
			data, err := os.ReadFile(backups[0])
			if err != nil || string(data) != PackageExample {
				t.Fatalf("original lost: %q, %v", data, err)
			}
			data, err = os.ReadFile(destination)
			if err != nil || !strings.Contains(string(data), `layout = "os-native"`) {
				t.Fatalf("publication missing: %q, %v", data, err)
			}
		})
	}
}

func TestPackageLayoutSyncFailureRetainsBackup(t *testing.T) {
	for _, inPlace := range []bool{false, true} {
		t.Run(map[bool]string{false: "move", true: "in-place"}[inPlace], func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "config.toml")
			destination := filepath.Join(dir, "native", "config.toml")
			if inPlace {
				destination = source
			}
			writePackageTest(t, source, PackageExample)
			plan, err := planPackageLayout("os-native", source, destination)
			if err != nil {
				t.Fatal(err)
			}
			syncErr := errors.New("directory sync failed")
			err = plan.apply(func(string) error { return syncErr })
			if !errors.Is(err, syncErr) || !strings.Contains(err.Error(), "backup retained") {
				t.Fatalf("sync failure missing: %v", err)
			}
			backups, err := filepath.Glob(filepath.Join(dir, ".conficius-layout-backup-*"))
			if err != nil || len(backups) != 1 {
				t.Fatalf("backups: %v, %v", backups, err)
			}
			data, err := os.ReadFile(backups[0])
			if err != nil || string(data) != PackageExample {
				t.Fatalf("backup lost: %q, %v", data, err)
			}
			if _, err := os.Stat(destination); err != nil {
				t.Fatalf("published config lost: %v", err)
			}
		})
	}
}

func TestProfilesRejectRemovedFields(t *testing.T) {
	packageTestHome(t)
	fields := map[string]string{
		"mode":                    "[profiles.work]\nmode = 'denylist'",
		"security_scope":          "[profiles.work]\nsecurity_scope = 'all'",
		"manager_order":           "[profiles.work]\nmanager_order = ['npm']",
		"packages":                "[[profiles.work.packages]]\nmanager = 'npm'\nname = 'x'\npolicy = 'include'\ndesired_version = '1'",
		"executable":              "[profiles.work.managers.npm]\nexecutable = '/usr/bin/npm'",
		"expected_global_prefix":  "[profiles.work.managers.npm]\nexpected_global_prefix = '/tmp/npm'",
		"include_bump_candidates": "[profiles.work.managers.mise]\ninclude_bump_candidates = true",
		"include_greedy":          "[profiles.work.managers.brew]\ninclude_greedy = true",
		"project mode":            "[[profiles.work.projects]]\nname = 'web'\npath = './web'\nmode = 'denylist'",
		"npm_dependency_classes":  "[[profiles.work.projects]]\nname = 'web'\npath = './web'\nnpm_dependency_classes = ['runtime']",
		"project packages":        "[[profiles.work.projects]]\nname = 'web'\npath = './web'\n[[profiles.work.projects.packages]]\nmanager = 'npm'\nname = 'x'\npolicy = 'include'",
	}
	for name, field := range fields {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			writePackageTest(t, path, "version = 2\n"+field+"\n")
			for _, load := range []func(string) (*Project, error){Load, LoadPackages} {
				if _, err := load(path); err == nil || !strings.Contains(err.Error(), "unknown") {
					t.Fatalf("removed field accepted or wrong error: %v", err)
				}
			}
		})
	}
}
