package security

import (
	"context"
	"errors"
	"fmt"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/euvd"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/jccguimaraes/conficius/internal/vulnerability"
)

type Options struct {
	Outdated     []packagemanager.Package
	OSVMappings  []config.VulnerabilityMapping
	EUVDMappings []config.EUVDMapping
	OSVClient    vulnerability.Queryer
	EUVDClient   euvd.Queryer
}

type Summary struct {
	Vulnerable int `json:"vulnerable"`
	Unmapped   int `json:"unmapped"`
	Unknown    int `json:"unknown"`
	Errors     int `json:"errors"`
}

type Report struct {
	Evidence []Evidence `json:"evidence"`
	Summary  Summary    `json:"summary"`
}

func Scan(ctx context.Context, inventory []packagemanager.InstalledPackage, options Options) (Report, error) {
	type result struct {
		source   string
		evidence []Evidence
		err      error
	}
	results := make(chan result, 2)
	go func() {
		findings, err := vulnerability.Scan(ctx, inventory, options.OSVMappings, options.OSVClient)
		results <- result{source: SourceOSV, evidence: osvEvidence(findings), err: err}
	}()
	go func() {
		findings, err := euvd.Scan(ctx, inventory, options.Outdated, options.EUVDMappings, options.EUVDClient)
		results <- result{source: SourceEUVD, evidence: euvdEvidence(findings), err: err}
	}()

	var report Report
	var scanErrors []error
	for range 2 {
		result := <-results
		report.Evidence = append(report.Evidence, result.evidence...)
		if result.err != nil {
			scanErrors = append(scanErrors, fmt.Errorf("%s scan: %w", result.source, result.err))
		}
	}
	Sort(report.Evidence)
	report.Summary = summarize(report.Evidence)
	return report, errors.Join(scanErrors...)
}

func osvEvidence(findings []vulnerability.Finding) []Evidence {
	var evidence []Evidence
	for _, finding := range findings {
		if len(finding.Advisories) == 0 {
			evidence = append(evidence, Evidence{Source: SourceOSV, Manager: finding.Manager, Package: finding.Package, Version: finding.Version, Scope: finding.Scope, Path: finding.Path, Status: normalizeStatus(finding.Status), Reason: finding.Reason})
			continue
		}
		for i, advisory := range finding.Advisories {
			item := Evidence{Source: SourceOSV, Manager: finding.Manager, Package: finding.Package, Version: finding.Version, Scope: finding.Scope, Path: finding.Path, Status: normalizeStatus(finding.Status), Advisory: advisory.ID, SourceID: advisory.ID, UpgradeTo: advisory.FixedVersion, Score: advisory.Score}
			if i < len(finding.Vulnerabilities) {
				item.SourceID = finding.Vulnerabilities[i].ID
				item.Aliases = append([]string(nil), finding.Vulnerabilities[i].Aliases...)
			}
			evidence = append(evidence, item)
		}
	}
	return evidence
}

func euvdEvidence(findings []euvd.Finding) []Evidence {
	evidence := make([]Evidence, len(findings))
	for i, finding := range findings {
		evidence[i] = Evidence{Source: SourceEUVD, Manager: finding.Manager, Package: finding.Package, Version: finding.Version, Scope: finding.Scope, Status: normalizeStatus(finding.Status), Advisory: finding.Advisory, SourceID: finding.SourceID, Aliases: append([]string(nil), finding.Aliases...), UpgradeTo: finding.UpgradeTo, AffectedRule: finding.Rule, Reason: finding.Reason, Score: finding.Score}
	}
	return evidence
}

func normalizeStatus(status string) string {
	if status == vulnerability.StatusUnknown {
		return StatusUnknownVersionRule
	}
	return status
}

func summarize(evidence []Evidence) Summary {
	var summary Summary
	for _, item := range evidence {
		switch item.Status {
		case StatusVulnerable:
			summary.Vulnerable++
		case StatusUnmapped:
			summary.Unmapped++
		case StatusUnknownVersionRule:
			summary.Unknown++
		case StatusError:
			summary.Errors++
		}
	}
	return summary
}
