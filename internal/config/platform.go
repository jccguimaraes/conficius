package config

import "runtime"

type Platform struct {
	GOOS string
	Arch string
}

func ReadPlatform() Platform {
	return Platform{
		GOOS: runtime.GOOS,
		Arch: runtime.GOARCH,
	}
}
