package packagemanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func cargoProjectFixture(t *testing.T, entries string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"Cargo.toml": "[package]\nname = 'probe'\nversion = '0.1.0'\nedition = '2024'\n[lib]\npath = 'lib.rs'\n",
		"Cargo.lock": "version = 4\n" + entries,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func cargoProjectEntry(name, version, source string) string {
	return fmt.Sprintf("\n[[package]]\nname = %q\nversion = %q\nsource = %q\n", name, version, source)
}

func cargoProjectFake(t *testing.T, root, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	executable, capture := filepath.Join(dir, "cargo"), filepath.Join(dir, "args")
	writeExecutable(t, executable, fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$PWD|$*\" >> %q\nif [ \"$1\" = locate-project ]; then printf '%%s\\n' %q; exit; fi\n%s\n", capture, filepath.Join(root, "Cargo.toml"), body))
	return executable, capture
}

func TestCargoProjectOutdatedScopedDuplicateVersions(t *testing.T) {
	root := cargoProjectFixture(t, cargoProjectEntry("serde", "1.0.0", cargoProjectRegistry)+cargoProjectEntry("serde", "0.9.0", cargoProjectRegistry)+cargoProjectEntry("gitdep", "1.0.0", "git+https://example.test/repo#abc")+cargoProjectEntry("local", "0.1.0", "")+cargoProjectEntry("custom", "1.0.0", "registry+https://example.test/index"))
	executable, capture := cargoProjectFake(t, root, `case "$*" in
*serde@1.0.0*) printf '%s\n' '     Locking 2 packages to latest compatible versions' '    Updating serde v1.0.0 -> v1.0.1 (available: v2.0.0)' '      Adding helper v0.2.0' '    Removing old-helper v0.1.0' 'warning: not updating lockfile due to dry run' >&2;;
*serde@0.9.0*) printf '%s\n' '     Locking 0 packages to latest compatible versions' '    Removing serde v0.9.0' 'warning: not updating lockfile due to dry run' >&2;;
*) exit 22;;
esac`)
	packages := make(Packages)
	if err := cargoProjectOutdated(context.Background(), executable, Scope{Kind: "workspace", Path: root}, packages); err != nil {
		t.Fatal(err)
	}
	records := Records(packages)
	if len(records) != 1 {
		t.Fatalf("records = %#v", records)
	}
	pkg := records[0]
	if pkg.Name != "serde" || pkg.Current != "1.0.0" || pkg.Latest != "1.0.1" || pkg.Requested != pkg.Latest || pkg.Config != cargoProjectTargetAbsent || pkg.Path != root || pkg.Scope != "workspace" || pkg.Executable != executable {
		t.Fatalf("record = %#v", pkg)
	}
	if got, want := cargoProjectUpdateArgs([]Package{pkg}), []string{"update", "--color", "never", "serde@1.0.0", "--precise", "1.0.1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("update args = %v; want %v", got, want)
	}
	commands, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	want := root + "|locate-project --workspace --message-format plain\n" + root + "|update --color never serde@1.0.0 --dry-run\n" + root + "|update --color never serde@0.9.0 --dry-run\n"
	if string(commands) != want {
		t.Fatalf("commands = %s; want %s", commands, want)
	}
}

func TestCargoProjectUpdateArgs(t *testing.T) {
	for _, pkg := range []Package{
		{Name: "serde", Current: "1.0.0", Latest: "1.0.1", Requested: "1.0.1"},
		{Name: "serde", Current: "0.9.0", Latest: "0.9.1"},
		{Name: "serde", Current: "1.0.0-alpha.1", Latest: "1.0.0+build.2", Requested: "1.0.0+build.2"},
	} {
		if got, want := cargoProjectUpdateArgs([]Package{pkg}), []string{"update", "--color", "never", pkg.Name + "@" + pkg.Current, "--precise", pkg.Latest}; !reflect.DeepEqual(got, want) {
			t.Fatalf("args = %v; want %v", got, want)
		}
	}
	for _, invalid := range [][]Package{
		nil,
		{{Name: "--workspace", Current: "1.0.0", Latest: "1.0.1"}},
		{{Name: "serde", Latest: "1.0.1"}},
		{{Name: "serde@1", Current: "1.0.0", Latest: "1.0.1"}},
		{{Name: "serde", Current: "1.0.0"}},
		{{Name: "serde", Current: "1.0.0", Latest: "1.0.0"}},
		{{Name: "serde", Current: "1.0.0", Latest: "1.0.1", Requested: "1.0.2"}},
		{{Name: "serde", Current: "01.0.0", Latest: "1.0.1"}},
		{{Name: "serde", Current: "1.0.0", Latest: "1.0.1-01"}},
		{{Name: "serde", Current: "1.0.0", Latest: "1.0.1+"}},
		{{Name: "serde", Current: "1.0.0", Latest: "1.0"}},
		{{Name: "serde", Current: "1.0.0", Latest: "1.0.1"}, {Name: "serde", Current: "0.9.0", Latest: "0.9.1"}},
		{{Name: "serde", Current: "1.0.0", Latest: "1.0.1"}, {Name: "serde", Current: "1.0.0", Latest: "1.0.1"}},
	} {
		if got := cargoProjectUpdateArgs(invalid); got != nil {
			t.Fatalf("invalid targets produced %v", got)
		}
	}
}

func TestCargoProjectRejectsUnsafeScopeAndSources(t *testing.T) {
	for _, test := range []string{"child", "manifest-symlink", "lock-symlink", "duplicate-source", "global", "invalid-version", "invalid-name"} {
		t.Run(test, func(t *testing.T) {
			root := cargoProjectFixture(t, cargoProjectEntry("serde", "1.0.0", cargoProjectRegistry))
			scope := Scope{Kind: "project", Path: root}
			switch test {
			case "child":
				scope.Path = cargoProjectFixture(t, "")
			case "global":
				scope.Kind = "global"
			case "invalid-version", "invalid-name":
				name, version := "serde", "1.0.0"
				if test == "invalid-version" {
					version = "1.0.0-01"
				} else {
					name = "--workspace"
				}
				if err := os.WriteFile(filepath.Join(root, "Cargo.lock"), []byte("version = 4\n"+cargoProjectEntry(name, version, cargoProjectRegistry)), 0600); err != nil {
					t.Fatal(err)
				}
			case "duplicate-source":
				f, err := os.OpenFile(filepath.Join(root, "Cargo.lock"), os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteString(cargoProjectEntry("serde", "1.0.0", "registry+https://example.test/index"))
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			case "lock-symlink", "manifest-symlink":
				name := "Cargo.lock"
				if test == "manifest-symlink" {
					name = "Cargo.toml"
				}
				outside := cargoProjectFixture(t, "")
				if err := os.Remove(filepath.Join(root, name)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(outside, name), filepath.Join(root, name)); err != nil {
					t.Fatal(err)
				}
			}
			executable, capture := cargoProjectFake(t, root, "exit 99")
			err := cargoProjectOutdated(context.Background(), executable, scope, make(Packages))
			var op *OperationError
			if !errors.As(err, &op) || op.Category != ErrorParser {
				t.Fatalf("error = %v", err)
			}
			commands, _ := os.ReadFile(capture)
			if strings.Contains(string(commands), "|update") {
				t.Fatalf("unsafe update: %s", commands)
			}
		})
	}
}

func TestCargoProjectTargetFailClosed(t *testing.T) {
	prefix := "Locking 1 package to latest Rust 1.90 compatible version\n"
	suffix := "\nwarning: not updating lockfile due to dry run\n"
	for _, line := range []string{
		"Updating serde v1.0.0 => v1.0.1", "Updating serde v1.0.0 -> v1.0.1 (registry `private`)",
		"Removing serde v1.0.0\nAdding serde v1.0.1", "Downgrading serde v1.0.0 -> v0.9.0",
		"Updating serde v0.9.0 -> v1.0.1", "Updating serde v1.0.0 -> v1.0.1\nUpdating serde v1.0.0 -> v1.0.2",
		"Updating other v1.0.0 -> v1.1.0 (https://example.test/repo)", "something changed", "Adding other v1.0.0 -> v2.0.0",
		"Updating serde v1.0.0 -> v1.0.1-01", "Updating serde v01.0.0 -> v1.0.1", "Adding other v1.0.0-alpha..1",
		"Removing serde v1.0.0\nsomething changed", "Removing serde v1.0.0\nUpdating serde v1.0.0 -> v1.0.1",
		"Updating serde v1.0.0 -> v1.0.1\nRemoving serde v1.0.0", "Removing serde v0.9.0",
	} {
		if _, err := cargoProjectTarget([]byte(prefix+line+suffix), "serde", "1.0.0"); err == nil {
			t.Errorf("accepted %q", line)
		}
	}
	for _, output := range []string{"", prefix, suffix, "Updating serde v1.0.0 -> v1.0.1"} {
		if _, err := cargoProjectTarget([]byte(output), "serde", "1.0.0"); err == nil {
			t.Errorf("accepted incomplete %q", output)
		}
	}
}

func TestCargoProjectTargetDuplicateRemoval(t *testing.T) {
	output := []byte("Locking 0 packages to latest compatible versions\nRemoving serde v0.9.0\nwarning: not updating lockfile due to dry run\n")
	if target, err := cargoProjectTarget(output, "serde", "0.9.0"); err != nil || target != "" {
		t.Fatalf("target = %q, error = %v", target, err)
	}
}

func TestCargoProjectErrorsAndLockIntegrity(t *testing.T) {
	for _, test := range []struct {
		body     string
		category ErrorCategory
	}{
		{"printf 'network connection refused' >&2; exit 101", ErrorNetwork},
		{"printf 'authentication required' >&2; exit 101", ErrorAuthentication},
		{"printf 'HTTP 429' >&2; exit 101", ErrorRateLimit},
		{"printf 'registry unavailable' >&2; exit 101", ErrorRegistry},
		{"printf 'unexpected output' >&2", ErrorParser},
		{"printf 'changed' > Cargo.lock", ErrorParser},
	} {
		root := cargoProjectFixture(t, cargoProjectEntry("serde", "1.0.0", cargoProjectRegistry))
		executable, _ := cargoProjectFake(t, root, test.body)
		packages := make(Packages)
		err := cargoProjectOutdated(context.Background(), executable, Scope{Kind: "project", Path: root}, packages)
		var op *OperationError
		if !errors.As(err, &op) || op.Category != test.category || len(packages) != 0 {
			t.Errorf("error = %v, packages = %v; want %s", err, packages, test.category)
		}
	}
}

func TestCargoProjectInstalledCargoDryRunDoesNotWrite(t *testing.T) {
	executable, err := exec.LookPath("cargo")
	if err != nil {
		t.Skip("cargo unavailable")
	}
	root := cargoProjectFixture(t, "")
	manifest := "[package]\nname = 'probe'\nversion = '0.1.0'\nedition = '2024'\n[lib]\npath = 'lib.rs'\n"
	if err := os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lib.rs"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	lock := "version = 4\n[[package]]\nname = 'probe'\nversion = '0.1.0'\n"
	if err := os.WriteFile(filepath.Join(root, "Cargo.lock"), []byte(lock), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := cargoProjectDryRun(context.Background(), executable, root, []string{"update", "--dry-run", "--color", "never", "--offline"})
	if err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	if target, err := cargoProjectTarget(output, "probe", "0.1.0"); err != nil || target != "" {
		t.Fatalf("target = %q, error = %v, output = %s", target, err, output)
	}
	contents, err := os.ReadFile(filepath.Join(root, "Cargo.lock"))
	if err != nil || string(contents) != lock {
		t.Fatalf("lock changed: %s, %v", contents, err)
	}
	if err := cargoProjectOutdated(context.Background(), executable, Scope{Kind: "project", Path: root}, make(Packages)); err != nil {
		t.Fatal(err)
	}
}

func cargoProjectStateWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCargoProjectStateBinding(t *testing.T) {
	for _, input := range []string{"manifest", "lock", "local-config", "ancestor-config", "home-config", "legacy-config", "environment", "toolchain", "config-identity"} {
		t.Run(input, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "project")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"Cargo.toml", "Cargo.lock"} {
				data, err := os.ReadFile(filepath.Join(cargoProjectFixture(t, ""), name))
				if err != nil {
					t.Fatal(err)
				}
				cargoProjectStateWrite(t, filepath.Join(root, name), string(data))
			}
			home := t.TempDir()
			t.Setenv("CARGO_HOME", home)
			config := filepath.Join(root, ".cargo", "config.toml")
			if input == "config-identity" {
				cargoProjectStateWrite(t, config, "[net]\noffline = true\n")
			}
			before, err := cargoProjectState(root)
			if err != nil {
				t.Fatal(err)
			}
			if repeat, err := cargoProjectState(root); err != nil || repeat != before {
				t.Fatalf("unstable state: %s, %v", repeat, err)
			}
			switch input {
			case "manifest":
				cargoProjectStateWrite(t, filepath.Join(root, "Cargo.toml"), "[package]\nname = 'changed'\nversion = '0.1.0'\n")
			case "lock":
				cargoProjectStateWrite(t, filepath.Join(root, "Cargo.lock"), "version = 3\n")
			case "local-config":
				cargoProjectStateWrite(t, config, "[net]\noffline = true\n")
			case "ancestor-config":
				cargoProjectStateWrite(t, filepath.Join(parent, ".cargo", "config.toml"), "[net]\noffline = true\n")
			case "home-config", "legacy-config":
				name := "config.toml"
				if input == "legacy-config" {
					name = "config"
				}
				cargoProjectStateWrite(t, filepath.Join(home, name), "[net]\noffline = true\n")
			case "environment":
				t.Setenv("CARGO_REGISTRIES_TEST_TOKEN", "secret-value")
			case "toolchain":
				t.Setenv("RUSTUP_TOOLCHAIN", "test-toolchain")
			case "config-identity":
				other := filepath.Join(t.TempDir(), "config.toml")
				cargoProjectStateWrite(t, other, "[net]\noffline = true\n")
				if err := os.Remove(config); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, config); err != nil {
					t.Fatal(err)
				}
			}
			after, err := cargoProjectState(root)
			if err != nil || after == before || len(after) != 64 {
				t.Fatalf("state not bound to %s: %s, %v", input, after, err)
			}
		})
	}
}

func TestCargoProjectStateRejectsUnsupportedInputs(t *testing.T) {
	for _, manifest := range []string{
		"[workspace]\nmembers = []\n",
		"[package]\nworkspace = '..'\n",
		"[dependencies]\nlocal = { path = '../local' }\n",
		"[target.'cfg(unix)'.build-dependencies]\nlocal = { path = 'local' }\n",
		"[patch.crates-io]\nlocal = { path = '../local' }\n",
	} {
		t.Run(manifest, func(t *testing.T) {
			root := cargoProjectFixture(t, "")
			cargoProjectStateWrite(t, filepath.Join(root, "Cargo.toml"), manifest)
			if _, err := cargoProjectState(root); err == nil {
				t.Fatal("unsupported manifest accepted")
			}
			executable, capture := cargoProjectFake(t, root, "exit 99")
			if err := cargoProjectOutdated(t.Context(), executable, Scope{Kind: "project", Path: root}, make(Packages)); err == nil {
				t.Fatal("unsupported manifest discovered")
			}
			if commands, _ := os.ReadFile(capture); len(commands) != 0 {
				t.Fatalf("command ran: %s", commands)
			}
		})
	}
}

func TestCargoValidateProjectPlan(t *testing.T) {
	for _, change := range []string{"none", "Cargo.toml", "Cargo.lock", "workspace-root", "ancestor-workspace"} {
		t.Run(change, func(t *testing.T) {
			root := cargoProjectFixture(t, "")
			t.Setenv("CARGO_HOME", t.TempDir())
			executable, _ := cargoProjectFake(t, root, "exit 99")
			plan := UpdatePlan{Scope: "project", Directory: root, Executable: executable}
			if err := cargoValidateProjectPlan(t.Context(), plan); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "Cargo.toml", "Cargo.lock":
				path := filepath.Join(root, change)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				other := filepath.Join(t.TempDir(), change)
				cargoProjectStateWrite(t, other, string(data))
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, path); err != nil {
					t.Fatal(err)
				}
			case "workspace-root":
				plan.Executable, _ = cargoProjectFake(t, cargoProjectFixture(t, ""), "exit 99")
			case "ancestor-workspace":
				cargoProjectStateWrite(t, filepath.Join(root, "Cargo.toml"), "[workspace]\nmembers = ['child']\n")
				child := filepath.Join(root, "child")
				cargoProjectStateWrite(t, filepath.Join(child, "Cargo.toml"), "[package]\nname = 'child'\nversion = '0.1.0'\n")
				cargoProjectStateWrite(t, filepath.Join(child, "Cargo.lock"), "version = 4\n")
				plan.Directory = child
			}
			if err := cargoValidateProjectPlan(t.Context(), plan); (err == nil) != (change == "none") {
				t.Fatalf("validation for %s: %v", change, err)
			}
		})
	}
}
