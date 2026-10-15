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
	if err := os.WriteFile(path, []byte("version = 2\n[profiles.work]\nmode = 'allowlist'\n"), 0o644); err != nil {
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
