package main

import (
	"testing"

	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

func TestOutdatedRowsMarkFailedPackage(t *testing.T) {
	pkg := packagemanager.Package{Manager: "brew", Name: "notion", Scope: "global"}
	key := packageKey("global", "", "brew", "notion")
	rows := outdatedRows([]packagemanager.Package{pkg}, map[string]bool{key: true}, map[string]bool{key: true})
	if len(rows) != 1 || !rows[0].Failed || rows[0].Mark != "[x]" {
		t.Fatalf("rows = %#v", rows)
	}
}
