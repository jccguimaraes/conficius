package main

import "testing"

func TestRestartDoesNotDuplicateDescriptorOntoItself(t *testing.T) {
	if shouldDuplicateFD(0, 0) {
		t.Fatal("same descriptor must not be duplicated")
	}
	if !shouldDuplicateFD(3, 0) {
		t.Fatal("different descriptor must be duplicated")
	}
}
