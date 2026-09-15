package packagemanager

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLookPathAllSkipsEmptyPathEntries(t *testing.T) {
	workingDirectory := t.TempDir()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	writeExecutable(t, filepath.Join(workingDirectory, "npm"), "#!/bin/sh\n")
	t.Setenv("PATH", string(os.PathListSeparator))

	if _, err := LookPathAll("npm"); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("error = %v; want exec.ErrNotFound", err)
	}
}
