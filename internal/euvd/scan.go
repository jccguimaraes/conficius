package euvd

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

const (
	StatusClean      = "clean"
	StatusVulnerable = "vulnerable"
	StatusUnknown    = "unknown"
	StatusError      = "error"
	StatusUnmapped   = "unmapped"
)

type Finding struct {
	Manager     string
	Package     string
	Version     string
	Scope       string
	Path        string
	Advisory    string
	UpgradeTo   string
	UpgradeNote string
	Score       float64
	Status      string
	SourceID    string
	Aliases     []string
	Rules       []string
	Reason      string
	Description string
}

type Queryer interface {
	Query(context.Context, Identity, string) ([]Vulnerability, error)
}

func Scan(ctx context.Context, installed []packagemanager.InstalledPackage, outdated []packagemanager.Package, mappings []config.EUVDMapping, client Queryer) ([]Finding, error) {
	latest := make(map[string]string, len(outdated))
	for _, pkg := range outdated {
		latest[key(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)] = pkg.Latest
	}
	overrides := make(map[string][]Identity)
	for _, mapping := range mappings {
		overrides[mapping.Manager+"\x00"+mapping.Package] = append(overrides[mapping.Manager+"\x00"+mapping.Package], Identity{Product: mapping.Product, Vendor: mapping.Vendor})
	}
	var findings []Finding
	var scanErrors []error
	for _, pkg := range installed {
		identities := overrides[pkg.Manager+"\x00"+pkg.Name]
		if len(identities) == 0 {
			if identity, ok := IdentityFor(pkg.Name); ok {
				identities = []Identity{identity}
			}
		}
		if len(identities) == 0 {
			findings = append(findings, Finding{Manager: pkg.Manager, Package: pkg.Name, Version: pkg.Version, Scope: pkg.Scope, Path: pkg.Path, Status: StatusUnmapped, Reason: "no explicit EUVD mapping"})
			continue
		}
		packageFindings := make(map[string]int)
		packageHasOutput := false
		for _, identity := range identities {
			items, err := client.Query(ctx, identity, pkg.Version)
			if err != nil {
				scanErrors = append(scanErrors, fmt.Errorf("%s:%s@%s: %w", pkg.Manager, pkg.Name, pkg.Version, err))
				findings = append(findings, Finding{Manager: pkg.Manager, Package: pkg.Name, Version: pkg.Version, Scope: pkg.Scope, Path: pkg.Path, Status: StatusError, Reason: "request failed"})
				packageHasOutput = true
				continue
			}
			for _, item := range items {
				match, rules := item.Match(identity, pkg.Version)
				status := StatusVulnerable
				if match == UnknownRule {
					status = StatusUnknown
				}
				packageHasOutput = true
				finding := Finding{
					Manager: pkg.Manager, Package: pkg.Name, Version: pkg.Version, Scope: pkg.Scope, Path: pkg.Path,
					Advisory: item.AdvisoryID(), UpgradeTo: latest[key(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)], Score: item.BaseScore,
					Status: status, SourceID: item.ID, Aliases: item.AliasList(), Rules: rules, Description: item.Description,
				}
				finding.UpgradeNote = euvdUpgradeNote(item, identity, finding.UpgradeTo)
				findingKey := item.ID
				if index, exists := packageFindings[findingKey]; exists {
					existing := &findings[index]
					if existing.Status == StatusVulnerable {
						continue
					}
					if finding.Status == StatusVulnerable {
						*existing = finding
					} else {
						for _, rule := range finding.Rules {
							if !slices.Contains(existing.Rules, rule) {
								existing.Rules = append(existing.Rules, rule)
							}
						}
					}
					continue
				}
				packageFindings[findingKey] = len(findings)
				findings = append(findings, finding)
			}
		}
		if !packageHasOutput {
			findings = append(findings, Finding{Manager: pkg.Manager, Package: pkg.Name, Version: pkg.Version, Scope: pkg.Scope, Path: pkg.Path, Status: StatusClean})
		}
	}
	return findings, errors.Join(scanErrors...)
}

func euvdUpgradeNote(item Vulnerability, identity Identity, upgradeTo string) string {
	if upgradeTo == "" {
		return "No newer version found; no confirmed fix available."
	}
	match, _ := item.Match(identity, upgradeTo)
	switch match {
	case Affected:
		return "Latest available version is still affected; it does not fix this CVE."
	case UnknownRule:
		return "Latest available version is not confirmed as a fix."
	default:
		return "Latest available version is not listed as affected by EUVD."
	}
}

func key(scope, path, manager, name string) string {
	return scope + "\x00" + path + "\x00" + manager + "\x00" + name
}
