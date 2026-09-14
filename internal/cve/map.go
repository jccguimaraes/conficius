package cve

func MapToolNameToEUVD(toolName string) []EUVD {
	mapping, ok := productMapping[toolName]
	if !ok {
		return nil
	}

	return mapping
}

var productMapping = map[string][]EUVD{
	"npm": {
		{
			Name:    "npm",
			Product: "NPM",
			Vendor:  "npm",
		},
	},
	"node": {
		{
			Name:    "node",
			Product: "Node",
			Vendor:  "nodejs",
		},
	},
	"mise": {
		{
			Name:    "mise",
			Product: "mise",
			Vendor:  "jdx",
		},
	},
	"openssl": {
		{
			Name:    "openssl",
			Product: "OpenSSL",
			Vendor:  "OpenSSL",
		},
	},
	"goreleaser": {
		{
			Name:    "goreleaser",
			Product: "goreleaser",
			Vendor:  "goreleaser",
		},
	},
	"terraform": {
		{
			Name:    "terraform",
			Product: "Terraform",
			Vendor:  "Hashicorp",
		},
		{
			Name:    "terraform",
			Product: "Terraform Enterprise",
			Vendor:  "Hashicorp",
		},
	},
}
