package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDotfilesLinkDefaultsToPreviewAndAppliesAllLinksForNamedTool(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	writeCLIFile(t, filepath.Join(root, "zshrc"), "zsh")
	writeCLIFile(t, filepath.Join(root, "gitconfig"), "git")
	configPath := filepath.Join(root, "conficius.toml")
	writeCLIFile(t, configPath, `version = 1
[tools.zsh]
links = [
  { source = "zshrc", target = "$HOME/.zshrc" },
  { source = "zshrc", target = "$HOME/.config/zsh/.zshrc" },
]
[tools.git]
links = [{ source = "gitconfig", target = "$HOME/.gitconfig" }]
`)

	var output bytes.Buffer
	command := New(&output, "test")
	args := []string{appName, "--config", configPath, "dotfiles", "link", "zsh"}
	if err := command.Execute(args); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) {
		t.Fatalf("preview mutated target: %v", err)
	}
	if strings.Count(output.String(), "[create] zsh") != 2 {
		t.Fatalf("preview output = %q", output.String())
	}

	output.Reset()
	command = New(&output, "test")
	args = []string{appName, "--config", configPath, "dotfiles", "link", "--apply", "zsh"}
	if err := command.Execute(args); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{filepath.Join(home, ".zshrc"), filepath.Join(home, ".config", "zsh", ".zshrc")} {
		if info, err := os.Lstat(target); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("target %q is not symlink: %v", target, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, ".gitconfig")); !os.IsNotExist(err) {
		t.Fatalf("unselected tool linked: %v", err)
	}
}

func TestDotfilesAuditJSONAndStrict(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("ZDOTDIR", "")
	writeCLIFile(t, filepath.Join(root, "zshrc"), "zsh")
	configPath := filepath.Join(root, "conficius.toml")
	writeCLIFile(t, configPath, `version = 1
[tools.zsh]
links = [{ source = "zshrc", target = "$HOME/.zshrc" }]
`)

	var output bytes.Buffer
	var errorOutput bytes.Buffer
	command := NewWithWriters(&output, &errorOutput, "test")
	err := command.Execute([]string{appName, "--config", configPath, "dotfiles", "audit", "--xdg", "--json", "--strict"})
	if err == nil {
		t.Fatal("strict audit succeeded with warnings")
	}
	var document struct {
		SchemaVersion int `json:"schema_version"`
		Data          struct {
			Findings []struct {
				Code string `json:"code"`
			} `json:"findings"`
		} `json:"data"`
	}
	jsonText := output.String()
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatalf("JSON output = %q: %v; stderr = %q", jsonText, err, errorOutput.String())
	}
	if !strings.Contains(errorOutput.String(), "dotfile audit found warnings or errors") {
		t.Fatalf("stderr = %q", errorOutput.String())
	}
	if document.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d", document.SchemaVersion)
	}
	codes := make(map[string]bool)
	for _, finding := range document.Data.Findings {
		codes[finding.Code] = true
	}
	for _, code := range []string{"xdg_config_home_unset", "zdotdir_unset", "dotfile_missing"} {
		if !codes[code] {
			t.Fatalf("missing finding %q in %#v", code, codes)
		}
	}
}

func writeCLIFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
