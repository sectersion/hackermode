// Package installer turns a resolved module graph into files on disk.
// This file holds the path conventions; download/extract/verify logic
// lives in installer.go.
//
// Layout under DataDir (~/.local/share/hackermode):
//
//   modules/
//   ├── acme.email/
//   │   ├── 0.3.4/
//   │   │   ├── hackermode.toml
//   │   │   ├── hackermode-email          (the binary)
//   │   │   ├── checksums.txt
//   │   │   └── ...
//   │   └── current -> 0.3.4              (symlink kept in sync per-platform)
//   └── acme.oauth/...
//
// Several versions of the same module may coexist; `current` is what the
// host actually launches. Phase E may switch this to a content-addressed
// store, but the public path (`current`) stays the same so the host's
// spawn code doesn't change.
package installer

import (
	"path/filepath"

	"github.com/sectersion/hackermode/internal/paths"
)

// ModulesDir is the root containing all installed modules.
func ModulesDir() string { return paths.ModulesDir() }

// ModuleDir returns the per-module directory: <ModulesDir>/<id>/
func ModuleDir(id string) string {
	return filepath.Join(ModulesDir(), id)
}

// VersionDir returns the per-version directory: <ModulesDir>/<id>/<version>/
func VersionDir(id, version string) string {
	return filepath.Join(ModuleDir(id), version)
}

// CurrentLink returns the path to the "current" symlink for a module.
// The host's spawn logic always launches the binary inside whatever
// directory this symlink resolves to.
func CurrentLink(id string) string {
	return filepath.Join(ModuleDir(id), "current")
}
