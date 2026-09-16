package security

import "sort"

const (
	SourceOSV  = "OSV"
	SourceEUVD = "EUVD"

	StatusVulnerable         = "vulnerable"
	StatusUnknownVersionRule = "unknown-version-rule"
	StatusError              = "error"
	StatusUnmapped           = "unmapped"
	StatusClean              = "clean"
)

type Evidence struct {
	Source       string   `json:"source"`
	Manager      string   `json:"manager"`
	Package      string   `json:"package"`
	Version      string   `json:"version"`
	Scope        string   `json:"scope,omitempty"`
	Path         string   `json:"path,omitempty"`
	Status       string   `json:"status"`
	Advisory     string   `json:"advisory,omitempty"`
	SourceID     string   `json:"source_id,omitempty"`
	Aliases      []string `json:"aliases,omitempty"`
	UpgradeTo    string   `json:"upgrade_to,omitempty"`
	AffectedRule string   `json:"affected_rule,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	Score        float64  `json:"score,omitempty"`
}

func Sort(items []Evidence) {
	rank := map[string]int{StatusVulnerable: 0, StatusUnknownVersionRule: 1, StatusError: 2, StatusUnmapped: 3, StatusClean: 4}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if rank[a.Status] != rank[b.Status] {
			return rank[a.Status] < rank[b.Status]
		}
		if a.Manager != b.Manager {
			return a.Manager < b.Manager
		}
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		if a.Advisory != b.Advisory {
			return a.Advisory < b.Advisory
		}
		return a.Source < b.Source
	})
}
