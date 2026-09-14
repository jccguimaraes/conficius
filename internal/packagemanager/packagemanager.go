package packagemanager

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/shell"
)

var RegisteredPackageManagers = []string{
	PM_MISE,
	PM_BREW,
	PM_NPM,
}

type Tool interface {
	GetName() string
	GetCurrentVersion() string
	GetLatestVersion() string
	GetRequestedVersion() string
	GetExecutable() string
}

type Packages map[string][]Tool

func Supported(pms []string) error {
	list := make(map[string]struct{}, len(RegisteredPackageManagers))

	for _, v := range RegisteredPackageManagers {
		if v == PM_HOMEBREW {
			list[PM_BREW] = struct{}{}
		} else {
			list[v] = struct{}{}
		}
	}

	var invalid []string

	for _, v := range pms {
		if _, ok := list[v]; !ok {
			invalid = append(invalid, v)
		}
	}

	if len(invalid) > 0 {
		return fmt.Errorf("unsupported packages %s", strings.Join(invalid, ", "))
	}

	return nil
}

func ReadOutdated(snapshot *config.Snapshot, pms []string) (Packages, error) {
	outdatedPackages := make(Packages)

	for _, pm := range pms {
		switch pm {
		case NPMCommandExec:
			npm := NPM{}

			executableList, err := LookPathAll(NPMCommandExec)

			if errors.Is(err, exec.ErrNotFound) {
				fmt.Printf("'%s' NOT FOUND\n", NPMCommandExec)
				return nil, nil
			}

			if err != nil {
				return nil, err
			}

			if len(executableList) > 1 {
				fmt.Printf("FOUND MORE THAN ONE EXECUTABLE FOR '%s'\n", NPMCommandExec)
			}

			for _, executable := range executableList {
				if err := npm.ListOutdated(snapshot, executable, outdatedPackages); err != nil {
					return nil, err
				}
			}

		case MiseCommandExec:
			mise := Mise{}

			executableList, err := LookPathAll(MiseCommandExec)

			if errors.Is(err, exec.ErrNotFound) {
				fmt.Printf("'%s' NOT FOUND\n", MiseCommandExec)
				return nil, nil
			}

			if err != nil {
				return nil, err
			}

			if len(executableList) > 1 {
				fmt.Printf("FOUND MORE THAN ONE EXECUTABLE FOR '%s'\n", MiseCommandExec)
			}

			for _, executable := range executableList {
				if err := mise.ListOutdated(snapshot, executable, outdatedPackages); err != nil {
					return nil, err
				}
			}
		case PM_BREW:
			homebrew := Homebrew{}

			executableList, err := LookPathAll(HomebrewCommandExec)

			if errors.Is(err, exec.ErrNotFound) {
				fmt.Printf("'%s' NOT FOUND\n", HomebrewCommandExec)
				return nil, nil
			}

			if err != nil {
				return nil, err
			}

			if len(executableList) > 1 {
				fmt.Printf("FOUND MORE THAN ONE EXECUTABLE FOR '%s'\n", HomebrewCommandExec)
			}

			for _, executable := range executableList {
				if err := homebrew.ListOutdated(snapshot, executable, outdatedPackages); err != nil {
					return nil, err
				}
			}
		}
	}

	return outdatedPackages, nil
}

func LookPathAll(file string) ([]string, error) {
	if strings.ContainsAny(file, `/\`) {
		if isExecutable(file) {
			return []string{file}, nil
		}

		return nil, exec.ErrNotFound
	}

	var exts []string
	if runtime.GOOS == "windows" {
		exts = pathExts()
	} else {
		exts = []string{""}
	}

	seen := make(map[string]bool)
	var matches []string

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}

		for _, ext := range exts {
			candidate := filepath.Join(dir, file)

			if ext != "" && !strings.EqualFold(filepath.Ext(candidate), ext) {
				candidate += ext
			}

			abs, err := filepath.Abs(candidate)
			if err != nil {
				continue
			}

			if seen[abs] || !isExecutable(abs) {
				continue
			}

			seen[abs] = true
			matches = append(matches, abs)
		}
	}

	if len(matches) == 0 {
		return nil, exec.ErrNotFound
	}

	return matches, nil
}

func pathExts() []string {
	pathext := os.Getenv("PATHEXT")
	if pathext == "" {
		return []string{".com", ".exe", ".bat", ".cmd"}
	}

	var exts []string
	for _, ext := range filepath.SplitList(pathext) {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}

		exts = append(exts, strings.ToLower(ext))
	}

	return exts
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}

	if runtime.GOOS == "windows" {
		return hasWindowsExecutableExt(path)
	}

	return info.Mode().Perm()&0111 != 0
}

func hasWindowsExecutableExt(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))

	for _, allowed := range pathExts() {
		if ext == strings.ToLower(allowed) {
			return true
		}
	}

	return false
}

func RunShellCommand(s config.Shell, args ...string) ([]byte, error) {
	switch name := s.Name; name {
	case shell.SHELL_ZSH:
		zsh := shell.Zsh{}

		content, err := zsh.RunCommand(s.Name, args)
		if err != nil {
			return content, err
		}

		return content, nil
	default:
		return nil, fmt.Errorf("shell '%s' not supported", name)
	}
}
