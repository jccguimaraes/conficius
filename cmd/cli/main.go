package main

import (
	"io"
	"os"
)

var (
	version = "dev"
)

func run(writer io.Writer, v string) error {
	cmd := New(writer, v)

	return cmd.Execute(os.Args)
}

func main() {
	if err := run(os.Stdout, version); err != nil {
		os.Exit(1)
	}
}
