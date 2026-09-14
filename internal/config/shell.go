package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jccguimaraes/conficius/internal/shell"
)

type Shell struct {
	Name   string
	Env    string
	Source string
}

const (
	ShellSourceShell   = "SHELL"
	ShellSourceComSpec = "ComSpec"
	ShellSourceUnknown = "unknown"
)

func ReadShell() (Shell, error) {
	env := os.Getenv("SHELL")
	source := ShellSourceShell

	if env == "" && runtime.GOOS == "windows" {
		env = os.Getenv("ComSpec")
		source = ShellSourceComSpec
	}

	if env == "" {
		return Shell{}, errors.New("no shell could be determined, SHELL env is empty")
	}

	s := Shell{
		Name:   normalizeShellName(env),
		Env:    env,
		Source: source,
	}

	return s, nil
}

func normalizeShellName(path string) string {
	path = strings.TrimSpace(path)
	path = strings.ReplaceAll(path, "\\", "/")

	name := filepath.Base(path)
	name = strings.ToLower(name)
	name = strings.TrimSuffix(name, ".exe")

	switch name {
	case shell.SHELL_ZSH:
		return shell.SHELL_ZSH
	// case "bash":
	// 	return "bash"
	// case "sh":
	// 	return "sh"
	// case "fish":
	// 	return "fish"
	// case "pwsh", "powershell":
	// 	return "pwsh"
	// case "cmd":
	// 	return "cmd"
	default:
		return name
	}
}
