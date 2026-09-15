package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

type migrationDocument struct {
	Version         int                 `toml:"version"`
	Profiles        map[string]Profile  `toml:"profiles,omitempty"`
	Tools           map[string]Tool     `toml:"tools,omitempty"`
	Vulnerabilities VulnerabilityConfig `toml:"vulnerabilities,omitempty"`
}

func Migrate(path string, apply bool, now time.Time) ([]byte, string, error) {
	path, err := absolutePath(path)
	if err != nil {
		return nil, "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, "", fmt.Errorf("inspect config %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		resolved, resolveErr := filepath.EvalSymlinks(path)
		if resolveErr != nil {
			return nil, "", fmt.Errorf("%w %q", errMigrationSymlink, path)
		}
		return nil, "", fmt.Errorf("%w %q; use resolved target %q", errMigrationSymlink, path, resolved)
	}
	project, err := Load(path)
	if err != nil {
		return nil, "", err
	}
	if project.Version == CurrentVersion {
		return nil, "", fmt.Errorf("config %q already uses version %d", path, CurrentVersion)
	}
	document := migrationDocument{
		Version:         CurrentVersion,
		Profiles:        map[string]Profile{"default": defaultProfile()},
		Tools:           project.Tools,
		Vulnerabilities: project.Vulnerabilities,
	}
	var output bytes.Buffer
	output.WriteString("# Migrated by Conficius. Original comments remain in the backup created by --apply.\n")
	if err := toml.NewEncoder(&output).Encode(document); err != nil {
		return nil, "", fmt.Errorf("encode migrated config: %w", err)
	}
	if !apply {
		return output.Bytes(), "", nil
	}
	backup, err := nextBackupPath(path, now.UTC())
	if err != nil {
		return nil, "", err
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read config %q: %w", path, err)
	}
	if err := writeNewFile(backup, original, info.Mode().Perm()); err != nil {
		return nil, "", fmt.Errorf("write backup %q: %w", backup, err)
	}
	if err := replaceFile(path, output.Bytes(), info.Mode().Perm()); err != nil {
		return nil, backup, err
	}
	return output.Bytes(), backup, nil
}

func nextBackupPath(path string, now time.Time) (string, error) {
	base := path + ".backup-" + now.Format("20060102T150405Z")
	for i := 0; ; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}
		_, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("inspect backup path %q: %w", candidate, err)
		}
	}
}

func replaceFile(path string, contents []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".conficius-migrate-*")
	if err != nil {
		return fmt.Errorf("create migration file: %w", err)
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return fmt.Errorf("set migration permissions: %w", err)
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return fmt.Errorf("write migration file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync migration file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close migration file: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace config %q: %w", path, err)
	}
	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open config directory: %w", err)
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return fmt.Errorf("sync config directory: %w", err)
	}
	if err := directory.Close(); err != nil {
		return fmt.Errorf("close config directory: %w", err)
	}
	return nil
}

func writeNewFile(path string, contents []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
