package audit

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/fatih/color"
	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/layout"
)

type Severity string

const (
	SeverityOK      Severity = "ok"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Finding struct {
	Severity       Severity `json:"severity"`
	Code           string   `json:"code"`
	Message        string   `json:"message"`
	Recommendation string   `json:"recommendation"`
}

type Report struct {
	Snapshot *config.Snapshot `json:"snapshot"`
	Expected layout.Expected  `json:"expected"`
	Findings []Finding        `json:"findings"`
}

func PrintJSON(report Report, writer io.Writer) error {
	return json.NewEncoder(writer).Encode(struct {
		SchemaVersion int    `json:"schema_version"`
		Data          Report `json:"data"`
	}{SchemaVersion: 1, Data: report})
}

func HasProblems(report Report) bool {
	for _, finding := range report.Findings {
		if finding.Severity == SeverityWarning || finding.Severity == SeverityError {
			return true
		}
	}
	return false
}

func PrintReport(report Report, writer io.Writer) error {
	cp := color.New(color.FgBlue)

	if _, err := cp.Fprint(writer, "GOOS: "); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%s\n", report.Snapshot.Platform.GOOS); err != nil {
		return err
	}

	if _, err := cp.Fprint(writer, "Arch: "); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%s\n", report.Snapshot.Platform.Arch); err != nil {
		return err
	}

	if _, err := cp.Fprint(writer, "HomeDir: "); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%s\n", report.Snapshot.UserEnv.HomeDir); err != nil {
		return err
	}

	if _, err := cp.Fprint(writer, "ConfigDir: "); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%s\n", report.Snapshot.UserEnv.ConfigDir); err != nil {
		return err
	}

	if _, err := cp.Fprint(writer, "XDG_CONFIG_HOME: "); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%s\n", report.Snapshot.UserEnv.XdgConfigHome); err != nil {
		return err
	}

	if _, err := cp.Fprint(writer, "XDG_DATA_HOME: "); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%s\n", report.Snapshot.UserEnv.XdgDataHome); err != nil {
		return err
	}

	if _, err := cp.Fprint(writer, "XDG_CACHE_HOME: "); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%s\n", report.Snapshot.UserEnv.XdgCacheHome); err != nil {
		return err
	}

	if _, err := cp.Fprint(writer, "XDG_STATE_HOME: "); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%s\n", report.Snapshot.UserEnv.XdgStateHome); err != nil {
		return err
	}

	if _, err := cp.Fprint(writer, "Shell: "); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%s\n", report.Snapshot.Shell.Env); err != nil {
		return err
	}

	if _, err := cp.Fprint(writer, "ShellName: "); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%s\n", report.Snapshot.Shell.Name); err != nil {
		return err
	}

	if _, err := cp.Fprint(writer, "ShellSource: "); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%s\n", report.Snapshot.Shell.Source); err != nil {
		return err
	}

	if _, err := cp.Fprint(writer, "LayoutMode: "); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%s\n", report.Expected.Mode); err != nil {
		return err
	}

	if _, err := cp.Fprint(writer, "ExpectedConfigRoot: "); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%s\n", report.Expected.ConfigRoot); err != nil {
		return err
	}

	for _, finding := range report.Findings {
		switch finding.Severity {
		case SeverityOK:
			if _, err := color.New(color.FgGreen).Fprintf(writer, "[%s] ", finding.Severity); err != nil {
				return err
			}
		case SeverityWarning:
			if _, err := color.New(color.FgYellow).Fprintf(writer, "[%s] ", finding.Severity); err != nil {
				return err
			}
		case SeverityError:
			if _, err := color.New(color.FgRed).Fprintf(writer, "[%s] ", finding.Severity); err != nil {
				return err
			}
		default:
			if _, err := color.New(color.FgBlue).Fprintf(writer, "[%s] ", finding.Severity); err != nil {
				return err
			}
		}

		if _, err := fmt.Fprintf(writer, "%s\n", finding.Message); err != nil {
			return err
		}

		if _, err := color.New(color.FgCyan).Fprint(writer, "Recommendation: "); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(writer, "%s\n", finding.Recommendation); err != nil {
			return err
		}
	}

	return nil
}
