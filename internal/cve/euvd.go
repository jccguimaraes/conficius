package cve

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jccguimaraes/conficius/internal/packagemanager"

	gocvss30 "github.com/pandatix/go-cvss/30"
	gocvss31 "github.com/pandatix/go-cvss/31"
	gocvss40 "github.com/pandatix/go-cvss/40"
)

const euvdSearchURL = "https://euvdservices.enisa.europa.eu/api/search"

type EUVDList map[string]EUVD

type EUVD struct {
	Name    string
	Text    string
	Product string
	Vendor  string
}

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

func (t EUVD) Do(page int, size int) (EUVDSearchResponse, error) {
	u, err := url.Parse(euvdSearchURL)
	if err != nil {
		return EUVDSearchResponse{}, err
	}

	q := u.Query()

	if t.Product != "" && t.Vendor != "" {
		q.Set("vendor", t.Vendor)
	} else {
		q.Set("text", t.Text)
	}

	q.Set("fromScore", "5")
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

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return EUVDSearchResponse{}, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	var result EUVDSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return EUVDSearchResponse{}, err
	}

	return result, nil
}

func (list EUVDList) Aggregate(outdatedList packagemanager.Packages, writer io.Writer) error {
	for _, tools := range outdatedList {
		for _, outdated := range tools {
			toolName := outdated.GetName()
			listMapping := MapToolNameToEUVD(toolName)

			if len(listMapping) == 0 {
				// fmt.Fprintf(writer, "did not find any product/vendor on EUVD for '%s', doing broader search!\n", toolName)

				list[toolName] = EUVD{
					Name: toolName,
					Text: toolName,
				}
			}

			for _, l := range listMapping {
				// fmt.Fprintf(writer, "found product/vendor on EUVD for '%s', doing broader search!\n", l)

				if _, ok := list[toolName]; !ok {
					list[toolName] = l
				}
			}
		}
	}

	return nil
}

type Vulnerability struct {
	EUVDVulnerability EUVDVulnerability
	Query             EUVD
}

func (v Vulnerability) GetVersions() []string {
	mapping := make(map[string]struct{})

	for _, p := range v.EUVDVulnerability.Products {
		if _, ok := mapping[p.ProductVersion]; !ok {
			mapping[p.ProductVersion] = struct{}{}
		}
	}

	var list []string

	for i := range mapping {
		list = append(list, i)
	}

	return list
}

func (list EUVDList) ListCVEs(max int) error {
	items := []Vulnerability{}

	for _, query := range list {
		// if query.Product != "NPM" {
		// 	continue
		// }

		// fmt.Println("Product:", query.Product, "Vendor:", query.Vendor, "Text:", query.Text)
		readTotal := false
		totalRead := 0
		page := 0

		for !readTotal {
			res, err := query.Do(page, 100)
			if err != nil {
				return err
			}

			for _, item := range res.Items {
				for _, product := range item.Products {
					isProductAccurate := query.Product != "" && product.Product.Name == query.Product
					isVendorAccurate := query.Vendor != "" && product.Product.Vendor.Name == query.Vendor
					// fmt.Println("\t\tProduct", product.Product.Name, "Vendor", product.Product.Vendor.Name)

					if query.Text != "" {
						// items = append(items, item)
						// fmt.Println("\t\tAdded Related")
						// fmt.Println("\t\tSkipped")
						continue
					}

					if !isProductAccurate || !isVendorAccurate {
						// fmt.Println("\t\tSkipped")
						continue
					}

					// fmt.Println("\t\tAdded Match")
					items = append(items, Vulnerability{
						EUVDVulnerability: item,
						Query:             query,
					})
					break
				}
			}

			totalRead += len(res.Items)

			if totalRead >= max {
				readTotal = true
			}

			if totalRead >= res.Total {
				readTotal = true
			}

			// fmt.Println(res.Total, page, totalRead)
			page++
		}
	}

	fmt.Println("TOTAL:", len(items))

	for _, item := range items {
		var baseScore float64
		var rate string

		switch item.EUVDVulnerability.BaseScoreVersion {
		case "3.0":
			cvss, err := gocvss30.ParseVector(item.EUVDVulnerability.BaseScoreVector)
			if err != nil {
				return err
			}

			baseScore = cvss.BaseScore()
			rate, err = gocvss31.Rating(baseScore)
			if err != nil {
				return err
			}

		case "3.1":
			cvss, err := gocvss31.ParseVector(item.EUVDVulnerability.BaseScoreVector)
			if err != nil {
				return err
			}

			baseScore = cvss.BaseScore()
			rate, err = gocvss31.Rating(baseScore)
			if err != nil {
				return err
			}

		case "4.0":
			cvss, err := gocvss40.ParseVector(item.EUVDVulnerability.BaseScoreVector)
			if err != nil {
				return err
			}

			baseScore = cvss.Score()
			rate, err = gocvss31.Rating(baseScore)
			if err != nil {
				return err
			}

		}

		cveID := ""

		for id := range strings.SplitSeq(item.EUVDVulnerability.Aliases, "\n") {
			if strings.Contains(id, "CVE-") {
				cveID = id
				break
			}
		}

		// fmt.Println("TOOL", item.Query.Name)
		// fmt.Println("\tCVE:", cveID)
		// fmt.Println("\tSCORE:", baseScore, "(", rate, ")")
		// fmt.Println("\tVERSIONS:")
		// for _, v := range item.GetVersions() {
		// 	fmt.Printf("\t\t%s\n", v)
		// }
		fmt.Print("TOOL: ", item.Query.Name)
		fmt.Print(" CVE:", cveID)
		fmt.Print(" SCORE:", baseScore, "(", rate, ")")
		fmt.Print(" VERSIONS:")
		for _, v := range item.GetVersions() {
			fmt.Printf(" %s, ", v)
		}
		fmt.Println("\n========================================================================")
	}

	return nil
}
