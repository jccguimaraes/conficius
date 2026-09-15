package main

import (
	"context"
	"fmt"
	"io"

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
		cli.dotfiles(),
		cli.packages(),
	}

	return cli
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
