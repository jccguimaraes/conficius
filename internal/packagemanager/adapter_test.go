package packagemanager

import (
	"reflect"
	"testing"
)

func TestRegisteredPackageManagersComeFromAdapters(t *testing.T) {
	want := []string{PM_MISE, PM_BREW, PM_NPM}
	if !reflect.DeepEqual(RegisteredPackageManagers, want) {
		t.Fatalf("registered managers = %#v; want %#v", RegisteredPackageManagers, want)
	}
	for _, name := range want {
		adapter, err := adapterFor(name)
		if err != nil {
			t.Fatalf("adapterFor(%q): %v", name, err)
		}
		if adapter.name() != name {
			t.Fatalf("adapterFor(%q).name() = %q", name, adapter.name())
		}
	}
}

func TestAdapterForRejectsUnknownManager(t *testing.T) {
	if _, err := adapterFor("unknown"); err == nil {
		t.Fatal("adapterFor unknown manager succeeded")
	}
}
