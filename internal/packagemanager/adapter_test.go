package packagemanager

import (
	"path/filepath"
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

func TestInspectReportsAdapterAndManagerVersions(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "npm"), "#!/bin/sh\nprintf 'npm 11.20.0\\n'\n")
	t.Setenv("PATH", dir)

	statuses, err := Inspect([]string{PM_NPM})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].AdapterVersion != adapterVersion || statuses[0].ManagerVersion != "11.20.0" {
		t.Fatalf("statuses = %#v", statuses)
	}
}

func TestAdaptersDeclareCapabilities(t *testing.T) {
	want := map[string][]Capability{
		PM_MISE: {CapabilityGlobalInventory, CapabilityProjectInventory, CapabilityGlobalOutdated, CapabilityProjectOutdated, CapabilityGlobalUpdate, CapabilityProjectUpdate},
		PM_BREW: {CapabilityGlobalInventory, CapabilityGlobalOutdated, CapabilityGlobalUpdate},
		PM_NPM:  {CapabilityGlobalInventory, CapabilityProjectInventory, CapabilityGlobalOutdated, CapabilityProjectOutdated, CapabilityGlobalUpdate, CapabilityProjectUpdate},
	}
	for name, capabilities := range want {
		adapter, err := adapterFor(name)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(adapter.capabilities(), capabilities) {
			t.Fatalf("%s capabilities = %#v; want %#v", name, adapter.capabilities(), capabilities)
		}
	}
}

func TestScopeCapabilityTreatsWorkspaceAsProject(t *testing.T) {
	if got := scopeCapability(Scope{Kind: "workspace"}, CapabilityGlobalInventory, CapabilityProjectInventory); got != CapabilityProjectInventory {
		t.Fatalf("capability = %q", got)
	}
}

func TestAdapterForRejectsUnknownManager(t *testing.T) {
	if _, err := adapterFor("unknown"); err == nil {
		t.Fatal("adapterFor unknown manager succeeded")
	}
}
