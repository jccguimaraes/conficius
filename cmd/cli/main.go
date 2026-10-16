package main

import (
	"fmt"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"io"
	"os"
)

var (
	version = "dev"
)

func run(writer, errWriter io.Writer, v string) error {
	if handled, err := packagemanager.HandleManagerWorker(os.Args[1:], writer); handled {
		return err
	}
	cmd := NewWithWriters(writer, errWriter, v)

	return cmd.Execute(os.Args)
}

func main() {
	if err := run(os.Stdout, os.Stderr, version); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
