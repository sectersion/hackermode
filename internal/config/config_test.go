package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault_FillsRequiredFields(t *testing.T) {
	c := Default()
	if c.UI.Theme == "" {
		t.Fatal("default theme should be non-empty")
	}
	if c.UI.SidePanelWidth == 0 {
		t.Fatal("default side panel width should be non-zero")
	}
	if !c.SidePanelEnabled() {
		t.Fatal("default side panel should be enabled")
	}
	if c.Marketplace.Registry == "" {
		t.Fatal("default registry should be set")
	}
	if len(c.Keymap) == 0 {
		t.Fatal("default keymap should be populated")
	}
}

func TestSidePanelEnabled_RespectsExplicitFalse(t *testing.T) {
	c := Default()
	f := false
	c.UI.SidePanel = &f
	if c.SidePanelEnabled() {
		t.Fatal("explicit false should disable panel")
	}
}

func TestLoad_NoFileReturnsDefaults(t *testing.T) {
	// Point XDG_CONFIG_HOME at a temp dir with no config.toml.
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	c, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.UI.Theme != Default().UI.Theme {
		t.Fatalf("expected default theme, got %q", c.UI.Theme)
	}
}

func TestLoad_MergesUserConfigOverDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfgDir := filepath.Join(dir, "hackermode")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `
[ui]
theme = "tokyo-night"
side_panel_width = 20

[keymap]
"quit" = "ctrl+q"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.UI.Theme != "tokyo-night" {
		t.Fatalf("expected tokyo-night, got %q", c.UI.Theme)
	}
	if c.UI.SidePanelWidth != 20 {
		t.Fatalf("expected width 20, got %d", c.UI.SidePanelWidth)
	}
	if c.Keymap["quit"] != "ctrl+q" {
		t.Fatalf("expected quit override, got %q", c.Keymap["quit"])
	}
	// Defaults for non-overridden keys should remain.
	if c.Keymap["toggle_panel"] == "" {
		t.Fatal("non-overridden keymap entries should remain")
	}
}

func TestLoad_InvalidTOMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgDir := filepath.Join(dir, "hackermode")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("this is not [[[ valid toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("expected parse error")
	}
}
