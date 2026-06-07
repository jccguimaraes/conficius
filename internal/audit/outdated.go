package audit

import (
	"fmt"
	"io"

	"github.com/fatih/color"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

func PrintOutdated(outdatedList packagemanager.Packages, writer io.Writer) error {
	cp := color.New(color.FgCyan)
	total := 0
	for _, tools := range outdatedList {
		total += len(tools)
	}

	if _, err := cp.Fprintf(writer, "Total number of packages found: "); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%d\n\n", total); err != nil {
		return err
	}

	for _, tools := range outdatedList {
		for _, outdated := range tools {
			current := outdated.GetCurrentVersion()
			latest := outdated.GetLatestVersion()

			if _, err := cp.Fprintf(writer, "Name: "); err != nil {
				return err
			}

			if _, err := fmt.Fprint(writer, outdated.GetName()); err != nil {
				return err
			}

			if _, err := cp.Fprintf(writer, " Current: "); err != nil {
				return err
			}

			if _, err := fmt.Fprint(writer, current); err != nil {
				return err
			}

			if _, err := cp.Fprintf(writer, " Latest: "); err != nil {
				return err
			}

			if _, err := fmt.Fprint(writer, latest); err != nil {
				return err
			}

			if _, err := cp.Fprintf(writer, " PM: "); err != nil {
				return err
			}

			if _, err := fmt.Fprintln(writer, outdated.GetExecutable()); err != nil {
				return err
			}
		}
	}

	return nil
}
