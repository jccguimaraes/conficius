package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/config"
)

func TestWisdomCommand(t *testing.T) {
	var output bytes.Buffer
	if err := New(&output, "test").Execute([]string{appName, "wisdom"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output.String(), "Conficius says: ") || strings.Count(output.String(), "\n") != 1 {
		t.Fatalf("unexpected wisdom: %q", output.String())
	}
}

func TestInitCommandDefaultPath(t *testing.T) {
	workingDirectory := t.TempDir()
	withWorkingDirectory(t, workingDirectory)

	var output bytes.Buffer
	cli := New(&output, "test")
	if err := cli.Execute([]string{appName, "init"}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(workingDirectory, config.FileName)
	if _, err := config.Load(path); err != nil {
		t.Fatalf("generated config: %v", err)
	}
	if !strings.Contains(output.String(), path) {
		t.Fatalf("output = %q; want created path", output.String())
	}
	if err := cli.Execute([]string{appName, "dotfiles", "link"}); err != nil {
		t.Fatalf("link preview after init: %v", err)
	}
}

func TestInitCommandExplicitRootConfigPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.toml")
	var output bytes.Buffer
	cli := New(&output, "test")
	if err := cli.Execute([]string{appName, "--config", path, "init"}); err != nil {
		t.Fatal(err)
	}

	if _, err := config.Load(path); err != nil {
		t.Fatalf("generated config: %v", err)
	}
}

func TestConfigResolveJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("version = 2\n[profiles.work]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cli := New(&output, "test")
	if err := cli.Execute([]string{appName, "--config", path, "config", "resolve", "--profile", "work", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"name": "work"`) || !strings.Contains(output.String(), `"schema_version": 1`) {
		t.Fatalf("output = %q", output.String())
	}
}

func TestConfigMigratePreview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("version = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cli := New(&output, "test")
	if err := cli.Execute([]string{appName, "--config", path, "config", "migrate"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "version = 2") {
		t.Fatalf("output = %q", output.String())
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "version = 1\n" {
		t.Fatalf("config changed: %q, %v", contents, err)
	}
}

func TestInitCommandRefusesExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.toml")
	if err := os.WriteFile(path, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	cli := New(&output, "test")
	if err := cli.Execute([]string{appName, "--config", path, "init"}); err == nil {
		t.Fatal("init succeeded; want error")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "keep me" {
		t.Fatalf("existing config changed to %q", contents)
	}
}

func TestPackageCommandsIgnoreLegacyDotfilesConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv(config.ProfileEnvironment, "")
	t.Setenv("PATH", t.TempDir())
	withWorkingDirectory(t, t.TempDir())
	writeCLIFile(t, config.FileName, "not valid TOML = [")
	plan, err := config.PlanPackageLayout("xdg")
	if err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, plan.Destination, "version = 2\n[profiles.work]\n")

	for _, args := range [][]string{
		{appName, "config", "resolve", "--profile", "work", "--json"},
		{appName, "packages", "doctor", "--profile", "work", "--pm", "npm", "--json"},
	} {
		var output bytes.Buffer
		if err := New(&output, "test").Execute(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestPackageCommandsRejectExplicitDotfilesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dotfiles.toml")
	writeCLIFile(t, path, "version = 2\n[tools.zsh]\nlinks = []\n")
	for _, args := range [][]string{
		{appName, "--config", path, "config", "resolve"},
		{appName, "--config", path, "packages", "doctor", "--pm", "npm"},
		{appName, "--config", path, "packages", "vulnerabilities", "--pm", "npm"},
	} {
		var output bytes.Buffer
		if err := New(&output, "test").Execute(args); err == nil {
			t.Fatalf("%v: accepted dotfiles config", args)
		}
	}
}

func TestPackageCommandsMissingImplicitConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv(config.ProfileEnvironment, "")
	t.Setenv("PATH", t.TempDir())
	withWorkingDirectory(t, t.TempDir())
	for _, args := range [][]string{
		{appName, "config", "resolve", "--json"},
		{appName, "packages", "doctor", "--pm", "npm", "--json"},
	} {
		var output bytes.Buffer
		if err := New(&output, "test").Execute(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestConfigLayoutPreviewAndApply(t *testing.T) {
	for _, mode := range []string{"", "xdg", "os-native"} {
		t.Run("mode="+mode, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			withWorkingDirectory(t, t.TempDir())
			selectedMode := mode
			if selectedMode == "" {
				selectedMode = "xdg"
			}
			plan, err := config.PlanPackageLayout(selectedMode)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{appName, "config", "layout"}
			if mode != "" {
				args = append(args, "--"+mode)
			}
			var output bytes.Buffer
			if err := New(&output, "test").Execute(args); err != nil {
				t.Fatal(err)
			}
			if got := output.String(); !strings.Contains(got, "Create package configuration") || !strings.Contains(got, "Source:") || !strings.Contains(got, "Destination: "+plan.Destination) || !strings.Contains(got, "Preview only") {
				t.Fatalf("preview = %q", got)
			}
			if _, err := os.Stat(plan.Destination); !os.IsNotExist(err) {
				t.Fatalf("preview destination: %v", err)
			}
			output.Reset()
			if err := New(&output, "test").Execute(append(args, "--apply")); err != nil {
				t.Fatal(err)
			}
			if _, err := config.LoadPackages(plan.Destination); err != nil {
				t.Fatalf("applied package config: %v", err)
			}
		})
	}
}

func TestConfigLayoutMovePreviewAndApply(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	withWorkingDirectory(t, t.TempDir())
	xdg, err := config.PlanPackageLayout("xdg")
	if err != nil {
		t.Fatal(err)
	}
	contents := "version = 2\nlayout = 'xdg'\n[profiles.work]\n"
	writeCLIFile(t, xdg.Destination, contents)
	native, err := config.PlanPackageLayout("os-native")
	if err != nil {
		t.Fatal(err)
	}
	if native.Source == native.Destination {
		t.Skip("OS-native and XDG package directories coincide")
	}
	var output bytes.Buffer
	args := []string{appName, "config", "layout", "--os-native"}
	if err := New(&output, "test").Execute(args); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "Move package configuration") || !strings.Contains(got, "Source: "+native.Source) || !strings.Contains(got, "Destination: "+native.Destination) {
		t.Fatalf("preview = %q", got)
	}
	if data, err := os.ReadFile(native.Source); err != nil || string(data) != contents {
		t.Fatalf("preview changed source: %q, %v", data, err)
	}
	if _, err := os.Stat(native.Destination); !os.IsNotExist(err) {
		t.Fatalf("preview destination: %v", err)
	}
	if err := New(&output, "test").Execute(append(args, "--apply")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(native.Source); !os.IsNotExist(err) {
		t.Fatalf("move source remains: %v", err)
	}
	project, err := config.LoadPackages(native.Destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.ResolveProfile("work", false); err != nil {
		t.Fatalf("moved profile: %v", err)
	}
}

func TestConfigLayoutPreservesPersistedMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	withWorkingDirectory(t, t.TempDir())
	plan, err := config.PlanPackageLayout("os-native")
	if err != nil {
		t.Fatal(err)
	}
	contents := "version = 2\nlayout = 'os-native'\n"
	writeCLIFile(t, plan.Destination, contents)
	for _, apply := range []bool{false, true} {
		args := []string{appName, "config", "layout"}
		if apply {
			args = append(args, "--apply")
		}
		var output bytes.Buffer
		if err := New(&output, "test").Execute(args); err != nil {
			t.Fatal(err)
		}
		if got := output.String(); !strings.Contains(got, "Destination: "+plan.Destination) {
			t.Fatalf("preview = %q", got)
		}
		if !apply {
			if data, err := os.ReadFile(plan.Destination); err != nil || string(data) != contents {
				t.Fatalf("preview changed source: %q, %v", data, err)
			}
		}
		project, err := config.LoadPackages(plan.Destination)
		if err != nil {
			t.Fatal(err)
		}
		if project.Layout != "os-native" {
			t.Fatalf("layout = %q, want os-native", project.Layout)
		}
	}
}

func TestConfigLayoutRejectsExplicitConfig(t *testing.T) {
	for _, path := range []string{"", filepath.Join(t.TempDir(), "packages.toml")} {
		for _, apply := range []bool{false, true} {
			args := []string{appName, "--config", path, "config", "layout"}
			if apply {
				args = append(args, "--apply")
			}
			var output bytes.Buffer
			err := New(&output, "test").Execute(args)
			if err == nil || !strings.Contains(err.Error(), "does not support --config; use the central package config") {
				t.Fatalf("%v: error = %v", args, err)
			}
			if strings.Contains(output.String(), "Destination:") {
				t.Fatalf("rejected layout produced plan: %q", output.String())
			}
		}
	}
}

func TestConfigLayoutRejectsConflictingModes(t *testing.T) {
	var output bytes.Buffer
	if err := New(&output, "test").Execute([]string{appName, "config", "layout", "--xdg", "--os-native"}); err == nil {
		t.Fatal("accepted conflicting layout modes")
	}
}

func withWorkingDirectory(t *testing.T, directory string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}

func TestPrintResolvedProfileDescribesManagersAndProjects(t *testing.T) {
	enabled, disabled := true, false
	resolved := config.ResolvedProfile{Name: "work", Profile: config.Profile{
		Managers: map[string]config.ManagerSettings{"npm": {Enabled: &enabled}, "brew": {Enabled: &disabled}, "mise": {}},
		Projects: []config.ProfileProject{{Name: "app", Path: "./app", ResolvedPath: "/work/app", AllowManifestChanges: true}},
	}}
	var output bytes.Buffer
	if err := printResolvedProfile(&output, resolved, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Profile: work", "Manager brew: disabled\nManager mise: default\nManager npm: enabled", "Project app: ./app (resolved: /work/app; allow manifest changes: true)"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q: %s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "Mode:") {
		t.Fatalf("policy mode in output: %s", output.String())
	}
}
