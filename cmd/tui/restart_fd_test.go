package main

import (
	"os"
	"syscall"
	"testing"
)

func TestRestoreStdinHandlesSourceEqualToTarget(t *testing.T) {
	file, err := os.OpenFile("/dev/null", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	target, err := syscall.Dup(int(file.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Close(target) }()
	if err := restoreStdin(target, target); err != nil {
		t.Fatal(err)
	}
	var buffer [1]byte
	if _, err := syscall.Read(target, buffer[:]); err != nil {
		t.Fatalf("restored descriptor unusable: %v", err)
	}
}
