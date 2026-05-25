package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocate_ProjectWalkUp(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, FileName)
	if err := os.WriteFile(manifestPath, []byte(`[hackermode]
version = "0.1"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	p, s, err := Locate(LocateOpts{CWD: deep})
	if err != nil {
		t.Fatal(err)
	}
	if s != ScopeProject {
		t.Fatalf("scope: %v", s)
	}
	if p != manifestPath {
		t.Fatalf("path: %q want %q", p, manifestPath)
	}
}

func TestLocate_ProfileFallback(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	profDir := filepath.Join(cfg, "hackermode", "profiles")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prof := filepath.Join(profDir, "work.toml")
	if err := os.WriteFile(prof, []byte(`[profile]
name = "work"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	other := t.TempDir() // no project manifest here
	_, s, err := Locate(LocateOpts{CWD: other, Profile: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if s != ScopeProfile {
		t.Fatalf("scope: %v", s)
	}
}

func TestLocate_SystemFallback(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	if err := os.MkdirAll(filepath.Join(cfg, "hackermode"), 0o755); err != nil {
		t.Fatal(err)
	}
	sys := filepath.Join(cfg, "hackermode", FileName)
	if err := os.WriteFile(sys, []byte(`[hackermode]
version = "0.1"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	other := t.TempDir()
	_, s, err := Locate(LocateOpts{CWD: other})
	if err != nil {
		t.Fatal(err)
	}
	if s != ScopeSystem {
		t.Fatalf("scope: %v", s)
	}
}

func TestLocate_NoneFound(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	other := t.TempDir()
	_, _, err := Locate(LocateOpts{CWD: other})
	if err != ErrNoManifest {
		t.Fatalf("expected ErrNoManifest, got %v", err)
	}
}

func TestLoad_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, FileName)
	if err := os.WriteFile(manifestPath, []byte(sampleManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	m, s, err := Load(LocateOpts{CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	if s != ScopeProject {
		t.Fatalf("scope: %v", s)
	}
	if len(m.Modules) == 0 {
		t.Fatal("expected modules parsed")
	}
}
