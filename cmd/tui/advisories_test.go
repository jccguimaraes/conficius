package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/euvd"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/jccguimaraes/conficius/internal/security"
	"github.com/jccguimaraes/conficius/internal/vulnerability"
	"github.com/kungfusheep/glyph"
)

type advisoryEUVDClient struct{ t *testing.T }

func (client advisoryEUVDClient) Query(_ context.Context, identity euvd.Identity, version string) ([]euvd.Vulnerability, error) {
	if identity.Product != "Node" || identity.Vendor != "nodejs" || version != "22.10.0" {
		client.t.Errorf("unexpected EUVD query: %+v@%s", identity, version)
	}
	entry := euvd.ProductEntry{ProductVersion: "before 23.0.0"}
	entry.Product.Name, entry.Product.Vendor.Name = "Node", "nodejs"
	return []euvd.Vulnerability{{ID: "EUVD-1", Aliases: "CVE-2026-1", Products: []euvd.ProductEntry{entry}}}, nil
}

type advisoryOSVClient struct{}

func (advisoryOSVClient) Query(context.Context, vulnerability.Package) ([]vulnerability.Vulnerability, error) {
	return nil, nil
}

func TestOutdatedSecurityFromScan(t *testing.T) {
	pkg := packagemanager.Package{Manager: "mise", Name: "node", Current: "22.10.0", Requested: "23.0.0", Latest: "23.0.0", Scope: "project", Path: "/project"}
	report, err := security.Scan(context.Background(), []packagemanager.InstalledPackage{
		{Manager: pkg.Manager, Name: pkg.Name, Version: pkg.Current, Scope: pkg.Scope, Path: pkg.Path},
	}, security.Options{
		Outdated:    []packagemanager.Package{pkg},
		OSVMappings: []config.VulnerabilityMapping{{Manager: "mise", Package: "node", Ecosystem: "npm", Name: "node"}},
		OSVClient:   advisoryOSVClient{}, EUVDClient: advisoryEUVDClient{t},
	})
	if err != nil {
		t.Fatal(err)
	}
	clean, vulnerable := false, false
	for _, item := range report.Evidence {
		if item.Path != pkg.Path || item.Scope != pkg.Scope {
			t.Fatalf("lost inventory identity: %+v", item)
		}
		clean = clean || item.Source == security.SourceOSV && item.Status == security.StatusClean
		vulnerable = vulnerable || item.Source == security.SourceEUVD && item.Status == security.StatusVulnerable
	}
	if !clean || !vulnerable {
		t.Fatalf("expected OSV clean and EUVD vulnerable: %+v", report.Evidence)
	}
	row := outdatedRow{Package: pkg}
	data := &model{securityChecked: true, allEvidence: report.Evidence}
	updateOutdatedSecurity(&row, data)
	if row.Security != "1 advisories" || row.SecurityColor != glyph.Red || !strings.Contains(row.AdvisoryDetails, "CVE-2026-1") {
		t.Fatalf("project CVE missing or false clean: %+v", row)
	}
	for _, scope := range []string{"global", "project"} {
		other := pkg
		other.Scope, other.Path = scope, "/other"
		row = outdatedRow{Package: other}
		updateOutdatedSecurity(&row, data)
		if row.Security != "Unknown" || row.AdvisoryDetails != "" {
			t.Fatalf("project evidence leaked: %+v", row)
		}
	}
}

func TestMissingMiseRequestedSecurityFromScan(t *testing.T) {
	pkg := packagemanager.Package{Manager: "mise", Name: "node", Requested: "22.10.0", Scope: "project", Path: "/project"}
	report, err := security.Scan(t.Context(), nil, security.Options{
		Outdated:    []packagemanager.Package{pkg, pkg},
		OSVMappings: []config.VulnerabilityMapping{{Manager: "mise", Package: "node", Ecosystem: "npm", Name: "node"}},
		OSVClient:   advisoryOSVClient{}, EUVDClient: advisoryEUVDClient{t},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Evidence) != 2 || report.Summary.Vulnerable != 0 || report.Summary.Requested != 1 {
		t.Fatalf("requested assessment missing or counted as installed: %+v", report)
	}
	for _, item := range report.Evidence {
		if item.VersionSource != "requested" {
			t.Fatalf("missing provenance: %+v", item)
		}
	}
	findings := vulnerabilityRows(report.Evidence)
	if len(findings) != 1 || !strings.Contains(findings[0].Current, "requested") || vulnerabilityOrigin(findings[0].Evidence) != "requested version (not installed)" {
		t.Fatalf("vulnerability provenance: %+v", findings)
	}
	row := outdatedRow{Package: pkg}
	updateOutdatedSecurity(&row, &model{securityChecked: true, allEvidence: report.Evidence})
	if row.Security != "1 advisories" || row.Package.Current != "" {
		t.Fatalf("requested findings missing or installed version fabricated: %+v", row)
	}
	for _, want := range []string{"Requested-version security evidence", "not installed", "Affected requested version: 22.10.0", "CVE-2026-1"} {
		if !strings.Contains(row.AdvisoryDetails, want) {
			t.Errorf("missing %q in %q", want, row.AdvisoryDetails)
		}
	}
	for _, other := range []packagemanager.Package{
		{Manager: "mise", Name: "node", Requested: "22.10.0", Scope: "global", Path: "/project"},
		{Manager: "mise", Name: "node", Requested: "22.10.0", Scope: "project", Path: "/other"},
		{Manager: "mise", Name: "node", Current: "22.10.0", Scope: "project", Path: "/project"},
	} {
		otherRow := outdatedRow{Package: other}
		updateOutdatedSecurity(&otherRow, &model{securityChecked: true, allEvidence: report.Evidence})
		if otherRow.Security != "Unknown" || otherRow.AdvisoryDetails != "" {
			t.Fatalf("requested evidence leaked: %+v", otherRow)
		}
	}
	if strings.Contains(row.AdvisoryDetails, "Affected installed version") {
		t.Fatal(row.AdvisoryDetails)
	}
}

func TestOutdatedSecurityStates(t *testing.T) {
	pkg := packagemanager.Package{Manager: "npm", Name: "tool", Current: "1", Scope: "project", Path: "/project"}
	clean := security.Evidence{Manager: pkg.Manager, Package: pkg.Name, Version: pkg.Current, Scope: pkg.Scope, Path: pkg.Path, Status: security.StatusClean}
	for _, tc := range []struct {
		name              string
		checked, scanning bool
		err               error
		evidence          []security.Evidence
		want              string
	}{
		{"unchecked", false, false, nil, nil, "Unknown"},
		{"scanning", false, true, nil, []security.Evidence{clean}, "Scanning"},
		{"absent", true, false, nil, nil, "Unknown"},
		{"clean", true, false, nil, []security.Evidence{clean}, "No known findings"},
		{"canceled", false, false, context.Canceled, []security.Evidence{clean}, "Canceled"},
		{"failed", true, false, errors.New("offline"), []security.Evidence{clean}, "Scan failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := outdatedRow{Package: pkg}
			updateOutdatedSecurity(&row, &model{securityChecked: tc.checked, securityScanning: tc.scanning, securityErr: tc.err, allEvidence: tc.evidence})
			if row.Security != tc.want {
				t.Fatalf("got %q, want %q", row.Security, tc.want)
			}
		})
	}
	for _, status := range []string{security.StatusUnknown, security.StatusUnmapped, security.StatusError} {
		item := clean
		item.Status = status
		row := outdatedRow{Package: pkg}
		updateOutdatedSecurity(&row, &model{securityChecked: true, allEvidence: []security.Evidence{clean, item}})
		if row.Security != "Unknown" {
			t.Fatal(row.Security)
		}
	}
	for _, version := range []string{"22", "22.10", "0.12", "1.2.3-rc.1", "1.2.3+build.2"} {
		for _, scanning := range []bool{false, true} {
			missing := outdatedRow{Package: packagemanager.Package{Manager: "mise", Name: "minisign", Requested: version}}
			clean := security.Evidence{Manager: "mise", Package: "minisign", Version: version, VersionSource: "requested", Status: security.StatusClean}
			updateOutdatedSecurity(&missing, &model{securityChecked: true, securityScanning: scanning, allEvidence: []security.Evidence{clean}})
			if missing.Security != "Unknown" || missing.AdvisoryDetails != "" {
				t.Fatalf("unsupported requested version %q (scanning=%t): %+v", version, scanning, missing)
			}
		}
	}
}

func TestOutdatedSecurityExactIdentity(t *testing.T) {
	pkg := packagemanager.Package{Manager: "npm", Name: "tool", Current: "1", Scope: "project", Path: "/project"}
	base := security.Evidence{Manager: pkg.Manager, Package: pkg.Name, Version: pkg.Current, Scope: pkg.Scope, Path: pkg.Path, Status: security.StatusVulnerable, Advisory: "CVE-1"}
	for _, mutate := range []func(*security.Evidence){
		func(e *security.Evidence) { e.Manager = "mise" },
		func(e *security.Evidence) { e.Package = "other" },
		func(e *security.Evidence) { e.Version = "2" },
		func(e *security.Evidence) { e.Scope = "global" },
		func(e *security.Evidence) { e.Path = "/other" },
		func(e *security.Evidence) { e.Path = "" },
		func(e *security.Evidence) { e.Transitive = true },
		func(e *security.Evidence) { e.DependencyClass = "graph-only" },
	} {
		item := base
		mutate(&item)
		row := outdatedRow{Package: pkg}
		updateOutdatedSecurity(&row, &model{securityChecked: true, allEvidence: []security.Evidence{item}})
		if row.Security != "Unknown" || row.AdvisoryDetails != "" {
			t.Fatalf("leaked %+v: %+v", item, row)
		}
	}
}

func TestOutdatedSecurityTransitiveDeclaredTool(t *testing.T) {
	pkg := packagemanager.Package{Manager: "go", Name: "example.com/tool", Current: "v1.0.0", Scope: "project", Path: "/project"}
	for _, tc := range []struct {
		name            string
		declaredTool    bool
		dependencyClass string
		want            string
	}{
		{"declared tool", true, "", "1 advisories"},
		{"regular transitive", false, "", "Unknown"},
		{"graph-only declared tool", true, "graph-only", "Unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := security.Evidence{
				Manager: pkg.Manager, Package: pkg.Name, Version: pkg.Current, Scope: pkg.Scope, Path: pkg.Path,
				Status: security.StatusVulnerable, Advisory: "CVE-2026-1", Transitive: true,
				DeclaredTool: tc.declaredTool, DependencyClass: tc.dependencyClass,
			}
			row := outdatedRow{Package: pkg}
			updateOutdatedSecurity(&row, &model{securityChecked: true, allEvidence: []security.Evidence{item}})
			if row.Security != tc.want {
				t.Fatalf("got %q, want %q", row.Security, tc.want)
			}
			if tc.want == "Unknown" {
				if row.AdvisoryDetails != "" {
					t.Fatalf("excluded evidence leaked into details: %q", row.AdvisoryDetails)
				}
			} else if row.SecurityColor != glyph.Red || !strings.Contains(row.AdvisoryDetails, item.Advisory) {
				t.Fatalf("declared tool finding missing: %+v", row)
			}
		})
	}
}

func TestOutdatedAdvisoryAliasesAndLifecycle(t *testing.T) {
	pkg := packagemanager.Package{Manager: "mise", Name: "node", Current: "20", Scope: "global"}
	items := []security.Evidence{
		{Manager: "mise", Package: "node", Version: "20", Scope: "global", Source: "OSV", Status: security.StatusVulnerable, Advisory: "GHSA-1", Aliases: []string{"CVE-1"}, Score: 9.1, Reference: "reference", CVELink: "cve-link"},
		{Manager: "mise", Package: "node", Version: "20", Scope: "global", Source: "EUVD", Status: security.StatusVulnerable, Advisory: "EUVD-1", Aliases: []string{"CVE-1", "CVE-2"}},
		{Manager: "mise", Package: "node", Version: "20", Scope: "global", Source: "OSV", Status: security.StatusVulnerable, Advisory: "GHSA-2", Aliases: []string{"CVE-2"}},
	}
	if advisoryCount(items) != 1 {
		t.Fatal("aliases not merged transitively")
	}
	data := &model{rows: outdatedRows([]packagemanager.Package{pkg}, nil, nil), packageDetail: pkg, securityScanning: true}
	syncOutdatedSecurity(data)
	if data.rows[0].Security != "Scanning" {
		t.Fatal(data.rows)
	}
	data.securityScanning, data.securityChecked, data.allEvidence = false, true, items
	syncOutdatedSecurity(data)
	if data.rows[0].Security != "1 advisories" || data.rows[0].SecurityColor != glyph.Red {
		t.Fatal(data.rows)
	}
	for _, want := range []string{"OSV", "EUVD", "GHSA-1", "GHSA-2", "CVE-1", "EUVD-1", "9.1", "Affected installed version: 20", "reference", "cve-link"} {
		if !strings.Contains(data.packageDetailText, want) {
			t.Fatalf("missing %q", want)
		}
	}
	data.securityChecked, data.securityErr = false, context.Canceled
	syncOutdatedSecurity(data)
	if data.rows[0].Security != "1 (incomplete)" {
		t.Fatal(data.rows)
	}
	data.allEvidence = nil
	ctx, cancel := context.WithCancel(context.Background())
	data.securityScanning, data.securityCancel = true, cancel
	cancelSecurity(data, glyph.NewApp())
	if ctx.Err() != context.Canceled || data.rows[0].Security != "Canceled" {
		t.Fatal(data.rows)
	}
}

func TestOutdatedSecurityBindingsResize(t *testing.T) {
	defer updateScreenSize(80, 24)
	data := &model{rows: []outdatedRow{
		{Package: packagemanager.Package{Name: "first", Current: "1"}, Security: "Scanning", SecurityColor: glyph.Yellow},
		{Package: packagemanager.Package{Name: "second", Current: "2"}, Security: "Scan failed", SecurityColor: glyph.Red},
	}}
	view, _ := resultView(data)
	template := glyph.Build(view)
	for _, width := range []int{200, 100, 99, 40, 120, 200} {
		updateScreenSize(width, 24)
		buffer := glyph.NewBuffer(width, 24)
		template.Execute(buffer, int16(width), 24)
		for i, name := range []string{"first", "second"} {
			found := false
			for y := range 24 {
				line := buffer.GetLine(y)
				if !strings.Contains(line, name) {
					continue
				}
				found = true
				x := strings.Index(line, data.rows[i].Security)
				if width < 100 {
					if x >= 0 {
						t.Fatal("security visible on narrow terminal")
					}
				} else if x < 0 || buffer.Get(x, y).Style.FG != data.rows[i].SecurityColor {
					t.Fatalf("wrong row binding: %q", line)
				}
			}
			if !found {
				t.Fatalf("missing row %s at width %d", name, width)
			}
		}
		data.rows[0].Security, data.rows[0].SecurityColor = "No known findings", glyph.Green
	}
}
