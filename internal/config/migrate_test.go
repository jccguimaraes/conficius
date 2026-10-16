package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMigratePreviewAndApply(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, FileName)
	original := "# keep in backup\nversion = 1\n"
	mustWrite(t, path, original)
	now := time.Date(2026, 9, 15, 14, 0, 0, 0, time.UTC)

	preview, backup, err := Migrate(path, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if backup != "" || !strings.Contains(string(preview), "version = 2") {
		t.Fatalf("preview = %q, backup = %q", preview, backup)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || string(unchanged) != original {
		t.Fatalf("dry run changed config: %q, %v", unchanged, err)
	}

	_, backup, err = Migrate(path, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if backup != path+".backup-20260915T140000Z" {
		t.Fatalf("backup = %q", backup)
	}
	backupContents, err := os.ReadFile(backup)
	if err != nil || string(backupContents) != original {
		t.Fatalf("backup contents = %q, %v", backupContents, err)
	}
	project, err := Load(path)
	if err != nil || project.Version != 2 {
		t.Fatalf("migrated project = %#v, %v", project, err)
	}
	resolved, err := project.ResolveProfile("default", false)
	if err != nil || len(resolved.Profile.Managers) != 0 || len(resolved.Profile.Projects) != 0 {
		t.Fatalf("migrated default profile = %#v, %v", resolved, err)
	}
	for _, field := range []string{"mode =", "manager_order", "security_scope", "desired_version"} {
		if strings.Contains(string(preview), field) {
			t.Fatalf("migration emitted removed field %q", field)
		}
	}
}

func TestMigrateUsesCollisionSafeBackup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, FileName)
	mustWrite(t, path, "version = 1\n")
	now := time.Date(2026, 9, 15, 14, 0, 0, 0, time.UTC)
	mustWrite(t, path+".backup-20260915T140000Z", "existing")
	_, backup, err := Migrate(path, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if backup != path+".backup-20260915T140000Z-1" {
		t.Fatalf("backup = %q", backup)
	}
}

func TestMigrateRefusesSymlink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	target := filepath.Join(root, "target.toml")
	path := filepath.Join(root, FileName)
	mustWrite(t, target, "version = 1\n")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Migrate(path, true, time.Now()); err == nil || !strings.Contains(err.Error(), "resolved target") {
		t.Fatalf("Migrate error = %v", err)
	}
}

func TestWriteNewFileRemovesPartialFileAndJoinsCleanupError(t *testing.T) {
	for _, failAt := range []string{"write", "sync", "close"} {
		t.Run(failAt, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "backup")
			operationErr := errors.New(failAt + " failed")
			cleanupErr := errors.New("cleanup failed")
			openFile := func(path string, flag int, mode os.FileMode) (syncWriteCloser, error) {
				file, err := os.OpenFile(path, flag, mode)
				if err != nil {
					return nil, err
				}
				return &failingSyncWriteCloser{File: file, failAt: failAt, err: operationErr}, nil
			}
			remove := func(path string) error {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				return cleanupErr
			}

			err := writeNewFileWith(path, []byte("contents"), 0o600, openFile, remove)
			if !errors.Is(err, operationErr) || !errors.Is(err, cleanupErr) {
				t.Fatalf("writeNewFileWith error = %v", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("partial backup remains: %v", err)
			}
		})
	}
}

func TestApplyMigrationSyncsBackupDirectoryBeforeReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, FileName)
	backup := path + ".backup"
	var events []string
	syncDirectory := func(dir string) error {
		events = append(events, "sync "+dir)
		return nil
	}
	replace := func(string, []byte, os.FileMode) error {
		events = append(events, "replace")
		return nil
	}

	_, returnedBackup, err := applyMigration(path, backup, []byte("new"), []byte("old"), 0o600, writeNewFile, syncDirectory, replace)
	if err != nil {
		t.Fatal(err)
	}
	if returnedBackup != backup {
		t.Fatalf("backup = %q", returnedBackup)
	}
	want := []string{"sync " + root, "replace"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %q, want %q", events, want)
	}
}

func TestApplyMigrationPreservesRecoveryBackupOnReplacementFailure(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, FileName)
	backup := path + ".backup"
	replaceErr := errors.New("replace failed")
	replace := func(string, []byte, os.FileMode) error { return replaceErr }

	_, returnedBackup, err := applyMigration(path, backup, []byte("new"), []byte("old"), 0o600, writeNewFile, func(string) error { return nil }, replace)
	if !errors.Is(err, replaceErr) || !strings.Contains(err.Error(), backup) {
		t.Fatalf("applyMigration error = %v", err)
	}
	if returnedBackup != backup {
		t.Fatalf("backup = %q", returnedBackup)
	}
	contents, readErr := os.ReadFile(backup)
	if readErr != nil || string(contents) != "old" {
		t.Fatalf("backup contents = %q, %v", contents, readErr)
	}
}

func TestReplaceFileReportsCompletedReplacementWhenDirectorySyncFails(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, FileName)
	mustWrite(t, path, "old")
	syncErr := errors.New("sync failed")

	err := replaceFileWithDirectorySync(path, []byte("new"), 0o600, func(string) error { return syncErr })
	if !errors.Is(err, syncErr) || !strings.Contains(err.Error(), "replacement completed but durability sync failed") {
		t.Fatalf("replaceFileWithDirectorySync error = %v", err)
	}
	contents, readErr := os.ReadFile(path)
	if readErr != nil || string(contents) != "new" {
		t.Fatalf("replacement contents = %q, %v", contents, readErr)
	}
}

type failingSyncWriteCloser struct {
	*os.File
	failAt string
	err    error
}

func (f *failingSyncWriteCloser) Write(contents []byte) (int, error) {
	if f.failAt == "write" {
		n, _ := f.File.Write(contents[:1])
		return n, f.err
	}
	return f.File.Write(contents)
}

func (f *failingSyncWriteCloser) Sync() error {
	if f.failAt == "sync" {
		return f.err
	}
	return f.File.Sync()
}

func (f *failingSyncWriteCloser) Close() error {
	err := f.File.Close()
	if f.failAt == "close" {
		return f.err
	}
	return err
}
