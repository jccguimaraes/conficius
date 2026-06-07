package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jccguimaraes/conficius/internal/audit"
	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/layout"
	urfave "github.com/urfave/cli/v3"
)

func (cli *Cli) dotfiles() *urfave.Command {
	return &urfave.Command{
		Name:        "dotfiles",
		Description: "Audit and link configuration files for a consistent home directory layout.",
		Usage:       "Manage dotfiles.",
		MutuallyExclusiveFlags: []urfave.MutuallyExclusiveFlags{
			{
				Required: true,
				Flags: [][]urfave.Flag{
					{
						&urfave.BoolFlag{
							Name:     "os-native",
							Usage:    "Use OS-native config directories.",
							Sources:  urfave.EnvVars("CONFICIUS_OS_NATIVE"),
							OnlyOnce: true,
						},
					},
					{
						&urfave.BoolFlag{
							Name:     "xdg",
							Usage:    "Use XDG config directories.",
							Sources:  urfave.EnvVars("CONFICIUS_XDG"),
							OnlyOnce: true,
						},
					},
				},
			},
		},
		Action: func(ctx context.Context, cmd *urfave.Command) error {
			return nil
		},
		Commands: []*urfave.Command{
			{
				Name:        "audit",
				Description: "Inspect the current shell and config layout and recommend a cleaner directory structure.",
				Usage:       "Audit dotfiles layout.",
				Action: func(ctx context.Context, c *urfave.Command) error {
					mode, err := dotfilesMode(c)
					if err != nil {
						return err
					}

					snapshot, err := config.Current()
					if err != nil {
						return err
					}

					expected := layout.Resolve(snapshot, mode)
					report := audit.Environment(snapshot, expected)

					if err := audit.PrintReport(report, cli.Writer); err != nil {
						return err
					}

					return nil
				},
			},
			{
				Name:        "link",
				Description: "Link configuration files into the current user's home directory.",
				Usage:       "Link dotfiles.",
				Action: func(ctx context.Context, c *urfave.Command) error {
					fmt.Println("os-native", c.Bool("os-native"))
					fmt.Println("xdg", c.Bool("xdg"))
					return nil
				},
			},
		},
	}
}

func dotfilesMode(cmd *urfave.Command) (layout.Mode, error) {
	if cmd.Bool("xdg") {
		return layout.ModeXDG, nil
	}

	if cmd.Bool("os-native") {
		return layout.ModeOSNative, nil
	}

	return "", errors.New("one of --xdg or --os-native is required")
}
