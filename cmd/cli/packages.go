package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	urfave "github.com/urfave/cli/v3"

	"github.com/jccguimaraes/conficius/internal/audit"
	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/euvd"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/jccguimaraes/conficius/internal/security"
	"github.com/jccguimaraes/conficius/internal/vulnerability"
)

const packageOutputSchemaVersion = 2

type packageSnapshot struct {
	Timestamp  string `json:"timestamp"`
	Platform   string `json:"platform"`
	Arch       string `json:"arch"`
	CLIVersion string `json:"cli_version"`
}

type packageOutputEnvelope struct {
	SchemaVersion int             `json:"schema_version"`
	Snapshot      packageSnapshot `json:"snapshot"`
	Data          any             `json:"data"`
}

type outdatedOutput struct {
	Scopes     []config.Scope                      `json:"scopes"`
	Managers   []packagemanager.ManagerStatus      `json:"managers"`
	Packages   []packagemanager.Package            `json:"packages"`
	Installed  []packagemanager.InstalledPackage   `json:"installed,omitempty"`
	Freshness  string                              `json:"freshness"`
	Collisions []packagemanager.CollisionCandidate `json:"collisions,omitempty"`
}

type updateOutput struct {
	Managers []packagemanager.ManagerStatus `json:"managers"`
	Updates  []packagemanager.UpdatePlan    `json:"updates"`
}

type doctorOutput struct {
	Managers []packagemanager.ManagerIntegrity `json:"managers"`
	Projects []packagemanager.ProjectIntegrity `json:"projects,omitempty"`
}

type vulnerabilityOutput struct {
	Scopes   []config.Scope                 `json:"scopes"`
	Managers []packagemanager.ManagerStatus `json:"managers"`
	Report   security.Report                `json:"report"`
}

func (cli *Cli) packages() *urfave.Command {
	pmFlag := func() urfave.Flag {
		return &urfave.StringSliceFlag{
			Name:  "pm",
			Usage: "Limit package managers (mise, brew, npm, mas, uv, pipx, poetry, pnpm, cargo, yarn, bun, go)",
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
				Name:        "go-tools",
				Description: "Record Go tool ownership without changing installed binaries.",
				Usage:       "Manage Go tool import records.",
				Commands: []*urfave.Command{
					{
						Name:      "import",
						Usage:     "Record MODULE VERSION COMMAND [COMMAND...]",
						ArgsUsage: "MODULE VERSION COMMAND [COMMAND...]",
						Action: func(_ context.Context, c *urfave.Command) error {
							if c.NArg() < 3 {
								return errors.New("import requires MODULE VERSION COMMAND [COMMAND...]")
							}
							path, err := goToolJournalPath()
							if err != nil {
								return err
							}
							journal, err := packagemanager.ReadGoToolJournal(path)
							if errors.Is(err, os.ErrNotExist) {
								journal = packagemanager.NewGoToolJournal(nil)
							} else if err != nil {
								return err
							}
							journal.Imports = append(journal.Imports, packagemanager.GoToolImport{Module: c.Args().First(), Version: c.Args().Get(1), Commands: c.Args().Slice()[2:]})
							if err := packagemanager.WriteGoToolJournal(path, journal); err != nil {
								return err
							}
							_, err = fmt.Fprintf(cli.Writer, "Recorded Go tool import in %s.\n", path)
							return err
						},
					},
				},
			},
			{
				Name:        "doctor",
				Description: "Check package manager environment integrity without freshness queries.",
				Usage:       "Check package manager integrity.",
				Flags:       append([]urfave.Flag{pmFlag(), jsonFlag()}, scopeFlags()...),
				Action: func(ctx context.Context, c *urfave.Command) error {
					scopes, resolved, err := resolvePackageScopes(c)
					if err != nil {
						return err
					}
					checks, err := packagemanager.CheckManagerIntegrity(c.StringSlice("pm"), packagemanager.QueryOptionsFromProfile(resolved.Profile))
					if err != nil {
						return err
					}
					projects := projectIntegrityChecks(scopes)
					if c.Bool("json") {
						return cli.writePackageJSON(doctorOutput{Managers: checks, Projects: projects})
					}
					if err := printManagerIntegrity(cli.Writer, checks); err != nil {
						return err
					}
					return printProjectIntegrity(cli.Writer, projects)
				},
			},
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
						return cli.writePackageJSON(statuses)
					}
					return printManagerStatuses(cli.Writer, statuses)
				},
			},
			{
				Name:        "outdated",
				Description: "List installed tools with available updates.",
				Usage:       "Show outdated tools.",
				Flags: append([]urfave.Flag{pmFlag(), jsonFlag(),
					&urfave.BoolFlag{Name: "offline", Usage: "Inspect installed state without network freshness checks"},
					&urfave.BoolFlag{Name: "check", Usage: "Return exit code 1 when updates exist"},
				}, scopeFlags()...),
				Action: func(ctx context.Context, c *urfave.Command) error {
					scopes, resolved, err := resolvePackageScopes(c)
					if err != nil {
						return err
					}
					options := packagemanager.QueryOptionsFromProfile(resolved.Profile)
					if c.Bool("offline") {
						inventory, queryErr := packagemanager.QueryInstalledScopesWithOptions(ctx, c.StringSlice("pm"), managerScopes(scopes), options)
						collisions := packagemanager.ResolveCollisionCandidates(packagemanager.CollisionCandidates(inventory.Commands), exec.LookPath)
						if c.Bool("json") {
							return errors.Join(queryErr, cli.writePackageJSON(outdatedOutput{Scopes: scopes, Managers: inventory.Managers, Installed: inventory.Packages, Freshness: "unknown", Collisions: collisions}))
						}
						if err := printManagerWarnings(cli.Writer, inventory.Managers); err != nil {
							return err
						}
						if err := printCollisionWarnings(cli.Writer, collisions); err != nil {
							return err
						}
						if _, err := fmt.Fprintf(cli.Writer, "Offline inventory: %d packages; freshness unknown.\n", len(inventory.Packages)); err != nil {
							return err
						}
						if queryErr != nil && c.Bool("check") {
							return urfave.Exit(queryErr, 2)
						}
						return queryErr
					}
					result, queryErr := packagemanager.QueryOutdatedScopesWithOptions(ctx, c.StringSlice("pm"), managerScopes(scopes), options)
					if c.Bool("json") {
						if err := cli.writePackageJSON(outdatedOutput{Scopes: scopes, Managers: result.Managers, Packages: packagemanager.Records(result.Packages), Freshness: "fresh"}); err != nil {
							return err
						}
					} else {
						if err := printManagerWarnings(cli.Writer, result.Managers); err != nil {
							return err
						}
						if err := audit.PrintOutdated(result.Packages, cli.Writer); err != nil {
							return err
						}
					}
					if c.Bool("check") {
						if err := packageCheckError(queryErr, len(packagemanager.Records(result.Packages))); err != nil {
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
				Flags:       append([]urfave.Flag{pmFlag(), jsonFlag(), &urfave.BoolFlag{Name: "transitive", Usage: "Include transitive package inventory"}}, scopeFlags()...),
				Action: func(ctx context.Context, c *urfave.Command) error {
					scopes, resolved, err := resolvePackageScopes(c)
					if err != nil {
						return err
					}
					project, loadErr := config.LoadPackages(c.String("config"))
					if loadErr != nil {
						return loadErr
					}
					osvMappings := project.Vulnerabilities.Mappings
					euvdMappings := project.Vulnerabilities.EUVD
					queryOptions := packagemanager.QueryOptionsFromProfile(resolved.Profile)
					queryOptions.IncludeTransitive = c.Bool("transitive")
					inventory, inventoryErr := packagemanager.QueryInstalledScopesWithOptions(ctx, c.StringSlice("pm"), managerScopes(scopes), queryOptions)
					var managerPackages []packagemanager.InstalledPackage
					if includesGlobalScope(scopes) {
						var managerErr error
						managerPackages, managerErr = packagemanager.ManagerPackages(ctx, inventory.Managers)
						inventoryErr = errors.Join(inventoryErr, managerErr)
					}
					inventory.Packages = append(inventory.Packages, managerPackages...)
					outdated, outdatedErr := packagemanager.QueryOutdatedScopesWithOptions(ctx, c.StringSlice("pm"), managerScopes(scopes), packagemanager.QueryOptionsFromProfile(resolved.Profile))
					managerUpdates, managerUpdateErr := packagemanager.ManagerUpdates(ctx, inventory.Managers, managerPackages)
					packagemanager.MergePackages(outdated.Packages, managerUpdates)
					outdatedErr = errors.Join(outdatedErr, managerUpdateErr)
					report, scanErr := security.Scan(ctx, inventory.Packages, security.Options{
						Outdated: packagemanager.Records(outdated.Packages), OSVMappings: osvMappings, EUVDMappings: euvdMappings,
						OSVClient:  vulnerability.NewClient(http.DefaultClient, os.Getenv("CONFICIUS_OSV_URL")),
						EUVDClient: euvd.NewClient(http.DefaultClient, os.Getenv("CONFICIUS_EUVD_URL")),
					})
					if c.Bool("json") {
						writeErr := cli.writePackageJSON(vulnerabilityOutput{Scopes: scopes, Managers: inventory.Managers, Report: report})
						return errors.Join(inventoryErr, outdatedErr, scanErr, writeErr)
					}
					if err := printManagerWarnings(cli.Writer, inventory.Managers); err != nil {
						return err
					}
					if err := printVulnerabilityFindings(cli.Writer, report.Evidence); err != nil {
						return err
					}
					return errors.Join(inventoryErr, outdatedErr, scanErr)
				},
			},
			{
				Name:        "update",
				Description: "Preview or apply updates for installed tools.",
				Usage:       "Preview updates by default; use --apply to mutate.",
				Flags: append([]urfave.Flag{
					pmFlag(),
					jsonFlag(),
					&urfave.BoolFlag{Name: "apply", Usage: "Run update commands interactively"},
					&urfave.BoolFlag{Name: "yes", Usage: "Run non-interactively with accepted plan hashes"},
					&urfave.BoolFlag{Name: "best-effort", Usage: "Continue with later plans after a failed plan"},
					&urfave.StringSliceFlag{Name: "accept-plan", Usage: "Accept exact plan `HASH`; may be repeated"},
					&urfave.BoolFlag{Name: "allow-dirty", Usage: "Allow manifest changes in a dirty Git repository."},
					&urfave.BoolFlag{Name: "allow-unversioned", Usage: "Allow manifest changes outside a Git repository."},
					&urfave.BoolFlag{Name: "allow-untested-manager", Usage: "Allow updates using manager versions outside tested ranges."},
					&urfave.StringSliceFlag{Name: "include", Usage: "Include package (manager:name or globally unique name)"},
					&urfave.StringSliceFlag{Name: "exclude", Usage: "Exclude package (manager:name or globally unique name)"},
				}, scopeFlags()...),
				Action: func(ctx context.Context, c *urfave.Command) error {
					if c.Bool("apply") && c.Bool("json") {
						return errors.New("--json cannot be used with --apply")
					}
					if c.Bool("yes") && !c.Bool("apply") {
						return errors.New("--yes requires --apply")
					}
					if c.Bool("best-effort") && !c.Bool("apply") {
						return errors.New("--best-effort requires --apply")
					}
					if len(c.StringSlice("accept-plan")) > 0 && !c.Bool("yes") {
						return errors.New("--accept-plan requires --yes")
					}

					if c.Bool("apply") && !c.IsSet("global") && !c.IsSet("project") && !c.IsSet("profile") && !c.IsSet("profile-project") {
						return errors.New("--apply requires --global, --project PATH, or --profile NAME")
					}
					scopes, resolved, err := resolvePackageScopes(c)
					if err != nil {
						return err
					}
					result, queryErr := packagemanager.QueryOutdatedScopesWithOptions(ctx, c.StringSlice("pm"), managerScopes(scopes), packagemanager.QueryOptionsFromProfile(resolved.Profile))
					plans, err := packagemanager.PlanUpdates(result, c.StringSlice("include"), c.StringSlice("exclude"))
					if err != nil {
						return err
					}
					if c.Bool("json") {
						return errors.Join(queryErr, cli.writePackageJSON(updateOutput{Managers: result.Managers, Updates: plans}))
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

					if err := validateManagerVersions(result.Managers, c.Bool("allow-untested-manager")); err != nil {
						return err
					}
					if err := packagemanager.ValidateProjectMutations(plans, resolved.Profile, c.Bool("allow-dirty"), c.Bool("allow-unversioned")); err != nil {
						return err
					}
					freshResult, freshQueryErr := packagemanager.QueryOutdatedScopesWithOptions(ctx, c.StringSlice("pm"), managerScopes(scopes), packagemanager.QueryOptionsFromProfile(resolved.Profile))
					freshPlans, freshPlanErr := packagemanager.PlanUpdates(freshResult, c.StringSlice("include"), c.StringSlice("exclude"))
					if err := errors.Join(freshQueryErr, freshPlanErr); err != nil {
						return err
					}
					plans = freshPlans
					if c.Bool("yes") {
						if err := validateAcceptedPlans(plans, c.StringSlice("accept-plan")); err != nil {
							return err
						}
					}

					log := newUpdateLog(plans)
					logPath, err := writeUpdateLog(log)
					if err != nil {
						return err
					}
					stdin := c.Reader
					if stdin == nil {
						stdin = os.Stdin
					}
					outcomes, applyErr := packagemanager.ApplyUpdatesWithOutcomes(ctx, plans, c.Bool("best-effort"), stdin, cli.Writer, cli.Writer)
					var verifyErr error
					outcomes, verifyErr = packagemanager.VerifyUpdateOutcomes(ctx, plans, outcomes, packagemanager.QueryOptionsFromProfile(resolved.Profile))
					log.Outcomes = outcomes
					log.Status = "completed"
					if applyErr != nil || verifyErr != nil {
						log.Status = "failed"
						if ctx.Err() != nil {
							log.Status = "interrupted"
						}
						log.Error = errors.Join(applyErr, verifyErr).Error()
					}
					logErr := finishUpdateLog(logPath, log)
					return errors.Join(queryErr, applyErr, verifyErr, logErr)
				},
			},
		},
	}
}

func packageCheckError(queryErr error, updates int) error {
	if queryErr != nil {
		return urfave.Exit(queryErr, 2)
	}
	if updates > 0 {
		return urfave.Exit("package updates found", 1)
	}
	return nil
}

func validateManagerVersions(statuses []packagemanager.ManagerStatus, allowUntested bool) error {
	if allowUntested {
		return nil
	}
	for _, status := range statuses {
		if status.Status == packagemanager.StatusAvailable && status.ManagerVersion != "" && !status.VersionTested {
			return fmt.Errorf("%s version %s is outside tested range; use --allow-untested-manager", status.Name, status.ManagerVersion)
		}
	}
	return nil
}

func validateAcceptedPlans(plans []packagemanager.UpdatePlan, accepted []string) error {
	acceptedSet := make(map[string]struct{}, len(accepted))
	for _, hash := range accepted {
		acceptedSet[hash] = struct{}{}
	}
	for _, plan := range plans {
		if _, ok := acceptedSet[plan.StateHash]; !ok {
			return fmt.Errorf("plan %s is not accepted; rerun with --accept-plan %s", plan.StateHash, plan.StateHash)
		}
		delete(acceptedSet, plan.StateHash)
	}
	if len(acceptedSet) > 0 {
		return errors.New("accepted plan hash is stale or not selected")
	}
	return nil
}

func printCollisionWarnings(writer io.Writer, candidates []packagemanager.CollisionCandidate) error {
	for _, candidate := range candidates {
		owners := make([]string, len(candidate.Owners))
		for i, owner := range candidate.Owners {
			owners[i] = owner.CanonicalID()
		}
		location := ""
		if candidate.Path != "" {
			location = " at " + candidate.Path
		}
		if _, err := fmt.Fprintf(writer, "warning: command %s%s has competing global owners: %s\n", candidate.Command, location, strings.Join(owners, ", ")); err != nil {
			return err
		}
	}
	return nil
}

func resolvePackageScopes(command *urfave.Command) ([]config.Scope, config.ResolvedProfile, error) {
	project, err := config.LoadPackages(command.String("config"))
	if err != nil {
		return nil, config.ResolvedProfile{}, err
	}
	resolved, err := project.ResolveProfile(command.String("profile"), false)
	if err != nil {
		return nil, config.ResolvedProfile{}, err
	}
	scopes, err := config.ResolveScopes(project, resolved, command.Bool("global"), command.StringSlice("project"), command.StringSlice("profile-project"))
	return scopes, resolved, err
}

func includesGlobalScope(scopes []config.Scope) bool {
	for _, scope := range scopes {
		if scope.Kind == "global" {
			return true
		}
	}
	return false
}

func managerScopes(scopes []config.Scope) []packagemanager.Scope {
	result := make([]packagemanager.Scope, 0, len(scopes))
	for _, scope := range scopes {
		result = append(result, packagemanager.Scope{Kind: scope.Kind, Path: scope.Path})
		for _, workspace := range scope.Workspaces {
			result = append(result, packagemanager.Scope{Kind: "workspace", Path: workspace})
		}
	}
	return result
}

func (cli *Cli) writePackageJSON(data any) error {
	encoder := json.NewEncoder(cli.Writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(packageOutputEnvelope{
		SchemaVersion: packageOutputSchemaVersion,
		Snapshot: packageSnapshot{
			Timestamp: cli.Now().UTC().Format(time.RFC3339), Platform: runtime.GOOS, Arch: runtime.GOARCH, CLIVersion: cli.Version,
		},
		Data: data,
	})
}

func projectIntegrityChecks(scopes []config.Scope) []packagemanager.ProjectIntegrity {
	var checks []packagemanager.ProjectIntegrity
	for _, scope := range scopes {
		if (scope.Kind == "project" || scope.Kind == "workspace") && hasPackageManifest(scope.Path) {
			checks = append(checks, packagemanager.CheckNPMProjectIntegrity(scope.Path))
		}
		for _, workspace := range scope.Workspaces {
			if hasPackageManifest(workspace) {
				checks = append(checks, packagemanager.CheckNPMProjectIntegrity(workspace))
			}
		}
	}
	return checks
}

func hasPackageManifest(path string) bool {
	for _, name := range []string{"package.json", "go.mod"} {
		if info, err := os.Stat(filepath.Join(path, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func printProjectIntegrity(writer io.Writer, checks []packagemanager.ProjectIntegrity) error {
	for _, check := range checks {
		if _, err := fmt.Fprintf(writer, "%s project %s: %s: %s\n", check.Manager, check.Path, check.Status, check.Message); err != nil {
			return err
		}
	}
	return nil
}

func printManagerIntegrity(writer io.Writer, checks []packagemanager.ManagerIntegrity) error {
	for _, check := range checks {
		if _, err := fmt.Fprintf(writer, "%s: %s: %s\n", check.Manager, check.Status, check.Message); err != nil {
			return err
		}
	}
	return nil
}

func printManagerStatuses(writer io.Writer, statuses []packagemanager.ManagerStatus) error {
	for _, status := range statuses {
		if status.Status == packagemanager.StatusAvailable {
			if _, err := fmt.Fprintf(writer, "%s: available (%s)\n", status.Name, strings.Join(status.Executables, ", ")); err != nil {
				return err
			}
			continue
		}
		if status.Error == "" {
			if _, err := fmt.Fprintf(writer, "warning: %s %s\n", status.Name, status.Status); err != nil {
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
		if status.Status != packagemanager.StatusAvailable {
			if status.Error == "" {
				if _, err := fmt.Fprintf(writer, "warning: %s %s\n", status.Name, status.Status); err != nil {
					return err
				}
			} else if _, err := fmt.Fprintf(writer, "warning: %s %s: %s\n", status.Name, status.Status, status.Error); err != nil {
				return err
			}
		}
		for _, warning := range status.Warnings {
			if _, err := fmt.Fprintf(writer, "warning: %s: %s\n", status.Name, warning); err != nil {
				return err
			}
		}
	}
	return nil
}

func printVulnerabilityFindings(writer io.Writer, evidence []security.Evidence) error {
	for _, item := range evidence {
		if item.Advisory == "" {
			continue
		}
		rule := ""
		if len(item.Rules) > 0 {
			rule = item.Rules[0]
			if len(item.Rules) > 1 {
				rule += fmt.Sprintf(" (+%d more)", len(item.Rules)-1)
			}
		}
		if rule != "" {
			rule = " " + rule
		}
		if _, err := fmt.Fprintf(writer, "%s %s %s %s %s %.1f %s %s %s %s%s\n", item.Source, item.Manager, item.Package, item.Version, item.Status, item.Score, item.Advisory, item.CVELink, item.UpgradeTo, item.UpgradeNote, rule); err != nil {
			return err
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
		if _, err := fmt.Fprintf(writer, "Confidence: %s Plan: %s\n", plan.Confidence, plan.StateHash); err != nil {
			return err
		}
		if plan.Impact.RequiresAdmin || plan.Impact.MayRestartServices || plan.Impact.MayReboot || plan.Impact.Interactive {
			if _, err := fmt.Fprintf(writer, "Impact: admin=%t restart-services=%t reboot=%t interactive=%t\n", plan.Impact.RequiresAdmin, plan.Impact.MayRestartServices, plan.Impact.MayReboot, plan.Impact.Interactive); err != nil {
				return err
			}
		}
		for _, recovery := range plan.Recovery {
			parts := append([]string{recovery.Executable}, recovery.Args...)
			for i, part := range parts {
				parts[i] = strconv.Quote(part)
			}
			if _, err := fmt.Fprintf(writer, "Recovery: %s\n", strings.Join(parts, " ")); err != nil {
				return err
			}
		}
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
