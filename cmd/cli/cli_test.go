package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/config"
)

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
