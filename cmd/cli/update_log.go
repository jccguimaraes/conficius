package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

type updateLog struct {
	StartedAt string                      `json:"started_at"`
	Status    string                      `json:"status"`
	Plans     []packagemanager.UpdatePlan `json:"plans"`
	Error     string                      `json:"error,omitempty"`
}

func writeUpdateLog(log updateLog) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory for update log: %w", err)
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	directory := filepath.Join(state, "conficius", "updates")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create update log directory: %w", err)
	}
	copyBytes, err := json.Marshal(log)
	if err != nil {
		return "", fmt.Errorf("copy update log: %w", err)
	}
	var redacted updateLog
	if err := json.Unmarshal(copyBytes, &redacted); err != nil {
		return "", fmt.Errorf("copy update log: %w", err)
	}
	for i := range redacted.Plans {
		redacted.Plans[i].Directory = redactHome(redacted.Plans[i].Directory, home)
		for j := range redacted.Plans[i].Packages {
			redacted.Plans[i].Packages[j].Path = redactHome(redacted.Plans[i].Packages[j].Path, home)
		}
	}
	contents, err := json.MarshalIndent(redacted, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode update log: %w", err)
	}
	name := strings.ReplaceAll(log.StartedAt, ":", "") + ".json"
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, append(contents, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("write update log: %w", err)
	}
	if err := rotateUpdateLogs(directory, 30); err != nil {
		return "", err
	}
	return path, nil
}

func finishUpdateLog(path string, log updateLog) error {
	written, err := writeUpdateLog(log)
	if err != nil {
		return err
	}
	if written != path {
		return fmt.Errorf("update log path changed from %q to %q", path, written)
	}
	return nil
}

func rotateUpdateLogs(directory string, keep int) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read update logs: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names[:max(0, len(names)-keep)] {
		if err := os.Remove(filepath.Join(directory, name)); err != nil {
			return fmt.Errorf("rotate update log %q: %w", name, err)
		}
	}
	return nil
}

func newUpdateLog(plans []packagemanager.UpdatePlan) updateLog {
	return updateLog{StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Status: "planned", Plans: plans}
}

func redactHome(path, home string) string {
	if path == home {
		return "${HOME}"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "${HOME}" + strings.TrimPrefix(path, home)
	}
	return path
}
