package main

import (
	"context"
	"fmt"
	"strings"

	urfave "github.com/urfave/cli/v3"

	"github.com/jccguimaraes/conficius/internal/audit"
	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/cve"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

func (cli *Cli) packages() *urfave.Command {
	return &urfave.Command{
		Name:        "packages",
		Description: "Inspect installed package managers, outdated tools, and vulnerability exposure.",
		Usage:       "Inspect package tool state.",
		Action: func(ctx context.Context, cmd *urfave.Command) error {
			return nil
		},
		Commands: []*urfave.Command{
			{
				Name:        "pms",
				Description: "List supported package managers.",
				Usage:       "Show supported package managers.",
				Action: func(ctx context.Context, c *urfave.Command) error {
					_, err := fmt.Fprintln(c.Writer, strings.Join(packagemanager.RegisteredPackageManagers, ", "))

					return err
				},
			},
			{
				Name:        "outdated",
				Description: "List installed tools with available updates across supported package managers.",
				Usage:       "Show outdated tools.",
				Flags: []urfave.Flag{
					&urfave.StringSliceFlag{
						Name:  "pm",
						Usage: "Pass multiple package managers (none if include all package managers)",
						Action: func(ctx context.Context, cmd *urfave.Command, pms []string) error {
							return packagemanager.Supported(pms)
						},
					},
				},
				Action: func(ctx context.Context, c *urfave.Command) error {
					snapshot, err := config.Current()
					if err != nil {
						return err
					}

					pms := c.StringSlice("pm")

					if len(pms) == 0 {
						pms = packagemanager.RegisteredPackageManagers
					}

					outdatedList, err := packagemanager.ReadOutdated(snapshot, pms)
					if err != nil {
						return err
					}

					if err := audit.PrintOutdated(outdatedList, c.Writer); err != nil {
						return err
					}

					return nil
				},
			},
			{
				Name:        "update",
				Description: "Update installed tools with available updates across supported package managers.",
				Usage:       "Update tools.",
				Flags: []urfave.Flag{
					&urfave.StringSliceFlag{
						Name:  "pm",
						Usage: "Pass multiple package managers (none if include all package managers)",
						Action: func(ctx context.Context, cmd *urfave.Command, pms []string) error {
							return packagemanager.Supported(pms)
						},
					},
				},
				MutuallyExclusiveFlags: []urfave.MutuallyExclusiveFlags{
					{
						Required: true,
						Flags: [][]urfave.Flag{
							{
								&urfave.StringSliceFlag{
									Name:  "exclude",
									Usage: "Pass multiple tools to exclude",
									Action: func(ctx context.Context, cmd *urfave.Command, tools []string) error {
										fmt.Println(tools)
										return nil
									},
								},
							},
							{
								&urfave.StringSliceFlag{
									Name:  "include",
									Usage: "Include only tools",
									Action: func(ctx context.Context, cmd *urfave.Command, tools []string) error {
										fmt.Println(tools)
										return nil
									},
								},
							},
						},
					},
				},
				Action: func(ctx context.Context, c *urfave.Command) error {
					snapshot, err := config.Current()
					if err != nil {
						return err
					}

					fmt.Println(snapshot)

					return nil
				},
			},
			{
				Name:        "vulnerabilities",
				Description: "Check installed tools against vulnerability databases.",
				Usage:       "Show known vulnerabilities.",
				Flags: []urfave.Flag{
					&urfave.StringSliceFlag{
						Name:  "pm",
						Usage: "Pass multiple package managers",
						Action: func(ctx context.Context, cmd *urfave.Command, pms []string) error {
							return packagemanager.Supported(pms)
						},
					},
				},
				Action: func(ctx context.Context, c *urfave.Command) error {
					snapshot, err := config.Current()
					if err != nil {
						return err
					}

					pms := c.StringSlice("pm")

					if len(pms) == 0 {
						pms = packagemanager.RegisteredPackageManagers
					}

					outdatedList, err := packagemanager.ReadOutdated(snapshot, pms)
					if err != nil {
						return err
					}

					aggregatedList := make(cve.EUVDList)

					if err := aggregatedList.Aggregate(outdatedList, c.Writer); err != nil {
						return err
					}

					fmt.Println("fetching CVEs")

					if err := aggregatedList.ListCVEs(500); err != nil {
						return err
					}

					return nil
				},
			},
		},
	}
}
