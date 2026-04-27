package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const DangerouslySkipPermissionsEnv = "STASH_YOLO"

type Config struct {
	DangerouslySkipPermissions bool `toml:"dangerously_skip_permissions"`
}

func Path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".stash", "config.toml")
}

func Load() (Config, error) {
	path := Path()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

func DangerouslySkipPermissions() (bool, error) {
	if value, ok := os.LookupEnv(DangerouslySkipPermissionsEnv); ok {
		return truthy(value), nil
	}

	cfg, err := Load()
	if err != nil {
		return false, err
	}
	return cfg.DangerouslySkipPermissions, nil
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
