package packagemanager

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const cargoGlobalFixture = `{"installs":{"demo 1.0.0 (registry+https://github.com/rust-lang/crates.io-index)":{"version_req":null,"bins":["demo","helper"],"features":["tls","demo/extra"],"all_features":true,"no_default_features":true,"profile":"release","target":"aarch64-apple-darwin","rustc":"rustc 1.99.0"}}}`

func cargoGlobalTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CARGO_HOME", home)
	t.Setenv("CARGO_INSTALL_ROOT", "")
	for _, entry := range os.Environ() {
		key := strings.SplitN(entry, "=", 2)[0]
		if strings.HasPrefix(key, "CARGO_SOURCE_") || strings.HasPrefix(key, "CARGO_REGISTRIES_CRATES_IO_") {
			t.Setenv(key, "")
			t.Skip("registry override environment unsupported")
		}
	}
	return home
}

func cargoGlobalTestWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func cargoGlobalTestOwned(t *testing.T, root, contents string) {
	t.Helper()
	var tracking struct {
		Installs map[string]cargoGlobalInstall `json:"installs"`
	}
	if err := json.Unmarshal([]byte(contents), &tracking); err != nil {
		t.Fatal(err)
	}
	owned := "[v1]\n"
	for id, install := range tracking.Installs {
		bins, err := json.Marshal(install.Bins)
		if err != nil {
			t.Fatal(err)
		}
		owned += fmt.Sprintf("%s = %s\n", strconvQuote(id), bins)
	}
	cargoGlobalTestWrite(t, filepath.Join(root, ".crates.toml"), owned)
}

func TestCargoValidateGlobalPlan(t *testing.T) {
	for _, name := range []string{"unchanged", "removed bin", "features", "current version", "removed package", "root", "config root", "source override", "registry override", "environment override", "arguments", "encoded settings", "invalid config", "multiple packages"} {
		t.Run(name, func(t *testing.T) {
			home := cargoGlobalTestHome(t)
			cargoGlobalTestOwned(t, home, cargoGlobalFixture)
			cargoGlobalTestWrite(t, filepath.Join(home, ".crates2.json"), cargoGlobalFixture)
			installed, err := cargoGlobalTracking(home)
			if err != nil {
				t.Fatal(err)
			}
			pkg := installed["demo"]
			pkg.Latest = "2.0.0"
			plan := UpdatePlan{Manager: PM_CARGO, Scope: "global", Packages: []Package{pkg}, Args: cargoGlobalUpdateArgs([]Package{pkg})}
			contents := cargoGlobalFixture
			switch name {
			case "removed bin":
				contents = strings.Replace(contents, `["demo","helper"]`, `["demo"]`, 1)
			case "features":
				contents = strings.Replace(contents, `["tls","demo/extra"]`, `["tls"]`, 1)
			case "current version":
				contents = strings.Replace(contents, "1.0.0", "1.1.0", 1)
			case "removed package":
				contents = `{"installs":{}}`
			case "root":
				t.Setenv("CARGO_INSTALL_ROOT", t.TempDir())
			case "config root":
				cargoGlobalTestWrite(t, filepath.Join(home, "config.toml"), "[install]\nroot = "+strconvQuote(t.TempDir())+"\n")
			case "source override":
				cargoGlobalTestWrite(t, filepath.Join(home, "config.toml"), "[source.crates-io]\nreplace-with = 'mirror'\n")
			case "registry override":
				cargoGlobalTestWrite(t, filepath.Join(home, "config.toml"), "[registry]\nindex = 'https://example.test/index'\n")
			case "environment override":
				t.Setenv("CARGO_SOURCE_CRATES_IO_REPLACE_WITH", "mirror")
			case "arguments":
				plan.Args = append(plan.Args, "--force")
			case "encoded settings":
				plan.Packages[0].Config = strings.Replace(pkg.Config, `"all_features":true`, `"all_features":false`, 1)
				plan.Args = cargoGlobalUpdateArgs(plan.Packages)
			case "invalid config":
				plan.Packages[0].Config = "{"
			case "multiple packages":
				plan.Packages = append(plan.Packages, pkg)
			}
			cargoGlobalTestOwned(t, home, contents)
			cargoGlobalTestWrite(t, filepath.Join(home, ".crates2.json"), contents)
			err = cargoValidateGlobalPlan(plan)
			if name == "unchanged" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "stale") {
				t.Fatalf("expected stale plan rejection, got %v", err)
			}
		})
	}
}

func TestCargoValidateGlobalPlanSymlinkRoot(t *testing.T) {
	home := cargoGlobalTestHome(t)
	root := t.TempDir()
	link := filepath.Join(home, "root")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv("CARGO_INSTALL_ROOT", link)
	cargoGlobalTestOwned(t, root, cargoGlobalFixture)
	cargoGlobalTestWrite(t, filepath.Join(root, ".crates2.json"), cargoGlobalFixture)
	installed, err := cargoGlobalTracking(link)
	if err != nil {
		t.Fatal(err)
	}
	pkg := installed["demo"]
	pkg.Latest = "2.0.0"
	plan := UpdatePlan{Manager: PM_CARGO, Scope: "global", Packages: []Package{pkg}, Args: cargoGlobalUpdateArgs([]Package{pkg})}
	if err := cargoValidateGlobalPlan(plan); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	cargoGlobalTestOwned(t, other, cargoGlobalFixture)
	cargoGlobalTestWrite(t, filepath.Join(other, ".crates2.json"), cargoGlobalFixture)
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	if err := cargoValidateGlobalPlan(plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("accepted retargeted root: %v", err)
	}
}

func TestCargoGlobalTrackingAbsentRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	packages, err := cargoGlobalTracking(root)
	if err != nil {
		t.Fatal(err)
	}
	if packages == nil || len(packages) != 0 {
		t.Fatalf("expected empty map for absent root, got %v", packages)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("expected root to remain absent, got %v", err)
	}
}

func TestCargoGlobalTrackingAndArgs(t *testing.T) {
	home := cargoGlobalTestHome(t)
	cargoGlobalTestOwned(t, home, cargoGlobalFixture)
	cargoGlobalTestWrite(t, filepath.Join(home, ".crates2.json"), cargoGlobalFixture)
	packages, err := cargoGlobalTracking(home)
	if err != nil {
		t.Fatal(err)
	}
	pkg := packages["demo"]
	pkg.Latest = "2.0.0"
	root, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"install", "demo", "--version", "2.0.0", "--registry", "crates-io", "--root", root, "--profile", "release", "--target", "aarch64-apple-darwin", "--bin", "demo", "--bin", "helper", "--features", "tls", "--features", "demo/extra", "--all-features", "--no-default-features"}
	if got := cargoGlobalUpdateArgs([]Package{pkg}); !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q; want %q", got, want)
	}
	if cargoGlobalUpdateArgs([]Package{pkg, pkg}) != nil {
		t.Fatal("accepted multiple crates")
	}
	pkg.Config = strings.Replace(pkg.Config, `"demo","helper"`, `"--force","helper"`, 1)
	if cargoGlobalUpdateArgs([]Package{pkg}) != nil {
		t.Fatal("accepted injected bin")
	}
}

func TestCargoGlobalTrackingInvalid(t *testing.T) {
	for _, contents := range []string{"", "{", "null", `{}`, `{"installs":null}`, strings.Replace(cargoGlobalFixture, `"all_features":true,`, "", 1), strings.Replace(cargoGlobalFixture, `"target":"aarch64-apple-darwin"`, `"target":null`, 1), strings.Replace(cargoGlobalFixture, `"features":["tls","demo/extra"]`, `"features":null`, 1), strings.Replace(cargoGlobalFixture, `"profile":"release"`, `"profile":"--debug"`, 1)} {
		t.Run(contents, func(t *testing.T) {
			home := t.TempDir()
			cargoGlobalTestOwned(t, home, cargoGlobalFixture)
			cargoGlobalTestWrite(t, filepath.Join(home, ".crates2.json"), contents)
			if _, err := cargoGlobalTracking(home); err == nil {
				t.Fatal("accepted invalid metadata")
			}
		})
	}
}

func TestCargoGlobalSourcesAndDefaults(t *testing.T) {
	home := t.TempDir()
	for _, source := range []string{"git+https://example.test/demo#abcdef", "path+file:///tmp/demo", "registry+https://example.test/index"} {
		contents := strings.Replace(cargoGlobalFixture, "registry+https://github.com/rust-lang/crates.io-index", source, 1)
		cargoGlobalTestOwned(t, home, contents)
		cargoGlobalTestWrite(t, filepath.Join(home, ".crates2.json"), contents)
		packages, err := cargoGlobalTracking(home)
		if err != nil || len(packages) != 0 {
			t.Fatalf("source %s: %v %v", source, packages, err)
		}
	}
	contents := strings.ReplaceAll(cargoGlobalFixture, `:true`, `:false`)
	contents = strings.Replace(contents, `["tls","demo/extra"]`, `[]`, 1)
	contents = strings.Replace(contents, `"profile":"release"`, `"profile":"dev"`, 1)
	cargoGlobalTestOwned(t, home, contents)
	cargoGlobalTestWrite(t, filepath.Join(home, ".crates2.json"), contents)
	packages, err := cargoGlobalTracking(home)
	if err != nil {
		t.Fatal(err)
	}
	pkg := packages["demo"]
	pkg.Latest = "1.1.0"
	args := cargoGlobalUpdateArgs([]Package{pkg})
	if args == nil || strings.Contains(strings.Join(args, " "), "--all-features") || strings.Contains(strings.Join(args, " "), "--no-default-features") || !strings.Contains(strings.Join(args, " "), "--profile dev") {
		t.Fatalf("defaults: %q", args)
	}
}

func TestCargoGlobalRoot(t *testing.T) {
	home := cargoGlobalTestHome(t)
	if root, err := cargoGlobalRoot(); err != nil || root != home {
		t.Fatalf("default %s %v", root, err)
	}
	root := t.TempDir()
	cargoGlobalTestWrite(t, filepath.Join(home, "config.toml"), "[install]\nroot = "+strconvQuote(root)+"\n")
	if got, err := cargoGlobalRoot(); err != nil || got != root {
		t.Fatalf("config %s %v", got, err)
	}
	explicit := t.TempDir()
	t.Setenv("CARGO_INSTALL_ROOT", explicit)
	if got, err := cargoGlobalRoot(); err != nil || got != explicit {
		t.Fatalf("env %s %v", got, err)
	}
	t.Setenv("CARGO_INSTALL_ROOT", "relative")
	if _, err := cargoGlobalRoot(); err == nil {
		t.Fatal("accepted relative root")
	}
	t.Setenv("CARGO_INSTALL_ROOT", explicit)
	cargoGlobalTestWrite(t, filepath.Join(home, "config.toml"), "[source.crates-io]\nreplace-with = 'mirror'\n")
	if _, err := cargoGlobalRoot(); err == nil {
		t.Fatal("accepted source replacement")
	}
}

func strconvQuote(value string) string { encoded, _ := json.Marshal(value); return string(encoded) }

func TestCargoGlobalInfoVersion(t *testing.T) {
	for _, output := range []string{"demo\nversion: 2.0.0\n", "demo #cli #tool\nDescription\nversion: 2.0.0 (latest 3.0.0)\n", "demo\nversion: 2.0.0 (latest: 3.0.0)\n"} {
		got, err := cargoGlobalInfoVersion([]byte(output), "demo")
		if err != nil || got != "2.0.0" {
			t.Fatalf("%q: %s %v", output, got, err)
		}
	}
	for _, output := range []string{"other\nversion: 2.0.0", "demo\nversion: --force", "demo\nversion: 2.0", "demo\nversion: 2.0.0\nversion: 3.0.0", "demo\nDescription", "demo\nversion: 2.0.0 (latest: invalid)"} {
		if _, err := cargoGlobalInfoVersion([]byte(output), "demo"); err == nil {
			t.Fatalf("accepted %q", output)
		}
	}
}

func TestCargoGlobalOutdated(t *testing.T) {
	home := cargoGlobalTestHome(t)
	cargoGlobalTestOwned(t, home, cargoGlobalFixture)
	cargoGlobalTestWrite(t, filepath.Join(home, ".crates2.json"), cargoGlobalFixture)
	original := commandContext
	t.Cleanup(func() { commandContext = original })
	calls := 0
	commandContext = func(ctx context.Context, executable string, args ...string) *exec.Cmd {
		calls++
		if executable != "cargo-test" || !reflect.DeepEqual(args, []string{"info", "demo", "--registry", "crates-io", "--color", "never"}) {
			t.Fatalf("unsafe command %s %q", executable, args)
		}
		return exec.CommandContext(ctx, "/usr/bin/printf", "demo #cli\nversion: 2.0.0 (latest: 3.0.0)\n")
	}
	packages := make(Packages)
	if err := cargoGlobalOutdated(context.Background(), "cargo-test", Scope{Kind: "global"}, packages); err != nil {
		t.Fatal(err)
	}
	records := Records(packages)
	if calls != 1 || len(records) != 1 || records[0].Config == "" || records[0].Latest != "2.0.0" {
		t.Fatalf("records %+v, calls %d", records, calls)
	}
	if cargoGlobalUpdateArgs(records) == nil {
		t.Fatal("record lost install metadata")
	}
}

func TestCargoGlobalTrackingReconcile(t *testing.T) {
	for _, tc := range []struct {
		name     string
		owned    string
		options  string
		wantBins []string
		wantErr  string
	}{
		{name: "fresh"},
		{name: "empty v1", owned: "[v1]\n"},
		{name: "missing options", owned: "[v1]\n'demo 1.0.0 (registry+https://github.com/rust-lang/crates.io-index)' = ['demo']\n", wantErr: "read Cargo install options"},
		{name: "stale options", options: cargoGlobalFixture},
		{name: "owned subset", owned: "[v1]\n'demo 1.0.0 (registry+https://github.com/rust-lang/crates.io-index)' = ['helper']\n", options: cargoGlobalFixture, wantBins: []string{"helper"}},
		{name: "unowned install", owned: "[v1]\n'demo 1.0.0 (registry+https://github.com/rust-lang/crates.io-index)' = []\n", options: cargoGlobalFixture},
		{name: "missing entry", owned: "[v1]\n'demo 1.0.0 (registry+https://github.com/rust-lang/crates.io-index)' = ['demo']\n", options: `{"installs":{}}`, wantErr: "missing Cargo install options"},
		{name: "version mismatch", owned: "[v1]\n'demo 2.0.0 (registry+https://github.com/rust-lang/crates.io-index)' = ['demo']\n", options: cargoGlobalFixture, wantErr: "missing Cargo install options"},
		{name: "bin mismatch", owned: "[v1]\n'demo 1.0.0 (registry+https://github.com/rust-lang/crates.io-index)' = ['missing']\n", options: cargoGlobalFixture, wantErr: "missing install options"},
		{name: "invalid owned file", owned: "[v1", options: cargoGlobalFixture, wantErr: "decode Cargo owned installs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.owned != "" {
				cargoGlobalTestWrite(t, filepath.Join(home, ".crates.toml"), tc.owned)
			}
			if tc.options != "" {
				cargoGlobalTestWrite(t, filepath.Join(home, ".crates2.json"), tc.options)
			}
			packages, err := cargoGlobalTracking(home)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v; want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantBins == nil {
				if len(packages) != 0 {
					t.Fatalf("unexpected packages: %+v", packages)
				}
				return
			}
			var config cargoGlobalConfig
			if err := json.Unmarshal([]byte(packages["demo"].Config), &config); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(config.Install.Bins, tc.wantBins) {
				t.Fatalf("bins = %q; want %q", config.Install.Bins, tc.wantBins)
			}
		})
	}
}

func TestCargoGlobalUnknownTarget(t *testing.T) {
	for _, target := range []string{"", `,"target":null`, `,"target":""`} {
		t.Run(target, func(t *testing.T) {
			home := t.TempDir()
			cargoGlobalTestOwned(t, home, cargoGlobalFixture)
			contents := strings.Replace(cargoGlobalFixture, `,"target":"aarch64-apple-darwin"`, target, 1)
			cargoGlobalTestWrite(t, filepath.Join(home, ".crates2.json"), contents)
			if _, err := cargoGlobalTracking(home); err == nil || !strings.Contains(err.Error(), "unknown Cargo install target") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCargoGlobalBinTarget(t *testing.T) {
	for _, tc := range []struct{ bin, goos, want string }{
		{"demo.exe", "windows", "demo"},
		{"demo", "windows", "demo"},
		{"demo.exe", "linux", "demo.exe"},
		{"demo.exe", "darwin", "demo.exe"},
		{"--force.exe", "windows", "--force"},
		{"../demo.exe", "windows", "../demo"},
	} {
		got := cargoGlobalBinTarget(tc.bin, tc.goos)
		if got != tc.want {
			t.Fatalf("bin %q on %s = %q; want %q", tc.bin, tc.goos, got, tc.want)
		}
		if tc.goos == "windows" && tc.want != "demo" && cargoGlobalName.MatchString(got) {
			t.Fatalf("unsafe bin accepted: %q", got)
		}
	}
}

func TestCargoGlobalRegistryConfig(t *testing.T) {
	for _, tc := range []struct {
		contents string
		wantErr  bool
	}{
		{"[registry]\ndefault = 'private'\n", false},
		{"[registry]\ndefault = 'crates-io'\n", false},
		{"[registry]\nindex = 'https://example.test/index'\n", true},
		{"[registries.crates-io]\nindex = 'https://example.test/index'\n", true},
		{"[registries.private]\nindex = 'https://example.test/index'\n", false},
	} {
		t.Run(tc.contents, func(t *testing.T) {
			home := cargoGlobalTestHome(t)
			cargoGlobalTestWrite(t, filepath.Join(home, "config.toml"), tc.contents)
			if _, err := cargoGlobalRoot(); (err != nil) != tc.wantErr {
				t.Fatalf("error = %v; want error %v", err, tc.wantErr)
			}
		})
	}
}
