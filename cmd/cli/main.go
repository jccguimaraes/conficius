package main

import (
	"io"
	"os"
)

var (
	version = "dev"
)

func run(writer, errWriter io.Writer, v string) error {
	cmd := NewWithWriters(writer, errWriter, v)

	return cmd.Execute(os.Args)
}

func main() {
	if err := run(os.Stdout, os.Stderr, version); err != nil {
		os.Exit(1)
	}
}
