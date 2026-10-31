package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/jccguimaraes/conficius/internal/security"
	"github.com/kungfusheep/glyph"
)

func syncOutdatedSecurity(data *model) {
	for i := range data.rows {
		row := &data.rows[i]
		updateOutdatedSecurity(row, data)
		if row.Package == data.packageDetail {
			data.packageDetailText = packageDetails(row)
		}
	}
}

func updateOutdatedSecurity(row *outdatedRow, data *model) {
	row.Security, row.SecurityColor, row.AdvisoryDetails = "Unknown", glyph.Yellow, ""
	pkg := row.Package
	if pkg.Manager == packagemanager.PM_MISE && pkg.Current == "" {
		row.Security = "Not installed"
		return
	}
	var findings []security.Evidence
	clean, incomplete := false, false
	seen := make(map[string]bool)
	var details []string
	for _, item := range data.allEvidence {
		if item.Manager != pkg.Manager || item.Package != pkg.Name || item.Version != pkg.Current || item.Scope != pkg.Scope || item.Path != pkg.Path || item.Transitive && !item.DeclaredTool || item.DependencyClass == "graph-only" {
			continue
		}
		key := fmt.Sprintf("%+v", item)
		if seen[key] {
			continue
		}
		seen[key] = true
		switch item.Status {
		case security.StatusVulnerable:
			findings = append(findings, item)
		case security.StatusClean:
			clean = true
		default:
			incomplete = true
		}
		details = append(details, fmt.Sprintf("Source: %s\nStatus: %s\nAffected installed version: %s\nAdvisory: %s\nSource ID: %s\nAliases: %s\nCVSS: %g\nDescription: %s\nReference: %s\nCVE URL: %s\nReason: %s\nUnsupported rules: %s", item.Source, item.Status, item.Version, item.Advisory, item.SourceID, strings.Join(item.Aliases, ", "), item.Score, item.Description, item.Reference, item.CVELink, item.Reason, strings.Join(item.Rules, ", ")))
	}
	if len(details) > 0 {
		row.AdvisoryDetails = "Installed-version security evidence; latest version is not verified fixed.\n\n" + strings.Join(details, "\n\n")
	}
	canceled := errors.Is(data.securityErr, context.Canceled)
	switch {
	case data.securityScanning:
		row.Security = "Scanning"
	case len(findings) > 0:
		row.Security = fmt.Sprintf("%d advisories", advisoryCount(findings))
		row.SecurityColor = glyph.Red
		if incomplete || data.securityErr != nil || !data.securityChecked {
			row.Security = fmt.Sprintf("%d (incomplete)", advisoryCount(findings))
		}
	case canceled:
		row.Security = "Canceled"
	case data.securityErr != nil:
		row.Security, row.SecurityColor = "Scan failed", glyph.Red
	case data.securityChecked && clean && !incomplete:
		row.Security, row.SecurityColor = "No known findings", glyph.Green
	}
}

func advisoryCount(items []security.Evidence) int {
	parents := make([]int, len(items))
	root := func(i int) int {
		for parents[i] != i {
			i = parents[i]
		}
		return i
	}
	ids := make(map[string]int)
	for i, item := range items {
		parents[i] = i
		for _, id := range append([]string{item.Advisory, item.SourceID}, item.Aliases...) {
			id = strings.ToUpper(strings.TrimSpace(id))
			if id == "" {
				continue
			}
			if previous, ok := ids[id]; ok {
				parents[root(i)] = root(previous)
			} else {
				ids[id] = i
			}
		}
	}
	count := 0
	for i := range parents {
		if root(i) == i {
			count++
		}
	}
	return count
}
