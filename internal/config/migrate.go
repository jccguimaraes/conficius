package config

import (
	"bytes"
	"errors"
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
	return applyMigration(path, backup, output.Bytes(), original, info.Mode().Perm(), writeNewFile, syncDirectory, replaceFile)
}

func applyMigration(
	path, backup string,
	migrated, original []byte,
	mode os.FileMode,
	writeBackup func(string, []byte, os.FileMode) error,
	syncDir func(string) error,
	replace func(string, []byte, os.FileMode) error,
) ([]byte, string, error) {
	if err := writeBackup(backup, original, mode); err != nil {
		return nil, "", fmt.Errorf("write backup %q: %w", backup, err)
	}
	if err := syncDir(filepath.Dir(backup)); err != nil {
		return nil, backup, fmt.Errorf("sync backup directory for %q: %w", backup, err)
	}
	if err := replace(path, migrated, mode); err != nil {
		return nil, backup, fmt.Errorf("%w; recovery backup: %q", err, backup)
	}
	return migrated, backup, nil
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
	return replaceFileWithDirectorySync(path, contents, mode, syncDirectory)
}

func replaceFileWithDirectorySync(path string, contents []byte, mode os.FileMode, syncDir func(string) error) error {
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
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("replacement completed but durability sync failed for config directory %q: %w", dir, err)
	}
	return nil
}

type syncWriteCloser interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

func writeNewFile(path string, contents []byte, mode os.FileMode) error {
	return writeNewFileWith(path, contents, mode, func(path string, flag int, mode os.FileMode) (syncWriteCloser, error) {
		return os.OpenFile(path, flag, mode)
	}, os.Remove)
}

func writeNewFileWith(
	path string,
	contents []byte,
	mode os.FileMode,
	openFile func(string, int, os.FileMode) (syncWriteCloser, error),
	remove func(string) error,
) error {
	file, err := openFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(contents); err != nil {
		return errors.Join(err, cleanupPartialFile(path, file, remove))
	}
	if err := file.Sync(); err != nil {
		return errors.Join(err, cleanupPartialFile(path, file, remove))
	}
	if err := file.Close(); err != nil {
		return errors.Join(err, removePartialFile(path, remove))
	}
	return nil
}

func cleanupPartialFile(path string, file syncWriteCloser, remove func(string) error) error {
	var closeErr error
	if err := file.Close(); err != nil {
		closeErr = fmt.Errorf("close partial file: %w", err)
	}
	return errors.Join(closeErr, removePartialFile(path, remove))
}

func removePartialFile(path string, remove func(string) error) error {
	if err := remove(path); err != nil {
		return fmt.Errorf("remove partial file %q: %w", path, err)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory %q: %w", path, err)
	}
	if err := directory.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync directory %q: %w", path, err), directory.Close())
	}
	if err := directory.Close(); err != nil {
		return fmt.Errorf("close directory %q: %w", path, err)
	}
	return nil
}
