package initializer

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bnkrr/rses/providers"
)

const configFileName = "rses.yaml"

func DefaultDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	if configDir == "" {
		return "", fmt.Errorf("user config directory is empty")
	}
	return filepath.Join(configDir, "rses"), nil
}

func Initialize(target string) (string, error) {
	if target == "" {
		return "", fmt.Errorf("init target is empty")
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve init target: %w", err)
	}
	if err := checkTarget(absTarget); err != nil {
		return "", err
	}

	targetExisted := true
	if _, err := os.Stat(absTarget); os.IsNotExist(err) {
		targetExisted = false
		if err := os.MkdirAll(absTarget, 0o755); err != nil {
			return "", fmt.Errorf("create init target %s: %w", absTarget, err)
		}
	}
	cleanupTarget := func() {
		if !targetExisted {
			_ = os.Remove(absTarget)
		}
	}

	stage, err := os.MkdirTemp(absTarget, ".rses-init-")
	if err != nil {
		cleanupTarget()
		return "", fmt.Errorf("create init staging directory: %w", err)
	}
	defer os.RemoveAll(stage)
	cleanupFailedInit := func() {
		_ = os.RemoveAll(stage)
		cleanupTarget()
	}

	if err := os.WriteFile(filepath.Join(stage, configFileName), defaultConfig, 0o644); err != nil {
		cleanupFailedInit()
		return "", fmt.Errorf("stage %s: %w", configFileName, err)
	}
	if err := extractProviders(stage); err != nil {
		cleanupFailedInit()
		return "", err
	}

	moved := []string{}
	for _, name := range []string{configFileName, "providers"} {
		from := filepath.Join(stage, name)
		to := filepath.Join(absTarget, name)
		if err := os.Rename(from, to); err != nil {
			for i := len(moved) - 1; i >= 0; i-- {
				_ = os.Rename(filepath.Join(absTarget, moved[i]), filepath.Join(stage, moved[i]))
			}
			cleanupFailedInit()
			return "", fmt.Errorf("install %s: %w", to, err)
		}
		moved = append(moved, name)
	}
	return absTarget, nil
}

func checkTarget(target string) error {
	if info, err := os.Stat(target); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("init target %s is not a directory", target)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect init target %s: %w", target, err)
	}
	for _, name := range []string{configFileName, "providers"} {
		path := filepath.Join(target, name)
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("refusing to overwrite existing %s", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect init destination %s: %w", path, err)
		}
	}
	return nil
}

func extractProviders(stage string) error {
	return fs.WalkDir(providers.FS, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		destination := filepath.Join(stage, "providers", filepath.FromSlash(path))
		if entry.IsDir() {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return fmt.Errorf("stage provider directory %s: %w", path, err)
			}
			return nil
		}
		data, err := providers.FS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded provider file %s: %w", path, err)
		}
		if err := os.WriteFile(destination, data, 0o644); err != nil {
			return fmt.Errorf("stage provider file %s: %w", path, err)
		}
		return nil
	})
}
