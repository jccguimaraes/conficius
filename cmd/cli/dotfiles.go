package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"

	"github.com/jccguimaraes/conficius/internal/audit"
	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/dotfiles"
	"github.com/jccguimaraes/conficius/internal/layout"
	"github.com/jccguimaraes/conficius/internal/shell"
	urfave "github.com/urfave/cli/v3"
)

func (cli *Cli) dotfiles() *urfave.Command {
	return &urfave.Command{
		Name:        "dotfiles",
		Description: "Audit and link configuration files for a consistent home directory layout.",
		Usage:       "Manage dotfiles.",
		Action:      func(context.Context, *urfave.Command) error { return nil },
		Commands: []*urfave.Command{
			{
				Name:        "audit",
				Description: "Audit XDG, Zsh, and configured dotfile link state.",
				Usage:       "Audit dotfiles layout.",
				Flags: []urfave.Flag{
					&urfave.BoolFlag{Name: "strict", Usage: "Return a nonzero exit code when warnings or errors exist."},
					&urfave.BoolFlag{Name: "json", Usage: "Write JSON output with schema_version 1."},
				},
				MutuallyExclusiveFlags: []urfave.MutuallyExclusiveFlags{
					{
						Required: true,
						Flags: [][]urfave.Flag{
							{&urfave.BoolFlag{Name: "os-native", Usage: "Use OS-native config directories.", Sources: urfave.EnvVars("CONFICIUS_OS_NATIVE"), OnlyOnce: true}},
							{&urfave.BoolFlag{Name: "xdg", Usage: "Use XDG config directories.", Sources: urfave.EnvVars("CONFICIUS_XDG"), OnlyOnce: true}},
						},
					},
				},
				Action: func(ctx context.Context, command *urfave.Command) error {
					mode, err := dotfilesMode(command)
					if err != nil {
						return err
					}
					snapshot, project, entries, err := loadDotfiles(command)
					if err != nil {
						return err
					}

					expected := layout.Resolve(snapshot, mode)
					report := audit.Environment(snapshot, expected)
					if snapshot.Shell.Name == shell.SHELL_ZSH {
						report.Findings = append(report.Findings, audit.Zsh(shell.ReadZsh(), expected)...)
					}
					states, err := dotfiles.Inspect(entries, dotfiles.Options{
						ConfigDir: project.Root,
						HomeDir:   snapshot.UserEnv.HomeDir,
						Platform:  runtime.GOOS,
					})
					if err != nil {
						return err
					}
					report.Findings = append(report.Findings, audit.LinkFindings(states)...)

					if command.Bool("json") {
						err = audit.PrintJSON(report, cli.Writer)
					} else {
						err = audit.PrintReport(report, cli.Writer)
					}
					if err != nil {
						return err
					}
					if command.Bool("strict") && audit.HasProblems(report) {
						return errors.New("dotfile audit found warnings or errors")
					}
					return nil
				},
			},
			{
				Name:        "link",
				Description: "Preview or create configured dotfile links.",
				Usage:       "Link dotfiles.",
				Arguments: []urfave.Argument{
					&urfave.StringArgs{Name: "tools", Max: -1},
				},
				Flags: []urfave.Flag{
					&urfave.StringSliceFlag{Name: "exclude", Usage: "Exclude a configured tool; may be repeated."},
					&urfave.BoolFlag{Name: "apply", Usage: "Apply planned filesystem changes."},
					&urfave.BoolFlag{Name: "force", Usage: "Replace conflicting symlinks that resolve inside this repository."},
					&urfave.BoolFlag{Name: "json", Usage: "Write JSON output with schema_version 1."},
				},
				Action: func(ctx context.Context, command *urfave.Command) error {
					names := command.StringArgs("tools")
					snapshot, project, entries, err := loadDotfiles(command)
					if err != nil {
						return err
					}
					results, err := dotfiles.Link(entries, dotfiles.Options{
						ConfigDir: project.Root,
						HomeDir:   snapshot.UserEnv.HomeDir,
						Platform:  runtime.GOOS,
						Names:     names,
						Exclude:   command.StringSlice("exclude"),
						Apply:     command.Bool("apply"),
						Force:     command.Bool("force"),
					})
					if err != nil {
						return err
					}
					return printLinkResults(cli.Writer, command.Bool("json"), results)
				},
			},
		},
	}
}

func loadDotfiles(command *urfave.Command) (*config.Snapshot, *config.Project, []dotfiles.Entry, error) {
	snapshot, err := config.Current()
	if err != nil {
		return nil, nil, nil, err
	}
	project, err := config.LoadDiscovered(command.String("config"))
	if err != nil {
		return nil, nil, nil, err
	}
	entries := make([]dotfiles.Entry, 0)
	for name, tool := range project.Tools {
		for _, link := range tool.Links {
			entries = append(entries, dotfiles.Entry{
				Name:      name,
				Source:    link.Source,
				Target:    link.Target,
				Platforms: link.Platforms,
			})
		}
	}
	return snapshot, project, entries, nil
}

func printLinkResults(writer io.Writer, jsonOutput bool, results []dotfiles.Result) error {
	if jsonOutput {
		return json.NewEncoder(writer).Encode(struct {
			SchemaVersion int               `json:"schema_version"`
			Data          []dotfiles.Result `json:"data"`
		}{SchemaVersion: 1, Data: results})
	}
	for _, result := range results {
		if _, err := fmt.Fprintf(writer, "[%s] %s: %s -> %s\n", result.Action, result.Name, result.Target, result.Source); err != nil {
			return err
		}
	}
	return nil
}

func dotfilesMode(command *urfave.Command) (layout.Mode, error) {
	if command.Bool("xdg") {
		return layout.ModeXDG, nil
	}
	if command.Bool("os-native") {
		return layout.ModeOSNative, nil
	}
	return "", errors.New("one of --xdg or --os-native is required")
}
