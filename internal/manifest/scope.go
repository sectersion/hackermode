// Package manifest — scope resolution: where on disk to find the active
// hackermode.toml.
//
// The host loads exactly one manifest per invocation. The lookup order is:
//
//   1. Project — a hackermode.toml in cwd or any ancestor (git-style
//      walk-up). Useful for per-repo / per-directory environments.
//   2. Profile — ~/.config/hackermode/profiles/<name>.toml. Selected
//      via the --profile flag or the HACKERMODE_PROFILE env var.
//   3. System — ~/.config/hackermode/hackermode.toml. The default
//      fallback when no project manifest exists and no profile is
//      selected.
//
// Locate returns the resolved path (without parsing). Load returns the
// parsed manifest. Both report ErrNoManifest when no manifest exists in
// any scope.
package manifest

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/sectersion/hackermode/internal/paths"
)

// ErrNoManifest is returned when no hackermode.toml exists in any scope.
var ErrNoManifest = errors.New("no hackermode.toml found in project, profile, or system scope")

// FileName is the canonical file name. Both project and system
// manifests use this; profiles use <name>.toml.
const FileName = "hackermode.toml"

// Scope identifies which level a manifest came from.
type Scope int

const (
	ScopeNone Scope = iota
	ScopeProject
	ScopeProfile
	ScopeSystem
)

func (s Scope) String() string {
	switch s {
	case ScopeProject:
		return "project"
	case ScopeProfile:
		return "profile"
	case ScopeSystem:
		return "system"
	default:
		return "none"
	}
}

// LocateOpts narrows the scope lookup.
type LocateOpts struct {
	// CWD is the starting directory for the project walk-up. Empty =
	// use os.Getwd().
	CWD string
	// Profile is the profile name to try (between project + system).
	// Empty = skip profile scope.
	Profile string
}

// Locate finds the active manifest path and returns it along with the
// scope it was found in.
func Locate(opts LocateOpts) (string, Scope, error) {
	if p := locateProject(opts.CWD); p != "" {
		return p, ScopeProject, nil
	}
	if opts.Profile != "" {
		p := filepath.Join(paths.ProfilesDir(), opts.Profile+".toml")
		if exists(p) {
			return p, ScopeProfile, nil
		}
	}
	sys := filepath.Join(paths.ConfigDir(), FileName)
	if exists(sys) {
		return sys, ScopeSystem, nil
	}
	return "", ScopeNone, ErrNoManifest
}

// Load is Locate + ParseFile. The returned manifest has its Path set to
// the file it came from.
func Load(opts LocateOpts) (*Manifest, Scope, error) {
	p, s, err := Locate(opts)
	if err != nil {
		return nil, ScopeNone, err
	}
	m, err := ParseFile(p)
	if err != nil {
		return nil, s, err
	}
	return m, s, nil
}

// locateProject walks up from cwd to root looking for a hackermode.toml.
// Returns "" when none is found.
func locateProject(cwd string) string {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return ""
		}
	}
	for {
		candidate := filepath.Join(cwd, FileName)
		if exists(candidate) {
			return candidate
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return ""
		}
		cwd = parent
	}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
