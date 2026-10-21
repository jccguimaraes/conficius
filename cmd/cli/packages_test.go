package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	urfave "github.com/urfave/cli/v3"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/jccguimaraes/conficius/internal/security"
)

func writeFakeManager(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestPackagesGoToolsImportWritesJournalWithoutRunningGo(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	var output bytes.Buffer
	if err := New(&output, "test").Command.Run(t.Context(), []string{"conficius", "packages", "go-tools", "import", "golang.org/x/tools/gopls", "v0.20.0", "gopls"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, "conficius", "go-tools.json")
	journal, err := packagemanager.ReadGoToolJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.Imports) != 1 || journal.Imports[0].Module != "golang.org/x/tools/gopls" || !strings.Contains(output.String(), path) {
		t.Fatalf("journal = %#v, output = %q", journal, output.String())
	}
}

func TestManagerScopesExpandsWorkspaces(t *testing.T) {
	scopes := managerScopes([]config.Scope{{Kind: "project", Path: "/repo", Workspaces: []string{"/repo/apps/a", "/repo/apps/b"}}})
	want := []packagemanager.Scope{{Kind: "project", Path: "/repo"}, {Kind: "workspace", Path: "/repo/apps/a"}, {Kind: "workspace", Path: "/repo/apps/b"}}
	if !reflect.DeepEqual(scopes, want) {
		t.Fatalf("scopes = %#v; want %#v", scopes, want)
	}
}

func TestPackageCheckErrorReturnsDocumentedExitCodes(t *testing.T) {
	for _, test := range []struct {
		name    string
		err     error
		updates int
		code    int
	}{
		{name: "updates", updates: 1, code: 1},
		{name: "inspection error", err: errors.New("failed"), code: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var exitErr urfave.ExitCoder
			err := packageCheckError(test.err, test.updates)
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != test.code {
				t.Fatalf("error = %#v", err)
			}
		})
	}
	if err := packageCheckError(nil, 0); err != nil {
		t.Fatal(err)
	}
}

func TestPackagesOutdatedOfflineReportsInventoryAndUnknownFreshness(t *testing.T) {
	dir := t.TempDir()
	writeFakeManager(t, filepath.Join(dir, "npm"), `#!/bin/sh
if [ "$*" = "--version" ]; then printf '11.0.0'; exit 0; fi
if echo "$*" | grep -q outdated; then exit 99; fi
printf '{"dependencies":{"typescript":{"version":"5.0.0"}}}'
`)
	t.Setenv("PATH", dir)
	var output bytes.Buffer
	cli := New(&output, "test")
	if err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "outdated", "--global", "--pm", "npm", "--offline", "--json"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"freshness": "unknown"`, `"name": "typescript"`, `"version": "5.0.0"`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %s: %s", want, output.String())
		}
	}
}

func TestPackagesOutdatedOfflineReportsNPMCommandCollisions(t *testing.T) {
	dir := t.TempDir()
	globalRoot := filepath.Join(t.TempDir(), "lib", "node_modules")
	for _, name := range []string{"first", "second"} {
		manifest := filepath.Join(globalRoot, name, "package.json")
		if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manifest, []byte(`{"bin":{"shared":"cli.js"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeFakeManager(t, filepath.Join(dir, "npm"), `#!/bin/sh
if [ "$1" = "--version" ]; then printf '11.0.0'; elif [ "$1" = "root" ]; then printf '%s' "$NPM_ROOT"; else printf '{"dependencies":{"first":{"version":"1.0.0"},"second":{"version":"1.0.0"}}}'; fi
`)
	t.Setenv("PATH", dir)
	t.Setenv("NPM_ROOT", globalRoot)

	for _, jsonOutput := range []bool{false, true} {
		var output bytes.Buffer
		args := []string{"conficius", "packages", "outdated", "--global", "--pm", "npm", "--offline"}
		if jsonOutput {
			args = append(args, "--json")
		}
		if err := New(&output, "test").Command.Run(t.Context(), args); err != nil {
			t.Fatal(err)
		}
		want := "competing global owners: npm:first, npm:second"
		if jsonOutput {
			want = `"command": "shared"`
		}
		if !strings.Contains(output.String(), want) {
			t.Fatalf("json = %t, output = %q", jsonOutput, output.String())
		}
	}
}

func TestProjectIntegrityChecksSkipCargoOnlyProject(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "Cargo.toml"), []byte("[package]\nname = \"app\"\nversion = \"0.1.0\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if checks := projectIntegrityChecks([]config.Scope{{Kind: "project", Path: project}}); len(checks) != 0 {
		t.Fatalf("checks = %#v", checks)
	}
}

func TestHasPackageManifestDetectsGoProject(t *testing.T) {
	project := t.TempDir()
	if hasPackageManifest(project) {
		t.Fatal("empty directory has package manifest")
	}
	manifest := filepath.Join(project, "go.mod")
	if err := os.Mkdir(manifest, 0o700); err != nil {
		t.Fatal(err)
	}
	if hasPackageManifest(project) {
		t.Fatal("go.mod directory treated as manifest")
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("module example.com/app\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !hasPackageManifest(project) {
		t.Fatal("go.mod not detected")
	}
}

func TestPackagesOutdatedJSONIncludesProjectScope(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeFakeManager(t, filepath.Join(dir, "npm"), `#!/bin/sh
printf '{"left-pad":{"current":"1.0.0","wanted":"1.1.0","latest":"2.0.0"}}'
`)
	t.Setenv("PATH", dir)

	var output bytes.Buffer
	cli := New(&output, "test")
	if err := cli.Execute([]string{appName, "packages", "outdated", "--pm", "npm", "--project", project, "--json"}); err != nil {
		t.Fatal(err)
	}
	resolvedProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"schema_version": 2`) || !strings.Contains(output.String(), `"scope": "project"`) || !strings.Contains(output.String(), resolvedProject) {
		t.Fatalf("output = %q", output.String())
	}
}

func TestPackagesOutdatedReportsAllUpdatesWithoutPolicy(t *testing.T) {
	dir := t.TempDir()
	writeFakeManager(t, filepath.Join(dir, "npm"), `#!/bin/sh
printf '{"typescript":{"current":"5.8.0","wanted":"5.9.2","latest":"5.9.2"},"prettier":{"current":"3.0.0","wanted":"3.1.0","latest":"3.1.0"}}'
`)
	t.Setenv("PATH", dir)
	configPath := filepath.Join(t.TempDir(), "conficius.toml")
	writeCLIFile(t, configPath, "version = 2\n[profiles.work.managers.npm]\nenabled = true\n")
	var output bytes.Buffer
	if err := New(&output, "test").Execute([]string{appName, "--config", configPath, "packages", "outdated", "--pm", "npm", "--profile", "work", "--global", "--json"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"name": "prettier"`, `"name": "typescript"`, `"freshness": "fresh"`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %s: %s", want, output.String())
		}
	}
	for _, removed := range []string{`"drift"`, `"npm_dependency_class_filters"`} {
		if strings.Contains(output.String(), removed) {
			t.Fatalf("output includes removed field %s: %s", removed, output.String())
		}
	}
}

func TestPackagesCommandsExcludeVulnerabilities(t *testing.T) {
	commands := New(io.Discard, "test").Command.Command("packages").VisibleCommands()
	var names []string
	for _, command := range commands {
		names = append(names, command.Name)
	}
	if got, want := strings.Join(names, ","), "go-tools,doctor,pms,outdated,vulnerabilities,update"; got != want {
		t.Fatalf("commands = %q; want %q", got, want)
	}
}

func TestPackagesDoctorReportsManagerIntegrityWithoutFreshnessQuery(t *testing.T) {
	dir := t.TempDir()
	writeFakeManager(t, filepath.Join(dir, "npm"), `#!/bin/sh
if [ "$1" = "--version" ]; then printf '11.0.0'; exit 0; fi
exit 99
`)
	t.Setenv("PATH", dir)
	for _, jsonOutput := range []bool{false, true} {
		var output bytes.Buffer
		args := []string{"conficius", "packages", "doctor", "--pm", "npm"}
		if jsonOutput {
			args = append(args, "--json")
		}
		if err := New(&output, "test").Command.Run(t.Context(), args); err != nil {
			t.Fatal(err)
		}
		want := "npm: healthy: executable and manager version available"
		if jsonOutput {
			want = `"status": "healthy"`
		}
		if !strings.Contains(output.String(), want) {
			t.Fatalf("json = %t, output = %q", jsonOutput, output.String())
		}
	}
}

func TestPackagesDoctorReportsProjectLockfileIntegrity(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	writeFakeManager(t, filepath.Join(dir, "npm"), "#!/bin/sh\nprintf '11.0.0'\n")
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{"dependencies":{"react":"^19"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "package-lock.json"), []byte(`{"lockfileVersion":3,"packages":{"":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	for _, jsonOutput := range []bool{false, true} {
		var output bytes.Buffer
		args := []string{"conficius", "packages", "doctor", "--pm", "npm", "--project", project}
		if jsonOutput {
			args = append(args, "--json")
		}
		if err := New(&output, "test").Command.Run(t.Context(), args); err != nil {
			t.Fatal(err)
		}
		want := "package-lock.json is missing direct dependencies: react"
		if jsonOutput && (!strings.Contains(output.String(), `"projects": [`) || !strings.Contains(output.String(), `"status": "warning"`)) {
			t.Fatalf("output = %q", output.String())
		}
		if !strings.Contains(output.String(), want) {
			t.Fatalf("json = %t, output = %q", jsonOutput, output.String())
		}
	}
}

func TestPackagesPMSJSONReportsUnavailableAndExitZero(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var output bytes.Buffer
	cli := New(&output, "test")

	if err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "pms", "--json"}); err != nil {
		t.Fatal(err)
	}

	var envelope struct {
		SchemaVersion int `json:"schema_version"`
		Data          []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, output.String())
	}
	if envelope.SchemaVersion != 2 || len(envelope.Data) != len(packagemanager.RegisteredPackageManagers) {
		t.Fatalf("envelope = %#v", envelope)
	}
	for _, status := range envelope.Data {
		if status.Status != "unavailable" {
			t.Fatalf("%s status = %q", status.Name, status.Status)
		}
	}
}

func TestPackagesOutdatedMissingManagerWarnsAndExitZero(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var output bytes.Buffer
	cli := New(&output, "test")

	if err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "outdated", "--global", "--pm", "mise"}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "warning: mise unavailable") || !strings.Contains(got, "Total number of packages found: 0") {
		t.Fatalf("output = %q", got)
	}
}

func TestPrintCollisionWarningsReportsCommandPathAndOwners(t *testing.T) {
	var output bytes.Buffer
	candidates := []packagemanager.CollisionCandidate{{
		Command: "node",
		Path:    "/usr/local/bin/node",
		Owners:  []packagemanager.PackageIdentity{{Manager: "brew", Name: "node"}, {Manager: "mise", Name: "node"}},
	}}
	if err := printCollisionWarnings(&output, candidates); err != nil {
		t.Fatal(err)
	}
	want := "warning: command node at /usr/local/bin/node has competing global owners: brew:node, mise:node\n"
	if output.String() != want {
		t.Fatalf("output = %q; want %q", output.String(), want)
	}
}

func TestPrintManagerWarningsReportsDisabledManagerWithoutEmptyError(t *testing.T) {
	var output bytes.Buffer
	statuses := []packagemanager.ManagerStatus{{Name: "npm", Status: packagemanager.StatusDisabled, Evidence: packagemanager.ManagerEvidence{Executable: true}}}
	if err := printManagerWarnings(&output, statuses); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "warning: npm disabled\n"; got != want {
		t.Fatalf("output = %q; want %q", got, want)
	}
}

func TestPackagesOutdatedWarnsWhenGlobalConfigHasNoManagerExecutable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	configPath := filepath.Join(t.TempDir(), "conficius.toml")
	if err := os.WriteFile(configPath, []byte("version = 2\n[profiles.work.managers.npm]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cli := New(&output, "test")

	if err := cli.Command.Run(t.Context(), []string{"conficius", "--config", configPath, "packages", "outdated", "--profile", "work", "--pm", "npm"}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "warning: npm unavailable: executable not found; global config detected") || !strings.Contains(got, "Total number of packages found: 0") {
		t.Fatalf("output = %q", got)
	}
}

func TestPackagesVulnerabilitiesQueriesExactInstalledNPMVersion(t *testing.T) {
	dir := t.TempDir()
	writeCLIExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
if [ "$1" = "--version" ]; then printf '11.19.1\n'; elif [ "$1" = "view" ]; then printf '11.19.1\n'; else printf '{"dependencies":{"left-pad":{"version":"1.3.0"}}}'; fi
`)
	t.Setenv("PATH", dir)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Package struct {
				Ecosystem string `json:"ecosystem"`
				Name      string `json:"name"`
			} `json:"package"`
			Version string `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Package.Ecosystem != "npm" {
			t.Errorf("request = %#v", request)
		}
		if request.Package.Name == "left-pad" && request.Version != "1.3.0" {
			t.Errorf("request = %#v", request)
		}
		if _, err := fmt.Fprint(w, `{"vulns":[{"id":"GHSA-test","summary":"test advisory"}]}`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	t.Setenv("CONFICIUS_OSV_URL", server.URL)

	var output bytes.Buffer
	cli := New(&output, "test")
	if err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "vulnerabilities", "--global", "--pm", "npm", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"status": "vulnerable"`) || !strings.Contains(output.String(), `"GHSA-test"`) {
		t.Fatalf("output = %q", output.String())
	}
}

func TestPackagesVulnerabilitiesDeduplicatesAcrossScopes(t *testing.T) {
	dir := t.TempDir()
	writeCLIExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
if [ "$1" = "--version" ]; then printf '11.19.1\n'; elif [ "$1" = "view" ]; then printf '11.19.1\n'; else printf '{"dependencies":{"left-pad":{"version":"1.3.0"}}}'; fi
`)
	t.Setenv("PATH", dir)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Package struct {
				Name string `json:"name"`
			} `json:"package"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Package.Name == "left-pad" {
			requests++
		}
		_, _ = fmt.Fprint(w, `{"vulns":[]}`)
	}))
	defer server.Close()
	t.Setenv("CONFICIUS_OSV_URL", server.URL)
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	cli := New(&output, "test")
	if err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "vulnerabilities", "--pm", "npm", "--project", project, "--json"}); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || !strings.Contains(output.String(), `"scope": "project"`) {
		t.Fatalf("requests = %d, output = %q", requests, output.String())
	}
}

func TestPrintVulnerabilityFindingsIncludesSharedReportFields(t *testing.T) {
	var output bytes.Buffer
	evidence := []security.Evidence{{Source: security.SourceEUVD, Manager: "mise", Package: "node", Version: "22.10.0", Status: security.StatusVulnerable, Score: 8.1, Advisory: "CVE-2026-1", CVELink: "https://www.cve.org/CVERecord?id=CVE-2026-1", UpgradeTo: "22.22.4", UpgradeNote: "Latest available version is still affected; it does not fix this CVE."}}
	if err := printVulnerabilityFindings(&output, evidence); err != nil {
		t.Fatal(err)
	}
	want := "EUVD mise node 22.10.0 vulnerable 8.1 CVE-2026-1 https://www.cve.org/CVERecord?id=CVE-2026-1 22.22.4 Latest available version is still affected; it does not fix this CVE.\n"
	if output.String() != want {
		t.Fatalf("output = %q", output.String())
	}
}

func TestPrintVulnerabilityFindingsHidesAdvisorylessDiagnostics(t *testing.T) {
	var output bytes.Buffer
	evidence := []security.Evidence{
		{Package: "unmapped", Status: security.StatusUnmapped},
		{Package: "failed", Status: security.StatusError},
		{Source: security.SourceOSV, Package: "affected", Status: security.StatusVulnerable, Advisory: "CVE-1"},
	}
	if err := printVulnerabilityFindings(&output, evidence); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "unmapped") || strings.Contains(output.String(), "failed") || !strings.Contains(output.String(), "CVE-1") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestPackageJSONIncludesSnapshotMetadata(t *testing.T) {
	var output bytes.Buffer
	cli := New(&output, "1.2.3")
	cli.Now = func() time.Time { return time.Date(2026, 12, 15, 10, 30, 0, 0, time.FixedZone("test", 3600)) }
	if err := cli.writePackageJSON([]string{"ok"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"timestamp": "2026-12-15T09:30:00Z"`, `"platform": "` + runtime.GOOS + `"`, `"arch": "` + runtime.GOARCH + `"`, `"cli_version": "1.2.3"`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %s: %s", want, output.String())
		}
	}
}

func TestVulnerabilityJSONRetainsEvidenceDetails(t *testing.T) {
	var output bytes.Buffer
	item := security.Evidence{Source: security.SourceEUVD, SourceID: "EUVD-1", Aliases: []string{"CVE-2026-1"}, Rules: []string{">=20", "20.*"}, Reason: "test reason", CVELink: "https://www.cve.org/CVERecord?id=CVE-2026-1", UpgradeNote: "not confirmed"}
	cli := New(&output, "test")
	if err := cli.writePackageJSON(vulnerabilityOutput{Report: security.Report{Evidence: []security.Evidence{item}}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"source_id": "EUVD-1"`, `"aliases": [`, `"rules": [`, `"\u003e=20`, `20.*`, `"reason": "test reason"`, `"cve_link": "https://www.cve.org/CVERecord?id=CVE-2026-1"`, `"upgrade_note": "not confirmed"`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %s: %s", want, output.String())
		}
	}
}

func TestVulnerabilityJSONOmitsRulesForConfirmedAffected(t *testing.T) {
	var output bytes.Buffer
	item := security.Evidence{Status: security.StatusVulnerable, Advisory: "CVE-1"}
	cli := New(&output, "test")
	if err := cli.writePackageJSON(vulnerabilityOutput{Report: security.Report{Evidence: []security.Evidence{item}}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), `"rules"`) {
		t.Fatalf("output = %s", output.String())
	}
}

func TestPrintVulnerabilityFindingsShowsFirstRuleAndRemainingCount(t *testing.T) {
	var output bytes.Buffer
	item := security.Evidence{Source: security.SourceEUVD, Status: security.StatusUnknown, Advisory: "CVE-1", Rules: []string{">=20", "20.*", "latest"}}
	if err := printVulnerabilityFindings(&output, []security.Evidence{item}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), ">=20 (+2 more)") || strings.Contains(output.String(), "20.*") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestPrintUpdatePlansShowsConfidence(t *testing.T) {
	var output bytes.Buffer
	plans := []packagemanager.UpdatePlan{{RunAs: "builder", RunAsMethod: "su", Executable: "/bin/npm", Args: []string{"update"}, Confidence: packagemanager.PlanConfidenceEstimated, StateHash: "abc123", Impact: packagemanager.MutationImpact{MayRestartServices: true}, Recovery: []packagemanager.RecoveryCommand{{Executable: "/bin/npm", Args: []string{"install", "typescript@5.0"}}}}}
	if err := printUpdatePlans(&output, plans, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Run as: builder (su login environment)") || !strings.Contains(output.String(), "Confidence: estimated Plan: abc123") || !strings.Contains(output.String(), "restart-services=true") || !strings.Contains(output.String(), `Recovery: "/bin/npm" "install" "typescript@5.0"`) {
		t.Fatalf("output = %q", output.String())
	}
}

func TestValidateManagerVersionsBlocksUntestedMutations(t *testing.T) {
	statuses := []packagemanager.ManagerStatus{{Name: "npm", Status: packagemanager.StatusAvailable, ManagerVersion: "12.0.0", VersionTested: false}}
	if err := validateManagerVersions(statuses, false); err == nil || !strings.Contains(err.Error(), "--allow-untested-manager") {
		t.Fatalf("error = %v", err)
	}
	if err := validateManagerVersions(statuses, true); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAcceptedPlansRequiresExactHashes(t *testing.T) {
	plans := []packagemanager.UpdatePlan{{StateHash: "first"}, {StateHash: "second"}}
	if err := validateAcceptedPlans(plans, []string{"first", "second"}); err != nil {
		t.Fatal(err)
	}
	if err := validateAcceptedPlans(plans, []string{"first"}); err == nil || !strings.Contains(err.Error(), "second") {
		t.Fatalf("missing hash error = %v", err)
	}
	if err := validateAcceptedPlans(plans, []string{"first", "second", "stale"}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale hash error = %v", err)
	}
}

func TestPackagesUpdateYesRequiresApply(t *testing.T) {
	var output bytes.Buffer
	cli := New(&output, "test")
	err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "update", "--yes"})
	if err == nil || !strings.Contains(err.Error(), "--yes requires --apply") {
		t.Fatalf("error = %v", err)
	}
}

func TestPackagesUpdateBestEffortRequiresApply(t *testing.T) {
	var output bytes.Buffer
	cli := New(&output, "test")
	err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "update", "--best-effort"})
	if err == nil || !strings.Contains(err.Error(), "--best-effort requires --apply") {
		t.Fatalf("error = %v", err)
	}
}

func TestPackagesUpdateApplyRequiresExplicitScope(t *testing.T) {
	var output bytes.Buffer
	cli := New(&output, "test")
	err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "update", "--apply"})
	if err == nil || !strings.Contains(err.Error(), "requires --global") {
		t.Fatalf("error = %v", err)
	}
}

func TestPackagesUpdateMASPreviewUsesAdamIDAndAdminImpact(t *testing.T) {
	dir := t.TempDir()
	writeCLIExecutable(t, filepath.Join(dir, "mas"), `#!/bin/sh
if [ "$1" = "--version" ]; then printf '7.0.0'; else printf '{"adamID":497799835,"version":"26.0","newVersion":"27.0"}'; fi
`)
	t.Setenv("PATH", dir)
	var output bytes.Buffer
	cli := New(&output, "test")
	if err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "update", "--pm", "mas", "--global", "--json"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"manager": "mas"`, `"requires_admin": true`, `"update"`, `"497799835"`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q: %s", want, output.String())
		}
	}
}

func TestPackagesOutdatedMASProjectScopeReportsUnsupportedOperation(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	writeCLIExecutable(t, filepath.Join(dir, "mas"), "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf '7.0.0'; else exit 99; fi\n")
	t.Setenv("PATH", dir)
	var output bytes.Buffer
	cli := New(&output, "test")
	if err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "outdated", "--pm", "mas", "--project", project, "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"status": "unsupported"`) || !strings.Contains(output.String(), `"scope": "project"`) {
		t.Fatalf("output = %q", output.String())
	}
}

func TestPackagesUpdateProjectPlanUsesProjectDirectory(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeCLIExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
printf '{"left-pad":{"current":"1.0.0","wanted":"1.1.0","latest":"1.1.0"}}'
`)
	t.Setenv("PATH", dir)
	var output bytes.Buffer
	cli := New(&output, "test")
	if err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "update", "--pm", "npm", "--project", project, "--json"}); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"directory": "`+resolved+`"`) || strings.Contains(output.String(), `"--global"`) {
		t.Fatalf("output = %q", output.String())
	}
}

func TestPackagesUpdateDefaultsToJSONPreviewWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "mutated")
	writeCLIExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
if [ "$1" = "outdated" ]; then
  printf '{"left-pad":{"current":"1.0.0","wanted":"1.1.0","latest":"1.1.0"}}'
  exit 1
fi
touch "$MUTATION_MARKER"
`)
	t.Setenv("PATH", dir)
	t.Setenv("MUTATION_MARKER", marker)
	var output bytes.Buffer
	cli := New(&output, "test")

	if err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "update", "--global", "--pm", "npm", "--json"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("mutation marker exists: %v", err)
	}
	if !strings.Contains(output.String(), `"schema_version": 2`) || !strings.Contains(output.String(), `"args": [`) || !strings.Contains(output.String(), `"left-pad"`) {
		t.Fatalf("output = %q", output.String())
	}
}

func TestPackagesUpdateJSONReportsManagerFailureWithNonzeroResult(t *testing.T) {
	dir := t.TempDir()
	writeCLIExecutable(t, filepath.Join(dir, "npm"), "#!/bin/sh\necho failed >&2\nexit 2\n")
	t.Setenv("PATH", dir)
	var output bytes.Buffer
	cli := New(&output, "test")

	err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "update", "--global", "--pm", "npm", "--json"})
	if err == nil {
		t.Fatal("update succeeded; want manager failure")
	}
	var envelope packageOutputEnvelope
	if jsonErr := json.Unmarshal(output.Bytes(), &envelope); jsonErr != nil {
		t.Fatalf("output is not JSON: %v\n%s", jsonErr, output.String())
	}
}

func writeCLIExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestPackagesGoProjectUpdatePreviewAndGuard(t *testing.T) {
	bin, project := t.TempDir(), t.TempDir()
	manifest := filepath.Join(project, "go.mod")
	original := "module example.com/main\n\ngo 1.26.0\n"
	if err := os.WriteFile(manifest, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	writeCLIExecutable(t, filepath.Join(bin, "go"), `#!/bin/sh
case "$1" in
version) printf 'go version go1.26.8' ;;
list) printf '%s' '{"Path":"example.com/direct","Version":"v1.0.0","Update":{"Path":"example.com/direct","Version":"v1.2.0"}}' ;;
*) exit 99 ;;
esac
`)
	t.Setenv("PATH", bin)
	var output bytes.Buffer
	cli := New(&output, "test")
	args := []string{"conficius", "packages", "update", "--pm", "go", "--project", project}
	if err := cli.Command.Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"get" "example.com/direct@v1.2.0"`, "go.mod, go.sum and transitive dependencies", "no automatic rollback"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q in %s", want, output.String())
		}
	}
	if err := cli.Command.Run(t.Context(), append(args, "--apply", "--allow-dirty", "--allow-unversioned")); err == nil || !strings.Contains(err.Error(), "does not allow manifest changes") {
		t.Fatalf("guard error = %v", err)
	}
	data, err := os.ReadFile(manifest)
	if err != nil || string(data) != original {
		t.Fatalf("manifest changed: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(project, "go.sum")); !os.IsNotExist(err) {
		t.Fatalf("unexpected go.sum: %v", err)
	}
}

func TestGraphOnlyOutputScopeFilterAndJSON(t *testing.T) {
	evidence := []security.Evidence{{Manager: "go", Package: "google.golang.org/grpc", DependencyClass: "graph-only", ScanScope: "current build; not call reachability", Status: security.StatusVulnerable, Advisory: "GO-1"}}
	var output bytes.Buffer
	if err := printVulnerabilityFindings(&output, evidence); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "GO-1") || !strings.Contains(output.String(), "current build") {
		t.Fatal(output.String())
	}
	output.Reset()
	if err := printVulnerabilityFindings(&output, evidence, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "[graph-only]") || !strings.Contains(output.String(), "GO-1") {
		t.Fatal(output.String())
	}
	output.Reset()
	cli := New(&output, "test")
	if err := cli.writePackageJSON(vulnerabilityOutput{Report: security.Report{Evidence: evidence}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"dependency_class": "graph-only"`, `"scan_scope": "current build; not call reachability"`, `"advisory": "GO-1"`} {
		if !strings.Contains(output.String(), want) {
			t.Fatal(output.String())
		}
	}
}

func TestGoVulnerabilityFlagsSelectJSONAndRequests(t *testing.T) {
	bin, project := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/main\ntool example.com/tool/cmd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeCLIExecutable(t, filepath.Join(bin, "go"), `#!/bin/sh
case "$*" in
version) printf 'go version go1.27.1' ;;
'list -m -json -mod=readonly all'|'list -m -json -mod=readonly -u all')
printf '%s\n' '{"Path":"example.com/direct","Version":"v1.0.0"}' '{"Path":"example.com/indirect","Version":"v1.0.0","Indirect":true}' '{"Path":"example.com/tool","Version":"v1.0.0","Indirect":true}' '{"Path":"example.com/nested","Version":"v1.0.0","Indirect":true}' '{"Path":"example.com/graph","Version":"v1.0.0","Indirect":true}' ;;
'list -deps -e -json -mod=readonly ./...'|'list -deps -e -json -mod=readonly -test ./...')
printf '%s\n' '{"Module":{"Path":"example.com/direct"}}' '{"Module":{"Path":"example.com/indirect"}}' ;;
'list -deps -e -json -mod=readonly tool')
printf '%s\n' '{"Module":{"Path":"example.com/tool"}}' '{"Module":{"Path":"example.com/nested"}}' ;;
*) exit 99 ;;
esac
`)
	t.Setenv("PATH", bin)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = fmt.Fprint(w, `{"vulns":[]}`)
	}))
	defer server.Close()
	t.Setenv("CONFICIUS_OSV_URL", server.URL)
	for _, tc := range []struct {
		name  string
		flags []string
		want  int
	}{
		{"default", nil, 2}, {"indirect", []string{"--include-indirect"}, 4},
		{"graph", []string{"--include-graph-only"}, 3},
		{"both", []string{"--include-indirect", "--include-graph-only"}, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests.Store(0)
			var output bytes.Buffer
			cli := New(&output, "test")
			args := append([]string{"conficius", "packages", "vulnerabilities", "--pm", "go", "--project", project, "--json"}, tc.flags...)
			if err := cli.Command.Run(t.Context(), args); err != nil {
				t.Fatal(err)
			}
			var result struct {
				Data vulnerabilityOutput `json:"data"`
			}
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			// Go has no automatic EUVD identity, so each selected module has OSV and unmapped EUVD evidence.
			if int(requests.Load()) != tc.want || len(result.Data.Report.Evidence) != tc.want*2 {
				t.Fatalf("requests=%d report=%#v", requests.Load(), result.Data.Report)
			}
			for _, item := range result.Data.Report.Evidence {
				if item.Package == "example.com/tool" && (!item.DeclaredTool || !item.Transitive) {
					t.Fatal(item)
				}
			}
		})
	}
}
