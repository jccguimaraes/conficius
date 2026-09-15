package audit

import (
	"fmt"
	"io"

	"github.com/fatih/color"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
)

func PrintOutdated(outdatedList packagemanager.Packages, writer io.Writer) error {
	cp := color.New(color.FgCyan)
	records := packagemanager.Records(outdatedList)

	if _, err := cp.Fprintf(writer, "Total number of packages found: "); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%d\n\n", len(records)); err != nil {
		return err
	}

	for _, outdated := range records {
		if _, err := cp.Fprintf(writer, "Name: "); err != nil {
			return err
		}
		if _, err := fmt.Fprint(writer, outdated.Name); err != nil {
			return err
		}
		if _, err := cp.Fprintf(writer, " Current: "); err != nil {
			return err
		}
		if _, err := fmt.Fprint(writer, outdated.Current); err != nil {
			return err
		}
		if _, err := cp.Fprintf(writer, " Latest: "); err != nil {
			return err
		}
		if _, err := fmt.Fprint(writer, outdated.Latest); err != nil {
			return err
		}
		if _, err := cp.Fprintf(writer, " PM: "); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "%s (%s)\n", outdated.Manager, outdated.Executable); err != nil {
			return err
		}
	}

	return nil
}
