package packagemanager

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestRegisteredPackageManagersComeFromAdapters(t *testing.T) {
	want := []string{PM_MISE, PM_BREW, PM_NPM, PM_MAS, PM_UV, PM_PIPX, PM_POETRY, PM_PNPM, PM_CARGO, PM_YARN, PM_BUN, PM_GO}
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

func TestInspectWarnsForUntestedManagerVersion(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "npm"), "#!/bin/sh\nprintf 'npm 12.0.0\\n'\n")
	t.Setenv("PATH", dir)

	statuses, err := Inspect([]string{PM_NPM})
	if err != nil {
		t.Fatal(err)
	}
	if statuses[0].VersionTested || len(statuses[0].Warnings) != 1 || statuses[0].Status != StatusAvailable {
		t.Fatalf("status = %#v", statuses[0])
	}
}

func TestAdaptersDeclareCapabilities(t *testing.T) {
	want := map[string][]Capability{
		PM_MISE:   {CapabilityGlobalInventory, CapabilityProjectInventory, CapabilityGlobalOutdated, CapabilityProjectOutdated, CapabilityGlobalUpdate, CapabilityProjectUpdate},
		PM_BREW:   {CapabilityGlobalInventory, CapabilityGlobalOutdated, CapabilityGlobalUpdate},
		PM_NPM:    {CapabilityGlobalInventory, CapabilityProjectInventory, CapabilityGlobalOutdated, CapabilityProjectOutdated, CapabilityGlobalUpdate, CapabilityProjectUpdate},
		PM_MAS:    {CapabilityGlobalInventory, CapabilityGlobalOutdated, CapabilityGlobalUpdate},
		PM_UV:     {CapabilityGlobalInventory, CapabilityGlobalOutdated, CapabilityGlobalUpdate},
		PM_PIPX:   {CapabilityGlobalInventory, CapabilityGlobalOutdated, CapabilityGlobalUpdate},
		PM_POETRY: {CapabilityProjectInventory, CapabilityProjectOutdated, CapabilityProjectUpdate},
		PM_PNPM:   {CapabilityProjectInventory, CapabilityProjectOutdated, CapabilityProjectUpdate},
		PM_CARGO:  {CapabilityGlobalInventory},
		PM_YARN:   {CapabilityProjectInventory},
		PM_BUN:    {CapabilityProjectInventory},
		PM_GO:     {CapabilityGlobalInventory},
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

func TestAdaptersDeclareUpdateImpacts(t *testing.T) {
	for _, name := range []string{PM_MISE, PM_NPM} {
		adapter, err := adapterFor(name)
		if err != nil {
			t.Fatal(err)
		}
		if got := adapter.updateImpact(Scope{Kind: "global"}); got != (MutationImpact{}) {
			t.Fatalf("%s impact = %#v", name, got)
		}
	}
	brew, err := adapterFor(PM_BREW)
	if err != nil {
		t.Fatal(err)
	}
	if !brew.updateImpact(Scope{Kind: "global"}).MayRestartServices {
		t.Fatal("Homebrew impact does not report service restart risk")
	}
	mas, err := adapterFor(PM_MAS)
	if err != nil {
		t.Fatal(err)
	}
	if !mas.updateImpact(Scope{Kind: "global"}).RequiresAdmin {
		t.Fatal("mas impact does not report admin requirement")
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
