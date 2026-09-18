package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/jccguimaraes/conficius/internal/config"
	urfave "github.com/urfave/cli/v3"
)

const (
	appName   string = "conficius"
	copyright string = "(c) 2026 João Guimarães"
)

type Cli struct {
	Writer    io.Writer
	ErrWriter io.Writer
	Version   string
	Now       func() time.Time
	Command   *urfave.Command
}

func (cli *Cli) Execute(osArgs []string) error {
	if err := cli.Command.Run(context.Background(), osArgs); err != nil {
		if _, werr := color.New(color.FgRed).Fprint(cli.ErrWriter, "\r\n[error] "); werr != nil {
			return werr
		}

		if _, werr := fmt.Fprintln(cli.ErrWriter, err); werr != nil {
			return werr
		}

		return err
	}

	return nil
}

func New(writer io.Writer, version string) *Cli {
	return NewWithWriters(writer, writer, version)
}

func NewWithWriters(writer, errWriter io.Writer, version string) *Cli {
	cli := &Cli{
		Writer:    writer,
		ErrWriter: errWriter,
		Version:   version,
		Now:       time.Now,
		Command: &urfave.Command{
			Writer:      writer,
			ErrWriter:   errWriter,
			Version:     version,
			Name:        appName,
			Copyright:   copyright,
			Description: "Manage dotfiles and inspect installed package tools.",
			Usage:       "Manage dotfiles and packages.",
			Flags: []urfave.Flag{
				&urfave.StringFlag{
					Name:      "config",
					Usage:     "Use config `FILE`.",
					TakesFile: true,
					OnlyOnce:  true,
				},
			},
			UseShortOptionHandling: true,
			EnableShellCompletion:  true,
		},
	}

	cli.Command.Commands = []*urfave.Command{
		cli.init(),
		cli.config(),
		cli.dotfiles(),
		cli.packages(),
	}

	return cli
}

func (cli *Cli) config() *urfave.Command {
	profileFlag := func() urfave.Flag {
		return &urfave.StringFlag{Name: "profile", Usage: "Select profile `NAME`.", Sources: urfave.EnvVars(config.ProfileEnvironment)}
	}
	return &urfave.Command{
		Name:  "config",
		Usage: "Inspect and migrate configuration.",
		Commands: []*urfave.Command{
			{
				Name:  "resolve",
				Usage: "Show resolved package profile.",
				Flags: []urfave.Flag{profileFlag(), &urfave.BoolFlag{Name: "json"}, &urfave.BoolFlag{Name: "verbose"}},
				Action: func(ctx context.Context, cmd *urfave.Command) error {
					project, err := config.LoadDiscovered(cmd.String("config"))
					if err != nil {
						return err
					}
					resolved, err := project.ResolveProfile(cmd.String("profile"), cmd.Bool("verbose"))
					if err != nil {
						return err
					}
					if cmd.Bool("json") {
						encoder := json.NewEncoder(cli.Writer)
						encoder.SetIndent("", "  ")
						return encoder.Encode(resolved)
					}
					return printResolvedProfile(cli.Writer, resolved, cmd.Bool("verbose"))
				},
			},
			{
				Name:  "migrate",
				Usage: "Preview migration to config version 2.",
				Flags: []urfave.Flag{&urfave.BoolFlag{Name: "apply", Usage: "Replace config atomically after creating a backup."}},
				Action: func(ctx context.Context, cmd *urfave.Command) error {
					path, err := config.Discover(cmd.String("config"))
					if err != nil {
						return err
					}
					output, backup, err := config.Migrate(path, cmd.Bool("apply"), time.Now())
					if err != nil {
						return err
					}
					if !cmd.Bool("apply") {
						_, err = cli.Writer.Write(output)
						return err
					}
					_, err = fmt.Fprintf(cli.Writer, "Migrated %s\nBackup: %s\n", path, backup)
					return err
				},
			},
		},
	}
}

func printResolvedProfile(writer io.Writer, resolved config.ResolvedProfile, verbose bool) error {
	if _, err := fmt.Fprintf(writer, "Profile: %s\nMode: %s\nManagers: %s\n", resolved.Name, resolved.Profile.Mode, strings.Join(resolved.Profile.ManagerOrder, ", ")); err != nil {
		return err
	}
	for _, project := range resolved.Profile.Projects {
		if _, err := fmt.Fprintf(writer, "Project %s: %s\n", project.Name, project.Path); err != nil {
			return err
		}
	}
	for _, warning := range resolved.Warnings {
		if _, err := fmt.Fprintf(writer, "Warning: %s\n", warning); err != nil {
			return err
		}
	}
	if verbose {
		keys := make([]string, 0, len(resolved.Provenance))
		for key := range resolved.Provenance {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if _, err := fmt.Fprintf(writer, "Source %s: %s\n", key, resolved.Provenance[key]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (cli *Cli) init() *urfave.Command {
	return &urfave.Command{
		Name:        "init",
		Description: "Create a commented Conficius project configuration.",
		Usage:       "Create conficius.toml.",
		Action: func(ctx context.Context, cmd *urfave.Command) error {
			path, err := config.Init(cmd.String("config"))
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cli.Writer, "Created %s\n", path)
			return err
		},
	}
}
