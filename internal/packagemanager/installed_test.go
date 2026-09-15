package packagemanager

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestQueryInstalledReadsSupportedManagerInventories(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
printf '{"dependencies":{"left-pad":{"version":"1.3.0"},"missing":{}}}'
`)
	writeExecutable(t, filepath.Join(dir, "mise"), `#!/bin/sh
printf '{"node":[{"version":"22.1.0","installed":true},{"version":"20","installed":false}]}'
`)
	writeExecutable(t, filepath.Join(dir, "brew"), `#!/bin/sh
printf '{"formulae":[{"name":"wget","installed":[{"version":"1.2"}]}],"casks":[{"token":"app","installed":"3.0"}]}'
`)
	t.Setenv("PATH", dir)

	result, err := QueryInstalled(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []InstalledPackage{
		{Manager: PM_MISE, Name: "node", Version: "22.1.0"},
		{Manager: PM_BREW, Name: "app", Version: "3.0"},
		{Manager: PM_BREW, Name: "wget", Version: "1.2"},
		{Manager: PM_NPM, Name: "left-pad", Version: "1.3.0"},
	}
	if !reflect.DeepEqual(result.Packages, want) {
		t.Fatalf("packages = %#v; want %#v", result.Packages, want)
	}
}

func TestQueryInstalledScopesRunsProjectNPMInProjectDirectory(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
printf '{"dependencies":{"project-package":{"version":"2.0.0"}}}'
`)
	t.Setenv("PATH", dir)

	result, err := QueryInstalledScopes(context.Background(), []string{PM_NPM}, []Scope{{Kind: "project", Path: project}})
	if err != nil {
		t.Fatal(err)
	}
	want := []InstalledPackage{{Manager: PM_NPM, Name: "project-package", Version: "2.0.0", Scope: "project", Path: project}}
	if !reflect.DeepEqual(result.Packages, want) {
		t.Fatalf("packages = %#v; want %#v", result.Packages, want)
	}
}

func TestQueryInstalledContinuesPastUnavailableManager(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "npm"), "#!/bin/sh\nprintf '{\"dependencies\":{}}'\n")
	t.Setenv("PATH", dir)

	result, err := QueryInstalled(context.Background(), []string{PM_MISE, PM_NPM})
	if err != nil {
		t.Fatal(err)
	}
	if result.Managers[0].Status != StatusUnavailable || result.Managers[1].Status != StatusAvailable {
		t.Fatalf("statuses = %#v", result.Managers)
	}
}

func TestQueryInstalledReportsMalformedInventory(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "npm"), "#!/bin/sh\nprintf broken\n")
	t.Setenv("PATH", dir)

	result, err := QueryInstalled(context.Background(), []string{PM_NPM})
	if err == nil {
		t.Fatal("query succeeded; want decode error")
	}
	if result.Managers[0].Status != StatusError {
		t.Fatalf("status = %#v", result.Managers[0])
	}
}
