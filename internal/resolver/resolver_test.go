package resolver

import (
	"errors"
	"testing"

	"github.com/sectersion/hackermode/internal/manifest"
)

// fakeIndex is a static VersionIndex for tests.
type fakeIndex map[string][]VersionInfo

func (f fakeIndex) Versions(id string) ([]VersionInfo, error) {
	v, ok := f[id]
	if !ok {
		return nil, errors.New("unknown module")
	}
	return v, nil
}

func TestResolve_NoDependencies(t *testing.T) {
	m := &manifest.Manifest{Modules: map[string]manifest.Dependency{}}
	r, err := Resolve(m, fakeIndex{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Pins) != 0 {
		t.Fatalf("expected no pins, got %v", r.Pins)
	}
}

func TestResolve_TopLevelOnly(t *testing.T) {
	m := &manifest.Manifest{Modules: map[string]manifest.Dependency{
		"acme.email": {Version: "^0.3"},
	}}
	idx := fakeIndex{
		"acme.email": {
			{Version: "0.3.0"},
			{Version: "0.3.4"},
			{Version: "0.2.9"},
			{Version: "0.4.0"}, // outside ^0.3
		},
	}
	r, err := Resolve(m, idx)
	if err != nil {
		t.Fatal(err)
	}
	pin, ok := r.Pins["acme.email"]
	if !ok {
		t.Fatal("missing pin")
	}
	if pin.Source != "registry" {
		t.Fatalf("source: %q", pin.Source)
	}
	if pin.Version != "0.3.4" {
		t.Fatalf("expected highest matching version 0.3.4, got %q", pin.Version)
	}
}

func TestResolve_TransitiveDeps(t *testing.T) {
	m := &manifest.Manifest{Modules: map[string]manifest.Dependency{
		"acme.email": {Version: "^0.3"},
	}}
	idx := fakeIndex{
		"acme.email": {
			{Version: "0.3.4", Dependencies: map[string]string{
				"acme.oauth": "^1.0",
			}},
		},
		"acme.oauth": {
			{Version: "1.0.0"},
			{Version: "1.0.7"},
		},
	}
	r, err := Resolve(m, idx)
	if err != nil {
		t.Fatal(err)
	}
	if r.Pins["acme.oauth"].Version != "1.0.7" {
		t.Fatalf("expected transitive pin oauth=1.0.7, got %+v", r.Pins["acme.oauth"])
	}
}

func TestResolve_ConflictReported(t *testing.T) {
	m := &manifest.Manifest{Modules: map[string]manifest.Dependency{
		"x": {Version: "^1"},
		"y": {Version: "^1"},
	}}
	idx := fakeIndex{
		"x": {
			{Version: "1.0.0", Dependencies: map[string]string{"shared": "^1.0"}},
		},
		"y": {
			{Version: "1.0.0", Dependencies: map[string]string{"shared": "^2.0"}},
		},
		"shared": {
			{Version: "1.0.0"},
			{Version: "2.0.0"},
		},
	}
	_, err := Resolve(m, idx)
	if err == nil {
		t.Fatal("expected conflict")
	}
}

func TestResolve_PathOverrideSatisfiesConstraint(t *testing.T) {
	m := &manifest.Manifest{Modules: map[string]manifest.Dependency{
		"acme.email": {Path: "/tmp/email"},
	}}
	// Even though the index would have versions, the path override wins.
	idx := fakeIndex{
		"acme.email": {{Version: "0.3.4"}},
	}
	r, err := Resolve(m, idx)
	if err != nil {
		t.Fatal(err)
	}
	pin := r.Pins["acme.email"]
	if pin.Source != "path" {
		t.Fatalf("source: %q", pin.Source)
	}
	if pin.PathDir != "/tmp/email" {
		t.Fatalf("path: %q", pin.PathDir)
	}
}

func TestResolve_GitOverridePin(t *testing.T) {
	m := &manifest.Manifest{Modules: map[string]manifest.Dependency{
		"corp.internal": {Git: "https://example.com/r", GitRev: "v0.4.1"},
	}}
	r, err := Resolve(m, fakeIndex{})
	if err != nil {
		t.Fatal(err)
	}
	pin := r.Pins["corp.internal"]
	if pin.Source != "git" || pin.GitRev != "v0.4.1" {
		t.Fatalf("git pin: %+v", pin)
	}
}

func TestResolve_UnknownModuleErrors(t *testing.T) {
	m := &manifest.Manifest{Modules: map[string]manifest.Dependency{
		"missing": {Version: "^1"},
	}}
	_, err := Resolve(m, fakeIndex{})
	if err == nil {
		t.Fatal("expected error for unknown module")
	}
}

func TestResolve_DiamondDepResolves(t *testing.T) {
	// a → shared@^1
	// b → shared@^1
	// both should land on the same version
	m := &manifest.Manifest{Modules: map[string]manifest.Dependency{
		"a": {Version: "^1"},
		"b": {Version: "^1"},
	}}
	idx := fakeIndex{
		"a": {{Version: "1.0.0", Dependencies: map[string]string{"shared": "^1.0"}}},
		"b": {{Version: "1.0.0", Dependencies: map[string]string{"shared": "^1.0"}}},
		"shared": {
			{Version: "1.0.0"},
			{Version: "1.5.0"},
		},
	}
	r, err := Resolve(m, idx)
	if err != nil {
		t.Fatal(err)
	}
	if r.Pins["shared"].Version != "1.5.0" {
		t.Fatalf("shared: %+v", r.Pins["shared"])
	}
}
