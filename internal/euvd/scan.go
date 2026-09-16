package euvd

import (
	"context"
	"errors"
	"fmt"

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

func Scan(ctx context.Context, installed []packagemanager.InstalledPackage, outdated []packagemanager.Package, client Queryer) ([]Finding, error) {
	latest := make(map[string]string, len(outdated))
	for _, pkg := range outdated {
		latest[key(pkg.Scope, pkg.Path, pkg.Manager, pkg.Name)] = pkg.Latest
	}
	var findings []Finding
	var scanErrors []error
	for _, pkg := range installed {
		identity, ok := IdentityFor(pkg.Name)
		if !ok {
			continue
		}
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
	return findings, errors.Join(scanErrors...)
}

func key(scope, path, manager, name string) string {
	return scope + "\x00" + path + "\x00" + manager + "\x00" + name
}
