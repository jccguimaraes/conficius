package main

import (
	"context"
	"io"

	"github.com/fatih/color"
	urfave "github.com/urfave/cli/v3"
	// "github.com/jccguimaraes/conficius/internal/config"
)

const (
	appName   string = "conficius"
	copyright string = "(c) 2026 João Guimarães"
)

type Cli struct {
	Writer  io.Writer
	Command *urfave.Command
	// UserConfig config.UserConfig
}

func (cli *Cli) Execute(osArgs []string) error {
	if err := cli.Command.Run(context.Background(), osArgs); err != nil {
		if _, werr := color.New(color.FgRed).Fprint(cli.Writer, "\r\n[error] "); werr != nil {
			return werr
		}

		if _, werr := cli.Writer.Write([]byte(err.Error() + "\r\n")); werr != nil {
			return werr
		}

		return err
	}

	return nil
}

func New(writer io.Writer, version string) *Cli {
	cli := &Cli{
		Writer: writer,
		Command: &urfave.Command{
			Writer:                 writer,
			Version:                version,
			Name:                   appName,
			Copyright:              copyright,
			Description:            "Manage dotfiles and inspect installed package tools.",
			Usage:                  "Manage dotfiles and packages.",
			UseShortOptionHandling: true,
			EnableShellCompletion:  true,
		},
	}

	cli.Command.Commands = []*urfave.Command{
		cli.dotfiles(),
		cli.packages(),
	}

	return cli
}
