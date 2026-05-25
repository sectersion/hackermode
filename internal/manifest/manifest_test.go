package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleManifest = `
[hackermode]
version = "0.1"

[profile]
name        = "work"
description = "Daily driver"
extends     = ["base"]

[modules]
"acme.email"   = "^0.3"
"acme.ai"     = "~1.2.0"
"corp.bits"    = { git = "https://example.com/repo", rev = "v0.4.1" }
"local-thing"  = { path = "../my-module" }
"alt-mod"      = { registry = "alt", version = "^2" }

[ui]
theme            = "tokyo-night"
side_panel_width = 14
`

func TestParse_AllDepShapes(t *testing.T) {
	m, err := Parse([]byte(sampleManifest))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Profile.Name != "work" {
		t.Fatalf("profile name: %q", m.Profile.Name)
	}
	if len(m.Profile.Extends) != 1 || m.Profile.Extends[0] != "base" {
		t.Fatalf("extends: %v", m.Profile.Extends)
	}
	if m.UI.Theme != "tokyo-night" {
		t.Fatalf("ui theme: %q", m.UI.Theme)
	}
	if m.UI.SidePanelWidth != 14 {
		t.Fatalf("ui width: %d", m.UI.SidePanelWidth)
	}

	wantDeps := map[string]Dependency{
		"acme.email":  {Version: "^0.3"},
		"acme.ai":     {Version: "~1.2.0"},
		"corp.bits":   {Git: "https://example.com/repo", GitRev: "v0.4.1"},
		"local-thing": {Path: "../my-module"},
		"alt-mod":     {Registry: "alt", Version: "^2"},
	}
	if len(m.Modules) != len(wantDeps) {
		t.Fatalf("expected %d modules, got %d", len(wantDeps), len(m.Modules))
	}
	for id, want := range wantDeps {
		got, ok := m.Modules[id]
		if !ok {
			t.Fatalf("missing dep %q", id)
		}
		if got != want {
			t.Fatalf("dep %q: got %+v want %+v", id, got, want)
		}
	}
}

func TestParse_InvalidDepShape(t *testing.T) {
	cases := map[string]string{
		"empty version":     `[modules]` + "\n" + `"x" = ""`,
		"path + version":    `[modules]` + "\n" + `"x" = { path = "./y", version = "^1" }`,
		"git without rev":   `[modules]` + "\n" + `"x" = { git = "https://e/r" }`,
		"missing both":      `[modules]` + "\n" + `"x" = { registry = "alt" }`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(src)); err == nil {
				t.Fatalf("expected error for %q", src)
			}
		})
	}
}

func TestValidate_RejectsUnsupportedSchemaVersion(t *testing.T) {
	m, err := Parse([]byte(`
[hackermode]
version = "9.9"
`))
	if err != nil {
		t.Fatal(err)
	}
	errs := m.Validate()
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "unsupported") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unsupported-version error, got %v", errs)
	}
}

func TestDependency_Helpers(t *testing.T) {
	cases := []struct {
		dep   Dependency
		isReg bool
		isPath bool
		isGit bool
	}{
		{Dependency{Version: "^1"}, true, false, false},
		{Dependency{Path: "./x"}, false, true, false},
		{Dependency{Git: "https://e/r", GitRev: "main"}, false, false, true},
		{Dependency{Registry: "alt", Version: "^2"}, true, false, false},
	}
	for _, c := range cases {
		if c.dep.IsRegistry() != c.isReg ||
			c.dep.IsPath() != c.isPath ||
			c.dep.IsGit() != c.isGit {
			t.Errorf("classifier mismatch for %+v", c.dep)
		}
		if c.dep.String() == "" {
			t.Errorf("String() empty for %+v", c.dep)
		}
	}
}

func TestParseFile_PopulatesPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, FileName)
	if err := os.WriteFile(p, []byte(sampleManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := ParseFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if m.Path != p {
		t.Fatalf("path: %q want %q", m.Path, p)
	}
}

func TestParseFile_MissingError(t *testing.T) {
	if _, err := ParseFile("/no/such/file"); err == nil {
		t.Fatal("expected error")
	}
}
