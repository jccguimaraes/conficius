package config

import (
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
