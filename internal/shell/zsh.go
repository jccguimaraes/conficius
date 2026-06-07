package shell

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const SHELL_ZSH = "zsh"

type Zsh struct {
	ZDOTDIR  string
	Zshenv   bool
	Zprofile bool
	Zshrc    bool
	Zlogin   bool
	Zlogout  bool
}

func ReadZsh() Zsh {
	return Zsh{
		ZDOTDIR: os.Getenv("ZDOTDIR"),
	}
}

func (s Zsh) RunCommand(ex string, args []string) ([]byte, error) {
	cmd := exec.Command(ex, "-lc", strings.Join(args, " "))

	path, err := exec.LookPath(ex)
	if err != nil {
		return nil, err
	}

	fmt.Println("path", path)
	fmt.Println("args", cmd.Args)

	return cmd.Output()
}
