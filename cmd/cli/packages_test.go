package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/security"
)

func writeFakeManager(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestPackagesOutdatedJSONIncludesProjectScope(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
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

func TestPackagesOutdatedAppliesProfileAllowlistAndReportsMissingDrift(t *testing.T) {
	dir := t.TempDir()
	writeFakeManager(t, filepath.Join(dir, "npm"), `#!/bin/sh
printf '{"typescript":{"current":"5.8.0","wanted":"5.9.2","latest":"5.9.2"},"prettier":{"current":"3.0.0","wanted":"3.1.0","latest":"3.1.0"}}'
`)
	t.Setenv("PATH", dir)
	configPath := filepath.Join(t.TempDir(), "conficius.toml")
	contents := `version = 2
[profiles.work]
mode = "allowlist"
[[profiles.work.packages]]
manager = "npm"
name = "typescript"
policy = "include"
desired_version = "5.9.2"
[[profiles.work.packages]]
manager = "npm"
name = "eslint"
policy = "include"
desired_version = "9.0.0"
`
	if err := os.WriteFile(configPath, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cli := New(&output, "test")
	if err := cli.Execute([]string{appName, "--config", configPath, "packages", "outdated", "--pm", "npm", "--profile", "work", "--global", "--json"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), `"name": "prettier"`) || !strings.Contains(output.String(), `"name": "typescript"`) || !strings.Contains(output.String(), `"status": "missing"`) {
		t.Fatalf("output = %q", output.String())
	}
}

func TestPackagesCommandsExcludeVulnerabilities(t *testing.T) {
	commands := New(io.Discard, "test").Command.Command("packages").VisibleCommands()
	var names []string
	for _, command := range commands {
		names = append(names, command.Name)
	}
	if got, want := strings.Join(names, ","), "pms,outdated,vulnerabilities,update"; got != want {
		t.Fatalf("commands = %q; want %q", got, want)
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
	if envelope.SchemaVersion != 2 || len(envelope.Data) != 3 {
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

	if err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "outdated", "--pm", "mise"}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "warning: mise unavailable") || !strings.Contains(got, "Total number of packages found: 0") {
		t.Fatalf("output = %q", got)
	}
}

func TestPackagesVulnerabilitiesQueriesExactInstalledNPMVersion(t *testing.T) {
	dir := t.TempDir()
	writeCLIExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
printf '{"dependencies":{"left-pad":{"version":"1.3.0"}}}'
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
		if request.Package.Ecosystem != "npm" || request.Package.Name != "left-pad" || request.Version != "1.3.0" {
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
	if err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "vulnerabilities", "--pm", "npm", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"status": "vulnerable"`) || !strings.Contains(output.String(), `"GHSA-test"`) {
		t.Fatalf("output = %q", output.String())
	}
}

func TestPackagesVulnerabilitiesDeduplicatesAcrossScopes(t *testing.T) {
	dir := t.TempDir()
	writeCLIExecutable(t, filepath.Join(dir, "npm"), `#!/bin/sh
printf '{"dependencies":{"left-pad":{"version":"1.3.0"}}}'
`)
	t.Setenv("PATH", dir)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = fmt.Fprint(w, `{"vulns":[]}`)
	}))
	defer server.Close()
	t.Setenv("CONFICIUS_OSV_URL", server.URL)
	project := t.TempDir()

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

func TestVulnerabilityJSONRetainsEvidenceDetails(t *testing.T) {
	var output bytes.Buffer
	item := security.Evidence{Source: security.SourceEUVD, SourceID: "EUVD-1", Aliases: []string{"CVE-2026-1"}, AffectedRule: "before 22.22.4", Reason: "test reason", CVELink: "https://www.cve.org/CVERecord?id=CVE-2026-1", UpgradeNote: "not confirmed"}
	if err := writePackageJSON(&output, vulnerabilityOutput{Report: security.Report{Evidence: []security.Evidence{item}}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"source_id": "EUVD-1"`, `"aliases": [`, `"affected_rule": "before 22.22.4"`, `"reason": "test reason"`, `"cve_link": "https://www.cve.org/CVERecord?id=CVE-2026-1"`, `"upgrade_note": "not confirmed"`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %s: %s", want, output.String())
		}
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

func TestPackagesUpdateProjectPlanUsesProjectDirectory(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()
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

	if err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "update", "--pm", "npm", "--json"}); err != nil {
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

	err := cli.Command.Run(t.Context(), []string{"conficius", "packages", "update", "--pm", "npm", "--json"})
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
