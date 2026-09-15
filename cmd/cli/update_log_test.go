package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

func TestRotateUpdateLogsKeepsLatestThirty(t *testing.T) {
	directory := t.TempDir()
	for i := range 31 {
		path := filepath.Join(directory, fmt.Sprintf("%02d.json", i))
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := rotateUpdateLogs(directory, 30); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 30 || entries[0].Name() != "01.json" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestWriteUpdateLogRedactsHome(t *testing.T) {
	home := t.TempDir()
	state := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", state)
	log := newUpdateLog([]packagemanager.UpdatePlan{{Manager: "npm", Scope: "project", Directory: filepath.Join(home, "src", "app")}})
	path, err := writeUpdateLog(log)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), home) || !strings.Contains(string(contents), `${HOME}`) {
		t.Fatalf("log = %s", contents)
	}
}
