package layout

import (
	"path/filepath"

	"github.com/jccguimaraes/conficius/internal/config"
)

type Expected struct {
	Mode       Mode
	ConfigRoot string
}

func Resolve(snapshot *config.Snapshot, mode Mode) Expected {
	return Expected{
		Mode:       mode,
		ConfigRoot: configRoot(snapshot, mode),
	}
}

func configRoot(snapshot *config.Snapshot, mode Mode) string {
	if mode == ModeXDG {
		return filepath.Join(snapshot.UserEnv.HomeDir, ".config")
	}

	switch snapshot.Platform.GOOS {
	case "darwin":
		return filepath.Join(snapshot.UserEnv.HomeDir, "Library", "Application Support")
	default:
		return filepath.Join(snapshot.UserEnv.HomeDir, ".config")
	}
}
