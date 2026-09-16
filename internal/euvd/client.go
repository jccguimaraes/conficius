package euvd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	return &Client{httpClient: httpClient, baseURL: baseURL}
}

func (client *Client) Query(ctx context.Context, identity Identity, version string) ([]Vulnerability, error) {
	const pageSize = 100
	var matched []Vulnerability
	for page := 0; ; page++ {
		u, err := url.Parse(client.baseURL)
		if err != nil {
			return nil, fmt.Errorf("parse EUVD URL: %w", err)
		}
		query := u.Query()
		query.Set("product", identity.Product)
		query.Set("vendor", identity.Vendor)
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
		for _, item := range body.Items {
			if item.Affects(identity, version) {
				matched = append(matched, item)
			}
		}
		if (page+1)*pageSize >= body.Total || len(body.Items) == 0 {
			return matched, nil
		}
	}
}

func (item Vulnerability) Affects(identity Identity, version string) bool {
	for _, product := range item.Products {
		if !strings.EqualFold(product.Product.Name, identity.Product) || !strings.EqualFold(product.Product.Vendor.Name, identity.Vendor) {
			continue
		}
		if versionMatches(version, product.ProductVersion) {
			return true
		}
	}
	return false
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

func versionMatches(version, rule string) bool {
	version, rule = strings.TrimSpace(version), strings.TrimSpace(rule)
	if version == "" || rule == "" || rule == "n/a" {
		return false
	}
	for _, token := range []string{"≤", "<="} {
		if before, upper, ok := strings.Cut(rule, token); ok {
			lower := strings.TrimSpace(strings.TrimPrefix(before, "0"))
			return (lower == "" || compare(version, lower) >= 0) && compare(version, strings.TrimSpace(upper)) <= 0
		}
	}
	return compare(version, rule) == 0
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
