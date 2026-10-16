package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverPrecedence(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	child := filepath.Join(project, "nested")
	xdg := filepath.Join(root, "xdg")
	mustMkdirAll(t, child)
	mustMkdirAll(t, filepath.Join(xdg, "conficius"))

	projectConfig := filepath.Join(project, FileName)
	xdgConfig := filepath.Join(xdg, "conficius", FileName)
	explicit := filepath.Join(root, "explicit.toml")
	for _, path := range []string{projectConfig, xdgConfig, explicit} {
		mustWrite(t, path, "version = 1\n")
	}

	path, err := discover("", child, xdg)
	if err != nil || path != projectConfig {
		t.Fatalf("parent discovery = %q, %v; want %q", path, err, projectConfig)
	}

	path, err = discover(explicit, child, xdg)
	if err != nil || path != explicit {
		t.Fatalf("explicit discovery = %q, %v; want %q", path, err, explicit)
	}

	if err := os.Remove(projectConfig); err != nil {
		t.Fatal(err)
	}
	path, err = discover("", child, xdg)
	if err != nil || path != xdgConfig {
		t.Fatalf("XDG discovery = %q, %v; want %q", path, err, xdgConfig)
	}
}

func TestLoadDiscoveredKeepsProfilesIsolated(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	projectRoot := filepath.Join(home, "project")
	mustMkdirAll(t, filepath.Join(xdg, "conficius"))
	mustMkdirAll(t, projectRoot)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	globalPath := filepath.Join(xdg, "conficius", FileName)
	mustWrite(t, globalPath, `version = 2
[profiles.work.managers.npm]
enabled = true
[profiles.global]
`)
	mustWrite(t, filepath.Join(projectRoot, FileName), `version = 2
profile = "work"
[profiles.work.managers.brew]
enabled = false
`)
	withDirectory(t, projectRoot)
	for _, explicit := range []string{"", filepath.Join(projectRoot, FileName)} {
		project, err := LoadDiscovered(explicit)
		if err != nil {
			t.Fatal(err)
		}
		if len(project.Profiles) != 1 || len(project.Profiles["work"].Managers) != 1 {
			t.Fatalf("profiles leaked from global config: %#v", project.Profiles)
		}
		if enabled := project.Profiles["work"].Managers["brew"].Enabled; enabled == nil || *enabled {
			t.Fatalf("local manager settings = %#v", project.Profiles["work"].Managers)
		}
	}
	mustWrite(t, globalPath, "invalid TOML")
	if _, err := LoadDiscovered(""); err != nil {
		t.Fatalf("unrelated global config affected local config: %v", err)
	}
}

func TestDiscoverExplicitDoesNotRequireExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.toml")
	got, err := discover(path, t.TempDir(), t.TempDir())
	if err != nil || got != path {
		t.Fatalf("discover = %q, %v; want %q", got, err, path)
	}
}

func TestLoadNormalizesProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	root := t.TempDir()
	path := filepath.Join(root, FileName)
	mustWrite(t, path, `version = 1

[tools.zsh]
links = [
  { source = "dotfiles/../dotfiles/zshrc", target = "${XDG_CONFIG_HOME}/zsh/.zshrc", platforms = [" Linux ", "DARWIN"] },
]
`)

	project, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	link := project.Tools["zsh"].Links[0]
	if project.Root != root {
		t.Fatalf("root = %q; want %q", project.Root, root)
	}
	if link.Source != filepath.Join(root, "dotfiles", "zshrc") {
		t.Fatalf("source = %q", link.Source)
	}
	if link.Target != filepath.Join(home, ".config", "zsh", ".zshrc") {
		t.Fatalf("target = %q", link.Target)
	}
	if got := strings.Join(link.Platforms, ","); got != "linux,darwin" {
		t.Fatalf("platforms = %q", got)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	t.Setenv("CONFICIUS_TEST_MISSING", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := map[string]string{
		"missing version": `
[tools.zsh]
links = [{ source = "zshrc", target = "$HOME/.zshrc" }]
`,
		"future version": `version = 3`,
		"unknown root field": `version = 1
extra = true
`,
		"unknown link field": `version = 1
[tools.zsh]
links = [{ source = "zshrc", target = "$HOME/.zshrc", extra = true }]
`,
		"source outside root": `version = 1
[tools.zsh]
links = [{ source = "../zshrc", target = "$HOME/.zshrc" }]
`,
		"target outside home": `version = 1
[tools.zsh]
links = [{ source = "zshrc", target = "$HOME/../zshrc" }]
`,
		"unsupported expansion": `version = 1
[tools.zsh]
links = [{ source = "zshrc", target = "~/.zshrc" }]
`,
		"unknown variable": `version = 1
[tools.zsh]
links = [{ source = "zshrc", target = "$CONFICIUS_TEST_MISSING/.zshrc" }]
`,
	}

	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, FileName)
			mustWrite(t, path, contents)
			if _, err := Load(path); err == nil {
				t.Fatal("Load succeeded; want error")
			}
		})
	}
}

func TestLoadNormalizesVulnerabilityMappings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	path := filepath.Join(root, FileName)
	mustWrite(t, path, `version = 1
[[vulnerabilities.mappings]]
manager = " MISE "
package = " node "
ecosystem = "npm"
name = "node"
`)

	project, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	mapping := project.Vulnerabilities.Mappings[0]
	if mapping.Manager != "mise" || mapping.Package != "node" || mapping.Name != "node" {
		t.Fatalf("mapping = %#v", mapping)
	}
}

func TestLoadRejectsInvalidVulnerabilityMapping(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, FileName)
	mustWrite(t, path, `version = 1
[[vulnerabilities.mappings]]
manager = "npm"
package = "left-pad"
ecosystem = "npm"
name = "left-pad"
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unsupported manager") {
		t.Fatalf("Load error = %v", err)
	}
}

func TestLoadRejectsSourceSymlinkOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	mustWrite(t, outside, "outside")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, FileName)
	mustWrite(t, path, `version = 1
[tools.zsh]
links = [{ source = "link", target = "$HOME/.zshrc" }]
`)
	if _, err := Load(path); err == nil {
		t.Fatal("Load succeeded; want symlink escape error")
	}
}

func TestLoadVersionTwoProfiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(ProfileEnvironment, "")
	root := t.TempDir()
	path := filepath.Join(root, FileName)
	mustWrite(t, path, `version = 2
profile = "work"
[profiles.work.managers.npm]
enabled = true
[profiles.work.managers.brew]
enabled = false
[profiles.work.managers.mise]
[[profiles.work.projects]]
name = "web"
path = "./web"
allow_manifest_changes = true
[profiles.personal.managers.cargo]
enabled = true
[[profiles.personal.projects]]
name = "web"
path = "./personal"
`)
	project, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := project.ResolveProfile("", true)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Name != "work" || resolved.Source != path || resolved.Provenance["config"] != path {
		t.Fatalf("resolved = %#v", resolved)
	}
	managers := resolved.Profile.Managers
	if len(managers) != 3 || managers["npm"].Enabled == nil || !*managers["npm"].Enabled || managers["brew"].Enabled == nil || *managers["brew"].Enabled || managers["mise"].Enabled != nil {
		t.Fatalf("managers = %#v", managers)
	}
	if len(resolved.Profile.Projects) != 1 {
		t.Fatalf("projects = %#v", resolved.Profile.Projects)
	}
	web := resolved.Profile.Projects[0]
	if web.Path != filepath.Join(root, "web") || web.ResolvedPath != web.Path || !web.AllowManifestChanges {
		t.Fatalf("project = %#v", web)
	}
	personal, err := project.ResolveProfile("personal", false)
	if err != nil || len(personal.Profile.Managers) != 1 || personal.Profile.Projects[0].Path != filepath.Join(root, "personal") || personal.Profile.Projects[0].AllowManifestChanges {
		t.Fatalf("personal = %#v, %v", personal, err)
	}
}

func TestResolveProfileSelectionPrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, FileName)
	mustWrite(t, path, `version = 2
profile = "project"
[profiles.project]
[profiles.environment]
[profiles.command]
`)
	project, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(ProfileEnvironment, "environment")
	resolved, err := project.ResolveProfile("command", false)
	if err != nil || resolved.Name != "command" {
		t.Fatalf("command profile = %#v, %v", resolved, err)
	}
	resolved, err = project.ResolveProfile("", false)
	if err != nil || resolved.Name != "environment" {
		t.Fatalf("environment profile = %#v, %v", resolved, err)
	}
	t.Setenv(ProfileEnvironment, "")
	resolved, err = project.ResolveProfile("", false)
	if err != nil || resolved.Name != "project" {
		t.Fatalf("project profile = %#v, %v", resolved, err)
	}
}

func TestVersionOneRejectsNamedProfileSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), FileName)
	mustWrite(t, path, "version = 1\n")
	project, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(ProfileEnvironment, "work")
	if _, err := project.ResolveProfile("", false); err == nil || !strings.Contains(err.Error(), "config version 2") {
		t.Fatalf("ResolveProfile error = %v", err)
	}
}

func TestLoadRejectsInvalidProfiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tests := map[string]string{
		"unknown field":        "version = 2\n[profiles.default]\nexlcude = true\n",
		"invalid name":         "version = 2\n[profiles.Upper]\n",
		"unsupported manager":  "version = 2\n[profiles.default.managers.unknown]\nenabled=true\n",
		"duplicate project":    "version = 2\n[[profiles.default.projects]]\nname='web'\npath='./web'\n[[profiles.default.projects]]\nname='web'\npath='./other'\n",
		"duplicate path":       "version = 2\n[[profiles.default.projects]]\nname='web'\npath='./web'\n[[profiles.default.projects]]\nname='other'\npath='./web/../web'\n",
		"empty path":           "version = 2\n[[profiles.default.projects]]\nname='web'\npath=''\n",
		"invalid project name": "version = 2\n[[profiles.default.projects]]\nname='Web'\npath='./web'\n",
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			mustWrite(t, path, contents)
			if _, err := Load(path); err == nil {
				t.Fatal("Load succeeded; want error")
			}
		})
	}
}

func TestInitWritesLoadableExampleAndRefusesExisting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(t.TempDir(), "project.toml")

	got, err := Init(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("Init path = %q; want %q", got, path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "# Conficius") {
		t.Fatal("example has no comments")
	}
	project, err := Load(path)
	if err != nil {
		t.Fatalf("generated config is invalid: %v", err)
	}
	if len(project.Tools) != 0 || len(project.Vulnerabilities.Mappings) != 0 || len(project.Profiles["default"].Managers) != 0 {
		t.Fatalf("generated config enables optional examples: %+v", project)
	}
	if project.Version != CurrentVersion || len(project.Profiles) != 0 {
		t.Fatalf("generated config defaults: %+v", project)
	}
	if _, err := Init(path); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second Init error = %v", err)
	}
}

func TestDiscoverNotFound(t *testing.T) {
	_, err := discover("", t.TempDir(), t.TempDir())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v; want ErrNotFound", err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsDuplicateNormalizedTargets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, links := range []string{
		`[tools.a]
links = [{ source = "one", target = "$HOME/nested/../target" }, { source = "two", target = "$HOME/target" }]`,
		`[tools.a]
links = [{ source = "one", target = "$HOME/target", platforms = ["linux"] }]
[tools.b]
links = [{ source = "two", target = "$HOME/./target", platforms = ["darwin"] }]`,
	} {
		root := t.TempDir()
		path := filepath.Join(root, FileName)
		mustWrite(t, path, "version = 1\n"+links)
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "same target") {
			t.Fatalf("error = %v, want duplicate target rejection", err)
		}
	}
}
