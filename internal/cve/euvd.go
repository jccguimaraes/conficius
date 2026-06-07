package cve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const euvdSearchURL = "https://euvdservices.enisa.europa.eu/api/search"
const euvdDefaultPageSize = 100

type EUVDSearchResponse struct {
	Items []EUVDVulnerability `json:"items"`
	Total int                 `json:"total"`
}

type EUVDVulnerability struct {
	ID               string             `json:"id"`
	ENISAUUID        string             `json:"enisaUuid"`
	Description      string             `json:"description"`
	DatePublished    string             `json:"datePublished"`
	DateUpdated      string             `json:"dateUpdated"`
	BaseScore        float64            `json:"baseScore"`
	BaseScoreVersion string             `json:"baseScoreVersion"`
	BaseScoreVector  string             `json:"baseScoreVector"`
	References       string             `json:"references"`
	Aliases          string             `json:"aliases"`
	Assigner         string             `json:"assigner"`
	EPSS             float64            `json:"epss"`
	Products         []EUVDProductEntry `json:"enisaIdProduct"`
	Vendors          []EUVDVendorEntry  `json:"enisaIdVendor"`
}

type EUVDProductEntry struct {
	ID             string      `json:"id"`
	Product        EUVDProduct `json:"product"`
	ProductVersion string      `json:"product_version"`
}

type EUVDProduct struct {
	Name   string     `json:"name"`
	Vendor EUVDVendor `json:"vendor"`
}

type EUVDVendorEntry struct {
	ID     string     `json:"id"`
	Vendor EUVDVendor `json:"vendor"`
}

type EUVDVendor struct {
	Name string `json:"name"`
}

type EUVDInfo struct {
	CVEID            string
	Severity         string
	Description      string
	Link             string
	AffectedVersions []string
}

type EUVDResult struct {
	Direct  []EUVDInfo
	Related []EUVDInfo
}

type Request struct {
	Name    string
	Version string
}

func NewRequest(name string) *Request {
	return &Request{
		Name: name,
	}
}

func NewRequestWithVersion(name string, version string) *Request {
	return &Request{
		Name:    name,
		Version: version,
	}
}

type EUV struct {
	requests map[string]*Request
}

func NewEUV(requests map[string]*Request) *EUV {
	return &EUV{
		requests: requests,
	}
}

func (euv *EUV) SetProductRequestVersion(request *Request, version string) error {
	request, ok := euv.requests[request.Name]

	if !ok {
		return fmt.Errorf("request '%s' not found", request.Name)
	}

	request.Version = version

	return nil
}

func Do() {}

func ReadEUVD(product string) (EUVDResult, error) {
	return ReadEUVDPageSize(product, euvdDefaultPageSize)
}

func ReadEUVDVersion(product string, version string) (EUVDResult, error) {
	items, err := readEUVDItems(product, euvdDefaultPageSize)
	if err != nil {
		return EUVDResult{}, err
	}

	var result EUVDResult
	for _, item := range items {
		if item.AffectsProductVersion(product, version) {
			result.Direct = append(result.Direct, item.Info())
		} else if !item.MatchesProduct(product) {
			result.Related = append(result.Related, item.Info())
		}
	}

	return result, nil
}

func ReadEUVDPageSize(product string, size int) (EUVDResult, error) {
	items, err := readEUVDItems(product, size)
	if err != nil {
		return EUVDResult{}, err
	}

	var result EUVDResult
	for _, item := range items {
		if item.MatchesProduct(product) {
			result.Direct = append(result.Direct, item.Info())
		} else {
			result.Related = append(result.Related, item.Info())
		}
	}

	return result, nil
}

func readEUVDItems(product string, size int) ([]EUVDVulnerability, error) {
	if size <= 0 {
		size = euvdDefaultPageSize
	}

	var items []EUVDVulnerability

	for page := 0; ; page++ {
		result, err := readEUVDPage(product, page, size)
		if err != nil {
			return nil, err
		}

		items = append(items, result.Items...)

		if len(items) >= result.Total || len(result.Items) == 0 {
			return items, nil
		}
	}
}

func readEUVDPage(product string, page int, size int) (EUVDSearchResponse, error) {
	u, err := url.Parse(euvdSearchURL)
	if err != nil {
		return EUVDSearchResponse{}, err
	}

	q := u.Query()
	q.Set("product", product)
	q.Set("fromScore", "6")
	q.Set("toScore", "10")
	q.Set("page", strconv.Itoa(page))
	q.Set("size", strconv.Itoa(size))
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return EUVDSearchResponse{}, err
	}
	req.Header.Set("User-Agent", "conficius/0.1 (+https://github.com/jccguimaraes/conficius)")
	req.Header.Set("Accept", "*/*")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return EUVDSearchResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return EUVDSearchResponse{}, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	var result EUVDSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return EUVDSearchResponse{}, err
	}

	return result, nil
}

func (v EUVDVulnerability) Info() EUVDInfo {
	cveID := firstLine(v.Aliases)
	if cveID == "" {
		cveID = v.ID
	}

	return EUVDInfo{
		CVEID:            cveID,
		Severity:         v.Severity(),
		Description:      v.Description,
		Link:             v.Link(),
		AffectedVersions: v.AffectedVersions(),
	}
}

func (v EUVDVulnerability) Severity() string {
	if v.BaseScore == 0 {
		return ""
	}

	return fmt.Sprintf("CVSS:%s %.1f", v.BaseScoreVersion, v.BaseScore)
}

func (v EUVDVulnerability) Link() string {
	for ref := range strings.Lines(v.References) {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			return ref
		}
	}

	if v.ID == "" {
		return ""
	}

	return "https://euvd.enisa.europa.eu/vulnerability/" + v.ID
}

func (v EUVDVulnerability) AffectedVersions() []string {
	versions := make([]string, 0, len(v.Products))
	seen := make(map[string]bool)

	for _, product := range v.Products {
		version := strings.TrimSpace(product.ProductVersion)
		if version == "" || seen[version] {
			continue
		}

		versions = append(versions, version)
		seen[version] = true
	}

	return versions
}

func (v EUVDVulnerability) AffectsVersion(version string) bool {
	for _, product := range v.Products {
		if versionMatchesEUVDRange(version, product.ProductVersion) {
			return true
		}
	}

	return false
}

func (v EUVDVulnerability) MatchesProduct(name string) bool {
	name = normalizeProductName(name)
	if name == "" {
		return false
	}

	for _, product := range v.Products {
		if normalizeProductName(product.Product.Name) == name {
			return true
		}
	}

	return false
}

func (v EUVDVulnerability) AffectsProductVersion(name string, version string) bool {
	name = normalizeProductName(name)
	if name == "" {
		return false
	}

	for _, product := range v.Products {
		if normalizeProductName(product.Product.Name) == name && versionMatchesEUVDRange(version, product.ProductVersion) {
			return true
		}
	}

	return false
}

func versionMatchesEUVDRange(version string, rule string) bool {
	version = strings.TrimSpace(version)
	rule = strings.TrimSpace(rule)
	if version == "" || rule == "" || rule == "n/a" {
		return false
	}

	matched := false
	for part := range strings.SplitSeq(rule, ",") {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "0 ")
		if part == "" || part == "n/a" {
			continue
		}

		if strings.HasPrefix(part, "<=") || strings.HasPrefix(part, "≤") {
			if compareVersion(version, strings.TrimLeft(part, "<=≤ ")) > 0 {
				return false
			}
			matched = true
			continue
		}
		if strings.HasPrefix(part, "<") {
			if compareVersion(version, strings.TrimLeft(part, "< ")) >= 0 {
				return false
			}
			matched = true
			continue
		}
		if strings.HasPrefix(part, ">=") || strings.HasPrefix(part, "≥") {
			if compareVersion(version, strings.TrimLeft(part, ">=≥ ")) < 0 {
				return false
			}
			matched = true
			continue
		}
		if strings.HasPrefix(part, ">") {
			if compareVersion(version, strings.TrimLeft(part, "> ")) <= 0 {
				return false
			}
			matched = true
			continue
		}

		if strings.Contains(rule, ",") {
			if compareVersion(version, part) < 0 {
				return false
			}
			matched = true
			continue
		}

		return compareVersion(version, part) == 0
	}

	return matched
}

func compareVersion(a string, b string) int {
	aParts := versionParts(a)
	bParts := versionParts(b)
	max := max(len(aParts), len(bParts))

	for i := range max {
		var av, bv int
		if i < len(aParts) {
			av = aParts[i]
		}
		if i < len(bParts) {
			bv = bParts[i]
		}

		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}

	return 0
}

func versionParts(version string) []int {
	fields := strings.FieldsFunc(version, func(r rune) bool {
		return r < '0' || r > '9'
	})

	parts := make([]int, 0, len(fields))
	for _, field := range fields {
		part, err := strconv.Atoi(field)
		if err == nil {
			parts = append(parts, part)
		}
	}

	return parts
}

func normalizeProductName(name string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return -1
	}, name)
}

func firstLine(value string) string {
	for line := range strings.Lines(value) {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}

	return ""
}
