package euvd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultBaseURL = "https://euvdservices.enisa.europa.eu/api/search"
	DefaultTimeout = 15 * time.Second
)

type Identity struct {
	Product string
	Vendor  string
}

var identities = map[string]Identity{
	"npm":        {Product: "NPM", Vendor: "npm"},
	"node":       {Product: "Node", Vendor: "nodejs"},
	"mise":       {Product: "mise", Vendor: "jdx"},
	"openssl":    {Product: "OpenSSL", Vendor: "OpenSSL"},
	"goreleaser": {Product: "goreleaser", Vendor: "goreleaser"},
	"terraform":  {Product: "Terraform", Vendor: "Hashicorp"},
}

func IdentityFor(name string) (Identity, bool) {
	identity, ok := identities[name]
	return identity, ok
}

type ProductEntry struct {
	Product struct {
		Name   string `json:"name"`
		Vendor struct {
			Name string `json:"name"`
		} `json:"vendor"`
	} `json:"product"`
	ProductVersion string `json:"product_version"`
}

type Vulnerability struct {
	ID          string         `json:"id"`
	Aliases     string         `json:"aliases"`
	Description string         `json:"description"`
	BaseScore   float64        `json:"baseScore"`
	Products    []ProductEntry `json:"enisaIdProduct"`
}

type response struct {
	Items []Vulnerability `json:"items"`
	Total int             `json:"total"`
}

type Client struct {
	httpClient *http.Client
	baseURL    string
	cacheMu    sync.RWMutex
	cache      map[string][]Vulnerability
}

func NewClient(httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	} else if httpClient.Timeout == 0 {
		copy := *httpClient
		copy.Timeout = DefaultTimeout
		httpClient = &copy
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{httpClient: httpClient, baseURL: baseURL, cache: make(map[string][]Vulnerability)}
}

func (client *Client) Query(ctx context.Context, identity Identity, version string) ([]Vulnerability, error) {
	items, err := client.queryProduct(ctx, identity.Product)
	if err != nil {
		return nil, err
	}
	var matched []Vulnerability
	for _, item := range items {
		match, _ := item.Match(identity, version)
		if match != NoMatch {
			matched = append(matched, item)
		}
	}
	return matched, nil
}

func (client *Client) ClearCache() {
	client.cacheMu.Lock()
	defer client.cacheMu.Unlock()
	client.cache = make(map[string][]Vulnerability)
}

func (client *Client) queryProduct(ctx context.Context, product string) ([]Vulnerability, error) {
	key := strings.ToLower(strings.TrimSpace(product))
	client.cacheMu.RLock()
	cached, ok := client.cache[key]
	client.cacheMu.RUnlock()
	if ok {
		return cached, nil
	}
	const pageSize = 100
	var items []Vulnerability
	for page := 0; ; page++ {
		u, err := url.Parse(client.baseURL)
		if err != nil {
			return nil, fmt.Errorf("parse EUVD URL: %w", err)
		}
		query := u.Query()
		query.Set("product", product)
		query.Set("page", strconv.Itoa(page))
		query.Set("size", strconv.Itoa(pageSize))
		u.RawQuery = query.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("create EUVD query: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "conficius/dev (+https://github.com/jccguimaraes/conficius)")
		res, err := client.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("query EUVD: %w", err)
		}
		var body response
		decodeErr := json.NewDecoder(res.Body).Decode(&body)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("query EUVD: %s", res.Status)
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("decode EUVD response: %w", decodeErr)
		}
		items = append(items, body.Items...)
		if (page+1)*pageSize >= body.Total || len(body.Items) == 0 {
			client.cacheMu.Lock()
			client.cache[key] = items
			client.cacheMu.Unlock()
			return items, nil
		}
	}
}

type Match int

const (
	NoMatch Match = iota
	Affected
	UnknownRule
)

func (item Vulnerability) Match(identity Identity, version string) (Match, []string) {
	var unsupportedRules []string
	for _, product := range item.Products {
		if !strings.EqualFold(product.Product.Name, identity.Product) || !strings.EqualFold(product.Product.Vendor.Name, identity.Vendor) {
			continue
		}
		rule := strings.TrimSpace(product.ProductVersion)
		if rule == "" {
			continue
		}
		matched, supported := versionMatches(version, rule)
		if matched {
			return Affected, nil
		}
		if !supported && !slices.Contains(unsupportedRules, rule) {
			unsupportedRules = append(unsupportedRules, rule)
		}
	}
	if len(unsupportedRules) > 0 {
		return UnknownRule, unsupportedRules
	}
	return NoMatch, nil
}

func (item Vulnerability) Affects(identity Identity, version string) bool {
	match, _ := item.Match(identity, version)
	return match == Affected
}

func (item Vulnerability) AliasList() []string {
	var aliases []string
	for line := range strings.Lines(item.Aliases) {
		if line = strings.TrimSpace(line); line != "" {
			aliases = append(aliases, line)
		}
	}
	return aliases
}

func (item Vulnerability) AdvisoryID() string {
	for line := range strings.Lines(item.Aliases) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CVE-") {
			return line
		}
	}
	return item.ID
}

func versionMatches(version, rule string) (bool, bool) {
	version, rule = strings.TrimSpace(version), strings.TrimSpace(rule)
	if version == "" || rule == "" || rule == "n/a" {
		return false, false
	}
	for _, token := range []string{"≤", "<="} {
		if before, upper, ok := strings.Cut(rule, token); ok {
			lower := strings.TrimSpace(strings.TrimPrefix(before, "0"))
			return (lower == "" || compare(version, lower) >= 0) && compare(version, strings.TrimSpace(upper)) <= 0, true
		}
	}
	lowerRule := strings.ToLower(rule)
	if strings.HasPrefix(lowerRule, "before ") {
		return compare(version, strings.TrimSpace(rule[len("before "):])) < 0, true
	}
	if strings.Contains(lowerRule, " through ") {
		bounds := strings.SplitN(lowerRule, " through ", 2)
		return compare(version, strings.TrimSpace(bounds[0])) >= 0 && compare(version, strings.TrimSpace(bounds[1])) <= 0, true
	}
	if strings.HasPrefix(rule, "<=") || strings.HasPrefix(rule, "≤") {
		return compare(version, strings.TrimLeft(rule, "<=≤ ")) <= 0, true
	}
	if strings.HasPrefix(rule, "<") {
		return compare(version, strings.TrimLeft(rule, "< ")) < 0, true
	}
	if lower, upper, ok := strings.Cut(rule, " <"); ok {
		lower = strings.TrimSpace(lower)
		upper = strings.TrimSpace(strings.TrimSuffix(upper, "*"))
		return compare(version, lower) >= 0 && compare(version, upper) < 0, true
	}
	if strings.ContainsAny(rule, "*>") {
		return false, false
	}
	return compare(version, rule) == 0, true
}

func compare(a, b string) int {
	ap, bp := parts(a), parts(b)
	for i := range max(len(ap), len(bp)) {
		var av, bv int
		if i < len(ap) {
			av = ap[i]
		}
		if i < len(bp) {
			bv = bp[i]
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

func parts(version string) []int {
	fields := strings.FieldsFunc(version, func(r rune) bool { return r < '0' || r > '9' })
	result := make([]int, 0, len(fields))
	for _, field := range fields {
		if value, err := strconv.Atoi(field); err == nil {
			result = append(result, value)
		}
	}
	return result
}
