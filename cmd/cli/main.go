package main

import (
	"fmt"
	"os"
	"path/filepath"

	cfgPath "github.com/jccguimaraes/conficius/internal/path"
)

type user struct {
	xdgConfigHome string
	userConfigDir string
	userHomeDir   string
}

func (u *user) SetXDGConfigHome(p string) {
	u.xdgConfigHome = p
}

func (u *user) SetUserConfigDir(p string) {
	u.userConfigDir = p
}

func (u *user) SetUserHomeDir(p string) {
	u.userHomeDir = p
}

const GOOSWINDOWS = "windows"
const GOOSDARWIN = "darwin"
const GOOSFREEBSD = "freebsd"
const GOOSLINUX = "linux"

func main() {
	u := user{}

	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		u.SetXDGConfigHome(xdg)
		fmt.Println("XDG_CONFIG_HOME", xdg)
	}

	cfg, err := os.UserConfigDir()
	if err != nil {
	}

	u.SetUserConfigDir(cfg)

	home, err := os.UserHomeDir()
	if err != nil {
	}

	u.SetUserHomeDir(home)

	// appName := "zsh"

	// switch runtime.GOOS {
	// case GOOSWINDOWS:
	// 	fmt.Println(filepath.Join(home, "."+appName))
	// default:
	// 	fmt.Println(filepath.Join(home, ".config", appName))
	// }

	walkDir := cfgPath.New(filepath.Join(home, ".config"))

	if err := filepath.Walk(walkDir.GetPath(), walkDir.Walk); err != nil {
		fmt.Println(err)
	}

	fmt.Println("xdgConfigHome", u.xdgConfigHome)
	fmt.Println("userConfigDir", u.userConfigDir)
	fmt.Println("userHomeDir", u.userHomeDir)
}
