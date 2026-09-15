package shell

import "os"

const SHELL_ZSH = "zsh"

type Zsh struct {
	ZDOTDIR string
}

func ReadZsh() Zsh {
	return Zsh{ZDOTDIR: os.Getenv("ZDOTDIR")}
}
