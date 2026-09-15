package dotfiles

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinkSelectsEveryLinkForToolAndCreatesRelativeSymlinks(t *testing.T) {
	configDir, homeDir := testDirs(t)
	writeFile(t, filepath.Join(configDir, "zsh", ".zshrc"))
	if err := os.Mkdir(filepath.Join(configDir, "zsh", "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(configDir, "gitconfig"))

	entries := []Entry{
		{Name: "zsh", Source: "zsh/.zshrc", Target: ".config/zsh/.zshrc"},
		{Name: "zsh", Source: "zsh/plugins", Target: ".config/zsh/plugins"},
		{Name: "git", Source: "gitconfig", Target: ".gitconfig"},
	}
	results, err := Link(entries, Options{ConfigDir: configDir, HomeDir: homeDir, Platform: "darwin", Names: []string{"zsh"}, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	for _, result := range results {
		link, err := os.Readlink(result.Target)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.IsAbs(link) {
			t.Fatalf("link %q is absolute", link)
		}
		resolved, err := filepath.EvalSymlinks(result.Target)
		if err != nil {
			t.Fatal(err)
		}
		if !sameFile(t, resolved, result.Source) {
			t.Fatalf("resolved link = %q, want %q", resolved, result.Source)
		}
	}
	if _, err := os.Lstat(filepath.Join(homeDir, ".gitconfig")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("excluded tool target exists: %v", err)
	}
}

func TestInspectAcceptsAbsoluteAndRelativeLinksByResolvedIdentity(t *testing.T) {
	configDir, homeDir := testDirs(t)
	source := filepath.Join(configDir, "source")
	writeFile(t, source)
	absoluteTarget := filepath.Join(homeDir, ".absolute")
	if err := os.Symlink(source, absoluteTarget); err != nil {
		t.Fatal(err)
	}
	relativeTarget := filepath.Join(homeDir, ".config", "relative")
	if err := os.Mkdir(filepath.Dir(relativeTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(filepath.Dir(relativeTarget), source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relative, relativeTarget); err != nil {
		t.Fatal(err)
	}

	results, err := Inspect([]Entry{
		{Name: "absolute", Source: "source", Target: ".absolute"},
		{Name: "relative", Source: "source", Target: ".config/relative"},
	}, Options{ConfigDir: configDir, HomeDir: homeDir})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.State != StateHealthy {
			t.Fatalf("%s state = %q, want %q", result.Name, result.State, StateHealthy)
		}
	}
}

func TestLinkValidatesAllEntriesBeforeMutation(t *testing.T) {
	configDir, homeDir := testDirs(t)
	writeFile(t, filepath.Join(configDir, "valid"))
	entries := []Entry{
		{Name: "valid", Source: "valid", Target: ".valid"},
		{Name: "missing", Source: "missing", Target: ".missing"},
	}

	_, err := Link(entries, Options{ConfigDir: configDir, HomeDir: homeDir, Apply: true})
	if err == nil || !strings.Contains(err.Error(), "source does not exist") {
		t.Fatalf("error = %v, want missing source", err)
	}
	if _, err := os.Lstat(filepath.Join(homeDir, ".valid")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("first target mutated before validation finished: %v", err)
	}
}

func TestLinkRejectsDuplicateTargetsAcrossAndWithinTools(t *testing.T) {
	configDir, homeDir := testDirs(t)
	writeFile(t, filepath.Join(configDir, "one"))
	writeFile(t, filepath.Join(configDir, "two"))

	for _, entries := range [][]Entry{
		{{Name: "same", Source: "one", Target: ".target"}, {Name: "same", Source: "two", Target: ".target"}},
		{{Name: "one", Source: "one", Target: ".target"}, {Name: "two", Source: "two", Target: ".target"}},
	} {
		_, err := Link(entries, Options{ConfigDir: configDir, HomeDir: homeDir})
		if err == nil || !strings.Contains(err.Error(), "same target") {
			t.Fatalf("error = %v, want duplicate target", err)
		}
	}
}

func TestLinkForceOnlyReplacesRepositorySymlink(t *testing.T) {
	configDir, homeDir := testDirs(t)
	writeFile(t, filepath.Join(configDir, "new"))
	outside := filepath.Join(t.TempDir(), "outside")
	writeFile(t, outside)
	target := filepath.Join(homeDir, ".target")

	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
	_, err := Link([]Entry{{Name: "tool", Source: "new", Target: ".target"}}, Options{ConfigDir: configDir, HomeDir: homeDir, Force: true, Apply: true})
	if err == nil || !strings.Contains(err.Error(), "outside current repository") {
		t.Fatalf("error = %v, want outside repository rejection", err)
	}
	if got, _ := os.Readlink(target); got != outside {
		t.Fatalf("outside symlink changed to %q", got)
	}

	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(configDir, "missing-old"), target); err != nil {
		t.Fatal(err)
	}
	if _, err := Link([]Entry{{Name: "tool", Source: "new", Target: ".target"}}, Options{ConfigDir: configDir, HomeDir: homeDir, Force: true, Apply: true}); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if !sameFile(t, resolved, filepath.Join(configDir, "new")) {
		t.Fatalf("resolved target = %q", resolved)
	}
}

func TestApplyRollbackRestoresLinksAndRemovesCreatedDirectories(t *testing.T) {
	configDir, homeDir := testDirs(t)
	oldSource := filepath.Join(configDir, "old")
	newSource := filepath.Join(configDir, "new")
	secondSource := filepath.Join(configDir, "second")
	writeFile(t, oldSource)
	writeFile(t, newSource)
	writeFile(t, secondSource)

	replacedTarget := filepath.Join(homeDir, ".old")
	oldLink := oldSource
	if err := os.Symlink(oldLink, replacedTarget); err != nil {
		t.Fatal(err)
	}
	createdTarget := filepath.Join(homeDir, ".config", "nested", "second")
	items := []prepared{
		{entry: Entry{Name: "first"}, source: newSource, target: replacedTarget, state: StateWrongLink},
		{entry: Entry{Name: "second"}, source: secondSource, target: createdTarget, state: StateMissing},
	}

	ops := osOperations()
	realSymlink := ops.symlink
	calls := 0
	ops.symlink = func(oldname, newname string) error {
		calls++
		if calls == 2 {
			return errors.New("injected failure")
		}
		return realSymlink(oldname, newname)
	}

	if err := apply(items, ops); err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("error = %v, want injected failure", err)
	}
	if got, err := os.Readlink(replacedTarget); err != nil || got != oldLink {
		t.Fatalf("restored symlink = %q, %v; want %q", got, err, oldLink)
	}
	if _, err := os.Lstat(createdTarget); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed target remains: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(homeDir, ".config")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created parent remains: %v", err)
	}
}

func TestApplyDoesNotRemoveTargetCreatedAfterValidation(t *testing.T) {
	configDir, homeDir := testDirs(t)
	source := filepath.Join(configDir, "source")
	target := filepath.Join(homeDir, ".target")
	writeFile(t, source)
	item := prepared{entry: Entry{Name: "tool"}, source: source, target: target, state: StateMissing}

	ops := osOperations()
	ops.symlink = func(_, path string) error {
		if err := os.WriteFile(path, []byte("user data"), 0o600); err != nil {
			t.Fatal(err)
		}
		return errors.New("target appeared")
	}

	if err := apply([]prepared{item}, ops); err == nil {
		t.Fatal("apply succeeded; want error")
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("concurrent target removed: %v", err)
	}
	if string(content) != "user data" {
		t.Fatalf("target content = %q", content)
	}
}

func TestPlatformOmittedMeansAllPlatforms(t *testing.T) {
	configDir, homeDir := testDirs(t)
	writeFile(t, filepath.Join(configDir, "all"))
	writeFile(t, filepath.Join(configDir, "darwin"))
	results, err := Inspect([]Entry{
		{Name: "all", Source: "all", Target: ".all"},
		{Name: "darwin", Source: "darwin", Target: ".darwin", Platforms: []string{"darwin"}},
	}, Options{ConfigDir: configDir, HomeDir: homeDir, Platform: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Name != "all" {
		t.Fatalf("results = %#v", results)
	}
}

func testDirs(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, "repo")
	homeDir := filepath.Join(root, "home")
	if err := os.Mkdir(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(homeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return configDir, homeDir
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sameFile(t *testing.T, left, right string) bool {
	t.Helper()
	leftInfo, err := os.Stat(left)
	if err != nil {
		t.Fatal(err)
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(leftInfo, rightInfo)
}
