package paths

import (
	"os"
	"path/filepath"
)

const AppName = "rses"

func StateDir() string {
	if v := os.Getenv("RSES_STATE_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), AppName)
	}
	return filepath.Join(home, ".local", "state", AppName)
}

func RuntimeDir() string {
	if v := os.Getenv("RSES_RUNTIME_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
		return v
	}
	return StateDir()
}

func SocketPath() string {
	return filepath.Join(RuntimeDir(), AppName+".sock")
}

func DaemonLogPath() string {
	return filepath.Join(StateDir(), "daemon.log")
}

func SessionLogDir() string {
	return filepath.Join(StateDir(), "logs")
}
