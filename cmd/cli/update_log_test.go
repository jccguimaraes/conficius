package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

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
