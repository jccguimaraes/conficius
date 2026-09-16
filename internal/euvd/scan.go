package euvd

import (
	"context"
	"errors"
	"fmt"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

type Finding struct {
	Manager   string
	Package   string
	Version   string
	Scope     string
	Advisory  string
	UpgradeTo string
	Score     float64
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
			continue
		}
		for _, identity := range identities {
			items, err := client.Query(ctx, identity, pkg.Version)
			if err != nil {
				scanErrors = append(scanErrors, fmt.Errorf("%s:%s@%s: %w", pkg.Manager, pkg.Name, pkg.Version, err))
				continue
			}
			for _, item := range items {
				findings = append(findings, Finding{
					Manager: pkg.Manager, Package: pkg.Name, Version: pkg.Version, Scope: pkg.Scope,
					Advisory: item.AdvisoryID(), UpgradeTo: latest[key(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)], Score: item.BaseScore,
				})
			}
		}
	}
	return findings, errors.Join(scanErrors...)
}

func key(scope, path, manager, name string) string {
	return scope + "\x00" + path + "\x00" + manager + "\x00" + name
}
