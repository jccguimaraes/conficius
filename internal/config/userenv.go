package config

import "os"

type UserEnv struct {
	HomeDir       string
	ConfigDir     string
	XdgConfigHome string
	XdgDataHome   string
	XdgCacheHome  string
	XdgStateHome  string
}

func ReadUserEnv() (UserEnv, error) {
	userenv := UserEnv{}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return UserEnv{}, err
	}

	userenv.HomeDir = homeDir

	configDir, err := os.UserConfigDir()
	if err != nil {
		return UserEnv{}, err
	}

	userenv.ConfigDir = configDir

	userenv.XdgConfigHome = os.Getenv("XDG_CONFIG_HOME")
	userenv.XdgDataHome = os.Getenv("XDG_DATA_HOME")
	userenv.XdgCacheHome = os.Getenv("XDG_CACHE_HOME")
	userenv.XdgStateHome = os.Getenv("XDG_STATE_HOME")

	return userenv, nil
}
