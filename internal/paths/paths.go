// Package paths resolves XDG-style filesystem locations used by hackermode.
package paths

import (
	"os"
	"path/filepath"
)

const appName = "hackermode"

func xdg(envVar, fallback string) string {
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, fallback)
}

func ConfigDir() string {
	return filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), appName)
}

func DataDir() string {
	return filepath.Join(xdg("XDG_DATA_HOME", ".local/share"), appName)
}

func CacheDir() string {
	return filepath.Join(xdg("XDG_CACHE_HOME", ".cache"), appName)
}

func ConfigFile() string {
	return filepath.Join(ConfigDir(), "config.toml")
}

func ProfilesDir() string {
	return filepath.Join(ConfigDir(), "profiles")
}

func ModulesDir() string {
	return filepath.Join(DataDir(), "modules")
}

func RegistryCacheDir() string {
	return filepath.Join(DataDir(), "registry-cache")
}

func EnsureAll() error {
	for _, d := range []string{ConfigDir(), DataDir(), CacheDir(), ProfilesDir(), ModulesDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
