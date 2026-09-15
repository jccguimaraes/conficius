package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	urfave "github.com/urfave/cli/v3"

	"github.com/jccguimaraes/conficius/internal/audit"
	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/jccguimaraes/conficius/internal/vulnerability"
)

const packageOutputSchemaVersion = 2

type packageOutputEnvelope struct {
	SchemaVersion int `json:"schema_version"`
	Data          any `json:"data"`
}

type outdatedOutput struct {
	Scopes   []config.Scope                 `json:"scopes"`
	Managers []packagemanager.ManagerStatus `json:"managers"`
	Packages []packagemanager.Package       `json:"packages"`
	Drift    []config.PackageState          `json:"drift"`
}

type updateOutput struct {
	Managers []packagemanager.ManagerStatus `json:"managers"`
	Updates  []packagemanager.UpdatePlan    `json:"updates"`
}

type vulnerabilityOutput struct {
	Managers []packagemanager.ManagerStatus `json:"managers"`
	Findings []vulnerability.Finding        `json:"findings"`
}

func (cli *Cli) packages() *urfave.Command {
	pmFlag := func() urfave.Flag {
		return &urfave.StringSliceFlag{
			Name:  "pm",
			Usage: "Limit package managers (mise, brew, npm)",
			Action: func(ctx context.Context, cmd *urfave.Command, pms []string) error {
				return packagemanager.Supported(pms)
			},
		}
	}
	jsonFlag := func() urfave.Flag {
		return &urfave.BoolFlag{Name: "json", Usage: "Write schema-versioned JSON"}
	}
	scopeFlags := func() []urfave.Flag {
		return []urfave.Flag{
			&urfave.BoolFlag{Name: "global", Usage: "Inspect global package installations only."},
			&urfave.StringSliceFlag{Name: "project", Usage: "Inspect project `PATH`; may be repeated."},
			&urfave.StringFlag{Name: "profile", Usage: "Select profile `NAME`.", Sources: urfave.EnvVars(config.ProfileEnvironment)},
			&urfave.StringSliceFlag{Name: "profile-project", Usage: "Inspect named project from selected profile; may be repeated."},
		}
	}

	return &urfave.Command{
		Name:        "packages",
		Description: "Inspect installed package managers and update installed tools.",
		Usage:       "Inspect and update packages.",
		Action: func(ctx context.Context, cmd *urfave.Command) error {
			return nil
		},
		Commands: []*urfave.Command{
			{
				Name:        "pms",
				Description: "List supported package managers and availability.",
				Usage:       "Show supported package managers.",
				Flags:       []urfave.Flag{jsonFlag()},
				Action: func(ctx context.Context, c *urfave.Command) error {
					statuses, err := packagemanager.Inspect(nil)
					if err != nil {
						return err
					}
					if c.Bool("json") {
						return writePackageJSON(cli.Writer, statuses)
					}
					return printManagerStatuses(cli.Writer, statuses)
				},
			},
			{
				Name:        "outdated",
				Description: "List installed tools with available updates.",
				Usage:       "Show outdated tools.",
				Flags:       append([]urfave.Flag{pmFlag(), jsonFlag()}, scopeFlags()...),
				Action: func(ctx context.Context, c *urfave.Command) error {
					scopes, resolved, err := resolvePackageScopes(c)
					if err != nil {
						return err
					}
					result, queryErr := packagemanager.QueryOutdatedScopes(ctx, c.StringSlice("pm"), managerScopes(scopes))
					records := packagemanager.Records(result.Packages)
					installed := make([]config.InstalledVersion, len(records))
					for i, record := range records {
						installed[i] = config.InstalledVersion{Manager: record.Manager, Name: record.Name, Scope: record.Scope, Path: record.Path, Version: record.Current}
					}
					selected, drift := config.EvaluatePackages(resolved.Profile, installed)
					selectedKeys := make(map[string]struct{}, len(selected))
					for _, pkg := range selected {
						selectedKeys[pkg.Scope+"\x00"+pkg.Path+"\x00"+pkg.Manager+"\x00"+pkg.Name] = struct{}{}
					}
					result.Packages = packagemanager.Filter(result.Packages, func(pkg packagemanager.Package) bool {
						_, exists := selectedKeys[pkg.Scope+"\x00"+pkg.Path+"\x00"+pkg.Manager+"\x00"+pkg.Name]
						return exists
					})
					if c.Bool("json") {
						if err := writePackageJSON(cli.Writer, outdatedOutput{Scopes: scopes, Managers: result.Managers, Packages: packagemanager.Records(result.Packages), Drift: drift}); err != nil {
							return err
						}
					} else {
						if err := printManagerWarnings(cli.Writer, result.Managers); err != nil {
							return err
						}
						if err := audit.PrintOutdated(result.Packages, cli.Writer); err != nil {
							return err
						}
						if err := printPackageDrift(cli.Writer, drift); err != nil {
							return err
						}
					}
					return queryErr
				},
			},
			{
				Name:        "vulnerabilities",
				Description: "Check installed packages against OSV using exact package versions.",
				Usage:       "Show known vulnerabilities and unsupported package mappings.",
				Flags:       []urfave.Flag{pmFlag(), jsonFlag()},
				Action: func(ctx context.Context, c *urfave.Command) error {
					project, err := config.LoadDiscovered(c.String("config"))
					if err != nil && !errors.Is(err, config.ErrNotFound) {
						return err
					}
					var mappings []config.VulnerabilityMapping
					if project != nil {
						mappings = project.Vulnerabilities.Mappings
					}
					inventory, inventoryErr := packagemanager.QueryInstalled(ctx, c.StringSlice("pm"))
					client := vulnerability.NewClient(http.DefaultClient, os.Getenv("CONFICIUS_OSV_URL"))
					findings, scanErr := vulnerability.Scan(ctx, inventory.Packages, mappings, client)
					if c.Bool("json") {
						writeErr := writePackageJSON(cli.Writer, vulnerabilityOutput{Managers: inventory.Managers, Findings: findings})
						return errors.Join(inventoryErr, scanErr, writeErr)
					}
					if err := printManagerWarnings(cli.Writer, inventory.Managers); err != nil {
						return err
					}
					if err := printVulnerabilityFindings(cli.Writer, findings); err != nil {
						return err
					}
					return errors.Join(inventoryErr, scanErr)
				},
			},
			{
				Name:        "update",
				Description: "Preview or apply updates for installed tools.",
				Usage:       "Preview updates by default; use --apply to mutate.",
				Flags: []urfave.Flag{
					pmFlag(),
					jsonFlag(),
					&urfave.BoolFlag{Name: "apply", Usage: "Run update commands interactively"},
					&urfave.StringSliceFlag{Name: "include", Usage: "Include package (manager:name or globally unique name)"},
					&urfave.StringSliceFlag{Name: "exclude", Usage: "Exclude package (manager:name or globally unique name)"},
				},
				Action: func(ctx context.Context, c *urfave.Command) error {
					if c.Bool("apply") && c.Bool("json") {
						return errors.New("--json cannot be used with --apply")
					}

					result, queryErr := packagemanager.QueryOutdated(ctx, c.StringSlice("pm"))
					plans, err := packagemanager.PlanUpdates(result, c.StringSlice("include"), c.StringSlice("exclude"))
					if err != nil {
						return err
					}
					if c.Bool("json") {
						return errors.Join(queryErr, writePackageJSON(cli.Writer, updateOutput{Managers: result.Managers, Updates: plans}))
					}
					if err := printManagerWarnings(cli.Writer, result.Managers); err != nil {
						return err
					}
					if err := printUpdatePlans(cli.Writer, plans, !c.Bool("apply")); err != nil {
						return err
					}
					if !c.Bool("apply") {
						return queryErr
					}

					freshResult, freshQueryErr := packagemanager.QueryOutdated(ctx, c.StringSlice("pm"))
					freshPlans, freshPlanErr := packagemanager.PlanUpdates(freshResult, c.StringSlice("include"), c.StringSlice("exclude"))
					if err := errors.Join(freshQueryErr, freshPlanErr); err != nil {
						return err
					}
					plans = freshPlans

					stdin := c.Reader
					if stdin == nil {
						stdin = os.Stdin
					}
					applyErr := packagemanager.ApplyUpdates(ctx, plans, stdin, cli.Writer, cli.Writer)
					return errors.Join(queryErr, applyErr)
				},
			},
		},
	}
}

func printPackageDrift(writer io.Writer, states []config.PackageState) error {
	for _, state := range states {
		if _, err := fmt.Fprintf(writer, "Drift: %s:%s scope=%s installed=%s desired=%s status=%s\n", state.Manager, state.Name, state.Scope, state.Installed, state.DesiredVersion, state.Status); err != nil {
			return err
		}
	}
	return nil
}

func resolvePackageScopes(command *urfave.Command) ([]config.Scope, config.ResolvedProfile, error) {
	project, err := config.LoadDiscovered(command.String("config"))
	if err != nil && !errors.Is(err, config.ErrNotFound) {
		return nil, config.ResolvedProfile{}, err
	}
	if project == nil {
		project = &config.Project{Version: config.CurrentVersion}
	}
	resolved, err := project.ResolveProfile(command.String("profile"), false)
	if err != nil {
		return nil, config.ResolvedProfile{}, err
	}
	scopes, err := config.ResolveScopes(project, resolved, command.Bool("global"), command.StringSlice("project"), command.StringSlice("profile-project"))
	return scopes, resolved, err
}

func managerScopes(scopes []config.Scope) []packagemanager.Scope {
	result := make([]packagemanager.Scope, len(scopes))
	for i, scope := range scopes {
		result[i] = packagemanager.Scope{Kind: scope.Kind, Path: scope.Path}
	}
	return result
}

func writePackageJSON(writer io.Writer, data any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(packageOutputEnvelope{SchemaVersion: packageOutputSchemaVersion, Data: data})
}

func printManagerStatuses(writer io.Writer, statuses []packagemanager.ManagerStatus) error {
	for _, status := range statuses {
		if status.Status == packagemanager.StatusAvailable {
			if _, err := fmt.Fprintf(writer, "%s: available (%s)\n", status.Name, strings.Join(status.Executables, ", ")); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(writer, "warning: %s %s: %s\n", status.Name, status.Status, status.Error); err != nil {
			return err
		}
	}
	return nil
}

func printManagerWarnings(writer io.Writer, statuses []packagemanager.ManagerStatus) error {
	for _, status := range statuses {
		if status.Status == packagemanager.StatusAvailable {
			continue
		}
		if _, err := fmt.Fprintf(writer, "warning: %s %s: %s\n", status.Name, status.Status, status.Error); err != nil {
			return err
		}
	}
	return nil
}

func printVulnerabilityFindings(writer io.Writer, findings []vulnerability.Finding) error {
	for _, finding := range findings {
		if _, err := fmt.Fprintf(writer, "%s:%s@%s: %s", finding.Manager, finding.Package, finding.Version, finding.Status); err != nil {
			return err
		}
		if finding.Reason != "" {
			if _, err := fmt.Fprintf(writer, " (%s)", finding.Reason); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
		for _, advisory := range finding.Vulnerabilities {
			if _, err := fmt.Fprintf(writer, "  %s: %s\n", advisory.ID, advisory.Summary); err != nil {
				return err
			}
		}
	}
	return nil
}

func printUpdatePlans(writer io.Writer, plans []packagemanager.UpdatePlan, preview bool) error {
	if preview {
		if _, err := fmt.Fprintln(writer, "Preview; rerun with --apply to update:"); err != nil {
			return err
		}
	}
	if len(plans) == 0 {
		_, err := fmt.Fprintln(writer, "No updates selected.")
		return err
	}
	for _, plan := range plans {
		parts := append([]string{plan.Executable}, plan.Args...)
		for i, part := range parts {
			parts[i] = strconv.Quote(part)
		}
		if _, err := fmt.Fprintln(writer, strings.Join(parts, " ")); err != nil {
			return err
		}
	}
	return nil
}
