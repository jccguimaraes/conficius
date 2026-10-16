package packagemanager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fakeApt(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, PM_APT), "#!/bin/sh\n[ \"$*\" = '--version' ] || exit 9\nprintf 'apt 3.0.3 (amd64)\\nsecond line 99.0\\n'\n")
	writeExecutable(t, filepath.Join(dir, "dpkg-query"), `#!/bin/sh
[ "$#" = 2 ] && [ "$1" = '-W' ] && [ "$2" = '-f=${binary:Package}	${Version}	${db:Status-Status}
' ] || exit 8
printf 'libfoo:amd64\t1:2.0-1\tinstalled\nlibfoo:i386\t1:2.0-1\tinstalled\nheld\t1.0\tinstalled\nremoved\t1.0\tconfig-files\n'
`)
	writeExecutable(t, filepath.Join(dir, "apt-get"), `#!/bin/sh
[ "$#" = 5 ] && [ "$1" = '--simulate' ] && [ "$2" = '--no-remove' ] && [ "$3" = '-o' ] && [ "$4" = 'APT::Get::Upgrade-Allow-New=false' ] && [ "$5" = 'upgrade' ] || exit 7
printf 'Reading package lists...\nThe following packages have been kept back:\n  held\nInst libfoo [1:2.0-1] (1:2.1-1 Debian:13/stable [amd64])\nInst libfoo:i386 [1:2.0-1] (1:2.1-1 Debian:13/stable [i386])\nConf libfoo (1:2.1-1 Debian:13/stable [amd64])\n'
`)
	t.Setenv("PATH", dir)
	return dir
}

func TestAptQueriesReadOnlyGlobalInventoryAndOutdated(t *testing.T) {
	dir := fakeApt(t)
	ctx := context.Background()
	scopes := []Scope{{Kind: "global"}, {Kind: "project", Path: t.TempDir()}}
	installed, err := QueryInstalledScopes(ctx, []string{PM_APT}, scopes)
	if err != nil {
		t.Fatal(err)
	}
	if len(installed.Packages) != 3 || installed.Packages[0].Role != RoleLibrary || installed.Packages[0].Scope != "global" {
		t.Fatalf("inventory = %#v", installed)
	}
	options := QueryOptions{Executables: map[string]string{PM_APT: filepath.Join(dir, PM_APT)}, Managers: map[string]ManagerOptions{PM_APT: {IncludeGreedy: true, IncludeBumpCandidates: true}}}
	outdated, err := QueryOutdatedScopesWithOptions(ctx, []string{PM_APT}, scopes, options)
	if err != nil {
		t.Fatal(err)
	}
	records := Records(outdated.Packages)
	if len(records) != 2 || records[0].Name != "libfoo:amd64" || records[1].Name != "libfoo:i386" || records[0].Current != "1:2.0-1" || records[0].Latest != "1:2.1-1" || records[0].Requested != "" || records[0].Executable != filepath.Join(dir, PM_APT) {
		t.Fatalf("records = %#v", records)
	}
	for _, status := range []ManagerStatus{installed.Managers[0], outdated.Managers[0]} {
		if status.Status != StatusAvailable || status.ManagerVersion != "3.0.3" || !status.VersionTested || !strings.Contains(strings.Join(status.Warnings, " "), "not refreshed") {
			t.Fatalf("status = %#v", status)
		}
		found := false
		for _, operation := range status.Operations {
			if operation.Scope == "project" && operation.Status == StatusUnsupported {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing unsupported project status: %#v", status)
		}
	}
	if _, err := PlanUpdates(outdated, nil, nil); err == nil {
		t.Fatal("apt update plan accepted")
	}
	if (Apt{}).updateArgs(nil) != nil || !reflect.DeepEqual((Apt{}).updateImpact(Scope{}), MutationImpact{RequiresAdmin: true, MayRestartServices: true, MayReboot: true}) {
		t.Fatal("incorrect apt mutation contract")
	}
}

func TestAptParserRejectsUnsafeSimulation(t *testing.T) {
	installed := []InstalledPackage{{Name: "foo", Version: "1"}}
	for _, output := range []string{
		"Inst foo (2 Debian [amd64])",
		"Inst foo [1] (2 Debian [amd64]) trailing",
		" Inst foo [1] (2 Debian [amd64])",
		"Inst foo [1] (2)",
		"Inst foo [1] (2 Debian [amd64]",
		"Inst foo [0] (2 Debian [amd64])",
		"Inst unknown [1] (2 Debian [amd64])",
		"Inst foo:amd64 [1] (2 Debian [i386])",
		"Remv foo [1]",
		"Inst foo [1] (2 Debian [amd64])\nInst foo [1] (2 Debian [amd64])",
	} {
		t.Run(output, func(t *testing.T) {
			_, err := parseAptOutdated([]byte(output), "apt", installed)
			var operation *OperationError
			if !errors.As(err, &operation) || operation.Category != ErrorParser {
				t.Fatalf("error = %v", err)
			}
		})
	}
	packages, err := parseAptOutdated([]byte("Inst foo [1] (2 Debian:12/stable [amd64])\n"), "apt", installed)
	if err != nil || len(packages["foo"]) != 1 {
		t.Fatalf("packages = %#v, error = %v", packages, err)
	}
}

func TestAptInventoryRejectsMalformedOutput(t *testing.T) {
	for _, output := range []string{"bad", "foo\t\tinstalled", "foo\t1\t", "foo bar\t1\tinstalled", "foo\t1\tinstalled\textra"} {
		if _, err := parseAptInstalled([]byte(output)); err == nil {
			t.Fatalf("accepted %q", output)
		}
	}
	packages, err := parseAptInstalled([]byte("foo\t1\tinstalled\nbar\t\tnot-installed\n"))
	if err != nil || len(packages) != 1 {
		t.Fatalf("packages = %#v, error = %v", packages, err)
	}
}

func TestAptMissingCompanionAndCommandFailure(t *testing.T) {
	for _, companion := range []string{"apt-get", "dpkg-query"} {
		t.Run(companion, func(t *testing.T) {
			dir := fakeApt(t)
			if err := os.Remove(filepath.Join(dir, companion)); err != nil {
				t.Fatal(err)
			}
			result, err := QueryOutdated(context.Background(), []string{PM_APT})
			if err == nil || result.Managers[0].Status != StatusError || result.Managers[0].Operations[0].Category != ErrorUnavailable {
				t.Fatalf("result = %#v, error = %v", result, err)
			}
		})
	}
	dir := fakeApt(t)
	writeExecutable(t, filepath.Join(dir, "apt-get"), "#!/bin/sh\nprintf 'simulation failed' >&2\nexit 2\n")
	result, err := QueryOutdated(context.Background(), []string{PM_APT})
	if err == nil || result.Managers[0].Operations[0].Category != ErrorCommand {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestAptCompanionPrefersSiblingAndFallsBackToPATH(t *testing.T) {
	dir := fakeApt(t)
	other := t.TempDir()
	writeExecutable(t, filepath.Join(other, "dpkg-query"), "#!/bin/sh\nexit 9\n")
	t.Setenv("PATH", other)
	path, err := aptCompanion(filepath.Join(dir, PM_APT), "dpkg-query")
	if err != nil || path != filepath.Join(dir, "dpkg-query") {
		t.Fatalf("path = %q, error = %v", path, err)
	}
	path, err = aptCompanion(filepath.Join(other, PM_APT), "apt-get")
	if err == nil || path != "" {
		t.Fatalf("missing companion: path = %q, error = %v", path, err)
	}
	t.Setenv("PATH", dir)
	path, err = aptCompanion(filepath.Join(other, PM_APT), "apt-get")
	if err != nil || path != filepath.Join(dir, "apt-get") {
		t.Fatalf("fallback: path = %q, error = %v", path, err)
	}
}

func TestAptVersionUsesFirstLineOnly(t *testing.T) {
	for _, version := range []string{"2.6.1", "3.0.3"} {
		if got := managerVersion(PM_APT, []byte("apt "+version+" (amd64)\n")); got != version || !versionInRange(got, (Apt{}).testedVersions()) {
			t.Fatalf("version = %q", got)
		}
	}
	if got := managerVersion(PM_APT, []byte("apt unknown\n3.0.3")); got != "" {
		t.Fatalf("version = %q", got)
	}
}
