// Package installer — Scan: enumerate installed modules.
//
// Walks ~/.local/share/hackermode/modules/ and returns one entry per
// module that has a usable `current` symlink + a parseable manifest.
//
// Used at host startup so the palette / command registry can advertise
// every installed module's static commands before any of them actually
// spawn. This is the bit that finally lights up the lazy-spawn flow:
// users see commands they can invoke; invocation spawns the module on
// demand.
package installer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	hlog "github.com/sectersion/hackermode/internal/log"
	"github.com/sectersion/hackermode/internal/modules"
)

// Installed is one discovered installation.
type Installed struct {
	// ID is the module ID (= directory name under modules/).
	ID string
	// Dir is the resolved per-version directory (what `current` points to).
	Dir string
	// Manifest is the parsed hackermode.toml in Dir.
	Manifest *modules.Manifest
}

// Scan returns every installable module under ModulesDir. Modules whose
// `current` link is missing or whose manifest fails to parse are skipped
// with a warning log; they don't abort the scan.
func Scan() ([]Installed, error) {
	root := ModulesDir()
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	out := make([]Installed, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		link := CurrentLink(id)
		target, err := os.Readlink(link)
		if err != nil {
			hlog.With("module", id).Warn("skip (no current symlink)", "err", err.Error())
			continue
		}
		dir := filepath.Join(ModuleDir(id), target)
		manifestPath := filepath.Join(dir, modules.ManifestFileName)
		m, err := modules.ParseFile(manifestPath)
		if err != nil {
			hlog.With("module", id).Warn("skip (manifest parse failed)", "err", err.Error())
			continue
		}
		if errs := m.Validate(); len(errs) > 0 {
			hlog.With("module", id).Warn("skip (manifest invalid)", "count", len(errs))
			continue
		}
		out = append(out, Installed{ID: id, Dir: dir, Manifest: m})
	}
	return out, nil
}
