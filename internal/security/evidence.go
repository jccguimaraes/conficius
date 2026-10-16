package security

import "sort"

const (
	SourceOSV  = "OSV"
	SourceEUVD = "EUVD"

	StatusVulnerable = "vulnerable"
	StatusUnknown    = "unknown"
	StatusError      = "error"
	StatusUnmapped   = "unmapped"
	StatusClean      = "clean"
)

type Evidence struct {
	DependencyClass string   `json:"dependency_class,omitempty"`
	ScanScope       string   `json:"scan_scope,omitempty"`
	Source          string   `json:"source"`
	Manager         string   `json:"manager"`
	Package         string   `json:"package"`
	Version         string   `json:"version"`
	Scope           string   `json:"scope,omitempty"`
	Path            string   `json:"path,omitempty"`
	Status          string   `json:"status"`
	Advisory        string   `json:"advisory,omitempty"`
	SourceID        string   `json:"source_id,omitempty"`
	Aliases         []string `json:"aliases,omitempty"`
	UpgradeTo       string   `json:"upgrade_to,omitempty"`
	UpgradeNote     string   `json:"upgrade_note,omitempty"`
	Rules           []string `json:"rules,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	Description     string   `json:"description,omitempty"`
	Reference       string   `json:"reference,omitempty"`
	CVELink         string   `json:"cve_link,omitempty"`
	Score           float64  `json:"score,omitempty"`
}

func Sort(items []Evidence) {
	rank := map[string]int{StatusVulnerable: 0, StatusUnknown: 1, StatusError: 2, StatusUnmapped: 3, StatusClean: 4}
	maxScore := make(map[string]float64)
	for _, item := range items {
		key := evidenceGroupKey(item)
		if item.Score > maxScore[key] {
			maxScore[key] = item.Score
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		aKey, bKey := evidenceGroupKey(a), evidenceGroupKey(b)
		if maxScore[aKey] != maxScore[bKey] {
			return maxScore[aKey] > maxScore[bKey]
		}
		if aKey != bKey {
			return aKey < bKey
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if rank[a.Status] != rank[b.Status] {
			return rank[a.Status] < rank[b.Status]
		}
		if a.Advisory != b.Advisory {
			return a.Advisory < b.Advisory
		}
		return a.Source < b.Source
	})
}

func evidenceGroupKey(item Evidence) string {
	return item.Manager + "\x00" + item.Package + "\x00" + item.Version
}
