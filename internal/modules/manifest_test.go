package modules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validManifest = `
[module]
id          = "acme.email"
name        = "Email"
version     = "0.3.1"
description = "IMAP/SMTP email client"
author      = "Acme"
license     = "MIT"

[entry]
binary = "hackermode-email"
mode   = "tui"
ansi   = true

[capabilities]
required = ["network", "secrets"]
optional = ["clipboard"]

[platforms]
supported = ["linux/amd64", "darwin/arm64"]

[[commands]]
id    = "acme.email.compose"
title = "Compose Email"
hint  = "Open new draft"
tags  = ["mail", "write"]
when  = "tab.module == 'acme.email'"

[[commands]]
id    = "acme.email.search"
title = "Search Mailbox"

[[keybinds]]
action = "acme.email.compose"
keys   = "ctrl+alt+m"

[dependencies]
"acme.oauth" = "^1.0"
`

func TestParse_ValidManifest(t *testing.T) {
	m, err := Parse([]byte(validManifest))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Module.ID != "acme.email" {
		t.Fatalf("id: %q", m.Module.ID)
	}
	if m.Entry.Mode != ModeTUI {
		t.Fatalf("mode: %q", m.Entry.Mode)
	}
	if !m.Entry.ANSI {
		t.Fatal("expected ansi=true")
	}
	if len(m.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(m.Commands))
	}
	if len(m.Keybinds) != 1 || m.Keybinds[0].Keys != "ctrl+alt+m" {
		t.Fatalf("keybind: %+v", m.Keybinds)
	}
	if m.Dependencies["acme.oauth"] != "^1.0" {
		t.Fatalf("dependencies: %+v", m.Dependencies)
	}
	if errs := m.Validate(); len(errs) != 0 {
		t.Fatalf("expected no validation errors, got %v", errs)
	}
}

func TestParse_DefaultsModeToStream(t *testing.T) {
	m, err := Parse([]byte(`
[module]
id = "a.b"
name = "x"
version = "0.1"

[entry]
binary = "x"
`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Entry.Mode != ModeStream {
		t.Fatalf("expected stream default, got %q", m.Entry.Mode)
	}
}

func TestParse_InvalidTOMLReturnsError(t *testing.T) {
	if _, err := Parse([]byte("this is not [[[ valid")); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidate_RequiresIDNameVersion(t *testing.T) {
	m := &Manifest{Entry: EntrySection{Binary: "x", Mode: ModeStream}}
	errs := m.Validate()
	if len(errs) < 3 {
		t.Fatalf("expected at least 3 errors, got %v", errs)
	}
}

func TestValidate_RejectsBadModuleID(t *testing.T) {
	m := &Manifest{
		Module: ModuleSection{ID: "no-dots", Name: "x", Version: "0.1"},
		Entry:  EntrySection{Binary: "x", Mode: ModeStream},
	}
	errs := m.Validate()
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "reverse-DNS") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected reverse-DNS error, got %v", errs)
	}
}

func TestValidate_RejectsUnnamespacedCommand(t *testing.T) {
	m := &Manifest{
		Module:   ModuleSection{ID: "acme.email", Name: "x", Version: "0.1"},
		Entry:    EntrySection{Binary: "x", Mode: ModeStream},
		Commands: []CommandSpec{{ID: "compose", Title: "Compose"}},
	}
	errs := m.Validate()
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "namespaced") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected namespace error, got %v", errs)
	}
}

func TestValidate_RejectsUnknownMode(t *testing.T) {
	m := &Manifest{
		Module: ModuleSection{ID: "a.b", Name: "x", Version: "0.1"},
		Entry:  EntrySection{Binary: "x", Mode: "weird"},
	}
	errs := m.Validate()
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "entry.mode") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected mode error, got %v", errs)
	}
}

func TestParseFile_PopulatesPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ManifestFileName)
	if err := os.WriteFile(path, []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := ParseDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Path != path {
		t.Fatalf("expected path %q, got %q", path, m.Path)
	}
}

func TestParseFile_MissingFileError(t *testing.T) {
	if _, err := ParseFile("/no/such/file.toml"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestIsModuleID(t *testing.T) {
	good := []string{"a.b", "acme.email", "corp.acme.email", "a_b.c-d"}
	bad := []string{"", "noDots", ".leading", "trailing.", "a..b", "bad space.x"}
	for _, s := range good {
		if !isModuleID(s) {
			t.Errorf("expected %q valid", s)
		}
	}
	for _, s := range bad {
		if isModuleID(s) {
			t.Errorf("expected %q invalid", s)
		}
	}
}
