// Package config loads ~/.config/hackermode/config.toml — the local user config
// (registry endpoint, default profile, keymap overrides). The shareable manifest
// (hackermode.toml) is handled separately by internal/manifest.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/sectersion/hackermode/internal/paths"
)

type Config struct {
	Hackermode  Section            `toml:"hackermode"`
	UI          UI                 `toml:"ui"`
	Marketplace Marketplace        `toml:"marketplace"`
	Keymap      map[string]string  `toml:"keymap"`
}

type Section struct {
	DefaultProfile string `toml:"default_profile"`
}

type UI struct {
	Theme           string `toml:"theme"`
	SidePanel       *bool  `toml:"side_panel"`
	SidePanelWidth  int    `toml:"side_panel_width"`
}

type Marketplace struct {
	Registry string   `toml:"registry"`
	Mirrors  []string `toml:"mirrors"`
}

// Default returns the built-in defaults used when no config file exists or
// fields are absent.
func Default() Config {
	t := true
	return Config{
		UI: UI{
			Theme:          "default",
			SidePanel:      &t,
			SidePanelWidth: 12,
		},
		Marketplace: Marketplace{
			Registry: "git+https://github.com/hackermode/registry",
		},
		Keymap: defaultKeymap(),
	}
}

func defaultKeymap() map[string]string {
	return map[string]string{
		"quit":             "ctrl+c",
		"toggle_panel":     "ctrl+b",
		"command_palette":  "ctrl+p,ctrl+k",
		"new_tab":          "ctrl+t",
		"close_tab":        "ctrl+w",
		"next_tab":         "ctrl+tab",
		"prev_tab":         "ctrl+shift+tab",
		"focus_input":      "esc",
		"scroll_up":        "ctrl+u",
		"scroll_down":      "ctrl+d",
		"history_prev":     "up",
		"history_next":     "down",
		"complete":         "tab",
		"submit":           "enter",
	}
}

// Load reads the user config, falling back to defaults for any missing fields.
// A missing file is not an error.
func Load() (Config, error) {
	cfg := Default()

	path := paths.ConfigFile()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}

	var disk Config
	if _, err := toml.Decode(string(data), &disk); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}

	mergeInPlace(&cfg, &disk)
	return cfg, nil
}

func mergeInPlace(base, over *Config) {
	if over.Hackermode.DefaultProfile != "" {
		base.Hackermode.DefaultProfile = over.Hackermode.DefaultProfile
	}
	if over.UI.Theme != "" {
		base.UI.Theme = over.UI.Theme
	}
	if over.UI.SidePanel != nil {
		base.UI.SidePanel = over.UI.SidePanel
	}
	if over.UI.SidePanelWidth != 0 {
		base.UI.SidePanelWidth = over.UI.SidePanelWidth
	}
	if over.Marketplace.Registry != "" {
		base.Marketplace.Registry = over.Marketplace.Registry
	}
	if len(over.Marketplace.Mirrors) > 0 {
		base.Marketplace.Mirrors = over.Marketplace.Mirrors
	}
	for k, v := range over.Keymap {
		base.Keymap[k] = v
	}
}

// SidePanelEnabled reports whether the side panel should be visible by default.
func (c Config) SidePanelEnabled() bool {
	if c.UI.SidePanel == nil {
		return true
	}
	return *c.UI.SidePanel
}
