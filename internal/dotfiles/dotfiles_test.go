package dotfiles

import (
	"errors"
	"fmt"
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
	items, err := prepare([]Entry{
		{Name: "first", Source: newSource, Target: replacedTarget},
		{Name: "second", Source: secondSource, Target: createdTarget},
	}, Options{ConfigDir: configDir, HomeDir: homeDir})
	if err != nil {
		t.Fatal(err)
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
	items, err := prepare([]Entry{{Name: "tool", Source: source, Target: target}}, Options{ConfigDir: configDir, HomeDir: homeDir})
	if err != nil {
		t.Fatal(err)
	}

	ops := osOperations()
	ops.symlink = func(_, path string) error {
		if err := os.WriteFile(path, []byte("user data"), 0o600); err != nil {
			t.Fatal(err)
		}
		return errors.New("target appeared")
	}

	if err := apply(items, ops); err == nil {
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

func TestInspectRepairEligibility(t *testing.T) {
	configDir, homeDir := testDirs(t)
	writeFile(t, filepath.Join(configDir, "source"))
	writeFile(t, filepath.Join(homeDir, "file"))
	for name, destination := range map[string]string{"owned": filepath.Join(configDir, "old"), "outside": filepath.Join(t.TempDir(), "old"), "healthy": filepath.Join(configDir, "source")} {
		if err := os.Symlink(destination, filepath.Join(homeDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]bool{"missing": true, "owned": true, "outside": false, "healthy": false, "file": false} {
		results, err := Inspect([]Entry{{Name: name, Source: "source", Target: name}}, Options{ConfigDir: configDir, HomeDir: homeDir})
		if err != nil || results[0].Repairable != want {
			t.Fatalf("%s: results = %+v, error = %v", name, results, err)
		}
	}
}

func TestLinkRejectsStalePreview(t *testing.T) {
	for _, change := range []string{"source removed", "source outside", "target file", "wrong destination", "parent outside"} {
		t.Run(change, func(t *testing.T) {
			configDir, homeDir := testDirs(t)
			source := filepath.Join(configDir, "source")
			target := filepath.Join(homeDir, "parent", "target")
			writeFile(t, source)
			if err := os.Mkdir(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if change == "wrong destination" {
				if err := os.Symlink(filepath.Join(configDir, "old-one"), target); err != nil {
					t.Fatal(err)
				}
			}
			entries := []Entry{{Name: "tool", Source: source, Target: target}}
			options := Options{ConfigDir: configDir, HomeDir: homeDir, Force: true}
			preview, err := Link(entries, options)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "source removed", "source outside":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
				if change == "source outside" {
					outside := filepath.Join(t.TempDir(), "source")
					writeFile(t, outside)
					if err := os.Symlink(outside, source); err != nil {
						t.Fatal(err)
					}
				}
			case "target file":
				writeFile(t, target)
			case "wrong destination":
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(configDir, "old-two"), target); err != nil {
					t.Fatal(err)
				}
			case "parent outside":
				if err := os.Remove(filepath.Dir(target)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), filepath.Dir(target)); err != nil {
					t.Fatal(err)
				}
			}
			options.Apply = true
			options.Preview = preview
			if _, err := Link(entries, options); err == nil {
				t.Fatal("stale preview accepted")
			}
			if change == "wrong destination" {
				if got, err := os.Readlink(target); err != nil || got != filepath.Join(configDir, "old-two") {
					t.Fatalf("changed symlink overwritten: %q, %v", got, err)
				}
			}
		})
	}
}

func TestApplyTypedRollbackError(t *testing.T) {
	for _, rollbackFails := range []bool{false, true} {
		t.Run(fmt.Sprint(rollbackFails), func(t *testing.T) {
			configDir, homeDir := testDirs(t)
			writeFile(t, filepath.Join(configDir, "source"))
			items, err := prepare([]Entry{{Name: "a", Source: "source", Target: "a"}, {Name: "b", Source: "source", Target: "b"}}, Options{ConfigDir: configDir, HomeDir: homeDir})
			if err != nil {
				t.Fatal(err)
			}
			cause := errors.New("injected create failure")
			rollbackCause := errors.New("injected rollback failure")
			ops := osOperations()
			symlink := ops.symlink
			ops.symlink = func(source, target string) error {
				if target == items[1].target {
					return cause
				}
				return symlink(source, target)
			}
			if rollbackFails {
				ops.remove = func(string) error { return rollbackCause }
			}
			err = apply(items, ops)
			var failure *ApplyError
			if !errors.As(err, &failure) || !errors.Is(err, cause) || failure.Action != "create symlink" || failure.Path != items[1].target || !failure.RollbackAttempted {
				t.Fatalf("failure = %+v, error = %v", failure, err)
			}
			if len(failure.AffectedPaths) != 1 || failure.AffectedPaths[0] != items[0].target || (failure.RollbackError != nil) != rollbackFails || errors.Is(err, rollbackCause) != rollbackFails {
				t.Fatalf("rollback metadata = %+v", failure)
			}
		})
	}
}

func TestApplyRevalidatesBeforeMutation(t *testing.T) {
	for _, change := range []string{"source", "target", "parent"} {
		t.Run(change, func(t *testing.T) {
			configDir, homeDir := testDirs(t)
			writeFile(t, filepath.Join(configDir, "source"))
			items, err := prepare([]Entry{{Name: "tool", Source: "source", Target: "parent/target"}}, Options{ConfigDir: configDir, HomeDir: homeDir})
			if err != nil {
				t.Fatal(err)
			}
			ops := osOperations()
			mkdir := ops.mkdir
			ops.mkdir = func(path string, mode os.FileMode) error {
				if err := mkdir(path, mode); err != nil {
					return err
				}
				switch change {
				case "source":
					if err := os.Remove(items[0].source); err != nil {
						t.Fatal(err)
					}
				case "target":
					writeFile(t, items[0].target)
				case "parent":
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(t.TempDir(), path); err != nil {
						t.Fatal(err)
					}
				}
				return nil
			}
			err = apply(items, ops)
			var failure *ApplyError
			if !errors.As(err, &failure) || failure.Action != "revalidate" || !failure.RollbackAttempted {
				t.Fatalf("error = %v", err)
			}
			if change == "target" {
				if _, err := os.ReadFile(items[0].target); err != nil {
					t.Fatalf("regular file removed: %v", err)
				}
			}
		})
	}
}

func TestTargetSnapshotRejectsSameStateChanges(t *testing.T) {
	for _, change := range []string{"identity", "destination"} {
		for _, preview := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/preview=%t", change, preview), func(t *testing.T) {
				configDir, homeDir := testDirs(t)
				writeFile(t, filepath.Join(configDir, "source"))
				writeFile(t, filepath.Join(configDir, "old"))
				writeFile(t, filepath.Join(configDir, "other"))
				middle := filepath.Join(configDir, "middle")
				if err := os.Symlink("old", middle); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(homeDir, "target")
				if err := os.Symlink(middle, target); err != nil {
					t.Fatal(err)
				}
				entries := []Entry{{Name: "a", Source: "source", Target: "target"}, {Name: "b", Source: "source", Target: "untouched/target"}}
				options := Options{ConfigDir: configDir, HomeDir: homeDir, Force: true}
				items, err := prepare(entries, options)
				if err != nil {
					t.Fatal(err)
				}
				results, err := Inspect(entries, options)
				if err != nil {
					t.Fatal(err)
				}
				if change == "identity" {
					// Retain old inode so replacement identity differs deterministically.
					if err := os.Rename(target, target+".old"); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(middle, target); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Remove(middle); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("other", middle); err != nil {
						t.Fatal(err)
					}
				}
				if preview {
					options.Apply, options.Preview = true, results
					_, err = Link(entries, options)
				} else {
					err = apply(items, osOperations())
				}
				if err == nil || !strings.Contains(err.Error(), "stale dotfile preview") {
					t.Fatalf("error = %v", err)
				}
				if _, err := os.Lstat(filepath.Join(homeDir, "untouched")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("mutation before validation: %v", err)
				}
				if text, err := os.Readlink(target); err != nil || text != middle {
					t.Fatalf("target changed: %q, %v", text, err)
				}
			})
		}
	}
}

func TestRegularSwapBeforeCaptureIsPreserved(t *testing.T) {
	for _, rollbackSwap := range []bool{false, true} {
		t.Run(fmt.Sprint(rollbackSwap), func(t *testing.T) {
			configDir, homeDir := testDirs(t)
			writeFile(t, filepath.Join(configDir, "source"))
			target := filepath.Join(homeDir, "a")
			if !rollbackSwap {
				if err := os.Symlink(filepath.Join(configDir, "old"), target); err != nil {
					t.Fatal(err)
				}
			}
			items, err := prepare([]Entry{{Name: "a", Source: "source", Target: "a"}, {Name: "b", Source: "source", Target: "b"}}, Options{ConfigDir: configDir, HomeDir: homeDir})
			if err != nil {
				t.Fatal(err)
			}
			ops := osOperations()
			readlink := ops.readlink
			swapped := false
			ops.readlink = func(path string) (string, error) {
				text, err := readlink(path)
				if path == target && !swapped && err == nil {
					swapped = true
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte("user data"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				return text, err
			}
			if rollbackSwap {
				symlink := ops.symlink
				ops.symlink = func(source, path string) error {
					if path == items[1].target {
						return errors.New("injected failure")
					}
					return symlink(source, path)
				}
			}
			err = apply(items, ops)
			if err == nil || !strings.Contains(err.Error(), "captured object retained at") {
				t.Fatalf("error = %v", err)
			}
			if rollbackSwap {
				var failure *ApplyError
				if !errors.As(err, &failure) || failure.RollbackError == nil {
					t.Fatalf("rollback claimed complete: %v", err)
				}
			}
			content, err := os.ReadFile(target)
			if err != nil || string(content) != "user data" {
				t.Fatalf("regular target lost: %q, %v", content, err)
			}
		})
	}
}

func TestRollbackNonemptyDirectoryReportsPath(t *testing.T) {
	configDir, homeDir := testDirs(t)
	writeFile(t, filepath.Join(configDir, "source"))
	items, err := prepare([]Entry{{Name: "tool", Source: "source", Target: "parent/target"}}, Options{ConfigDir: configDir, HomeDir: homeDir})
	if err != nil {
		t.Fatal(err)
	}
	ops := osOperations()
	ops.symlink = func(_, target string) error {
		writeFile(t, filepath.Join(filepath.Dir(target), "user-file"))
		return errors.New("injected failure")
	}
	err = apply(items, ops)
	var failure *ApplyError
	parent := filepath.Dir(items[0].target)
	if !errors.As(err, &failure) || failure.RollbackError == nil || !strings.Contains(failure.RollbackError.Error(), parent) {
		t.Fatalf("rollback claimed complete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, "user-file")); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureDoesNotOverwriteConcurrentRestoreTarget(t *testing.T) {
	_, homeDir := testDirs(t)
	target := filepath.Join(homeDir, "target")
	if err := os.Symlink("old", target); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	ops := osOperations()
	rename := ops.rename
	var captured string
	ops.rename = func(source, destination string) error {
		if err := os.Remove(source); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source, []byte("captured data"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := rename(source, destination); err != nil {
			return err
		}
		captured = destination
		return os.WriteFile(source, []byte("new data"), 0o600)
	}
	err = removeLink(target, info, "old", ops)
	if err == nil || !strings.Contains(err.Error(), captured) {
		t.Fatalf("error = %v", err)
	}
	for path, want := range map[string]string{target: "new data", captured: "captured data"} {
		content, err := os.ReadFile(path)
		if err != nil || string(content) != want {
			t.Fatalf("%s: got %q, %v; want %q", path, content, err, want)
		}
	}
}
