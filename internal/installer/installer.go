// Package installer — high-level Install / Sync flow.
//
// Install(manifest, registry, opts) is the do-everything entrypoint:
//
//	resolve → fetch → verify (hash twice: registry metadata + checksums.txt)
//	  → extract → activate (point `current` symlink) → write lockfile.
//
// Sync(lockfile, registry, opts) is the same flow without resolution; it
// refuses to install anything whose hash doesn't match the lockfile.
//
// Path / git deps are surfaced in the lockfile but not fetched here:
//   path → no install action; the host's spawn code resolves PathDir.
//   git  → deferred to Phase F (sigstore + git checkout dance).
package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	hlog "github.com/sectersion/hackermode/internal/log"
	"github.com/sectersion/hackermode/internal/manifest"
	"github.com/sectersion/hackermode/internal/registry"
	"github.com/sectersion/hackermode/internal/resolver"
)

// Opts tunes installer behavior.
type Opts struct {
	// Platform is the target platform string ("linux/amd64",
	// "darwin/arm64"). Empty = the host's current GOOS/GOARCH.
	Platform string

	// Out is where progress lines go. Defaults to os.Stderr.
	Out io.Writer
}

func (o Opts) platform() string {
	if o.Platform != "" {
		return o.Platform
	}
	return runtime.GOOS + "/" + runtime.GOARCH
}

func (o Opts) out() io.Writer {
	if o.Out != nil {
		return o.Out
	}
	return os.Stderr
}

// Install resolves the manifest against reg, downloads/verifies/extracts
// every registry dep, writes the lockfile, and returns it.
func Install(m *manifest.Manifest, reg registry.Client, opts Opts) (*manifest.Lockfile, error) {
	if m == nil {
		return nil, errors.New("installer: manifest required")
	}
	if reg == nil {
		return nil, errors.New("installer: registry client required")
	}

	hlog.Info("install start", "platform", opts.platform())

	// 1) Resolve.
	idx := registry.IndexFromClient{C: reg}
	result, err := resolver.Resolve(m, idx)
	if err != nil {
		return nil, fmt.Errorf("resolve: %w", err)
	}

	// 2) Build the lockfile skeleton.
	lockPath := manifest.LockfilePathFor(m.Path)
	lock := manifest.NewLockfile(lockPath)
	lock.Meta.ManifestHash = manifestHash(m.Path)
	lock.Meta.Generated = time.Now().UTC().Truncate(time.Second)

	platform := opts.platform()

	// 3) Walk each pin.
	for id, pin := range result.Pins {
		switch pin.Source {
		case "path":
			fmt.Fprintf(opts.out(), "  path %s → %s\n", id, pin.PathDir)
			lock.Upsert(manifest.LockModule{
				ID:      id,
				Source:  "path",
				PathDir: pin.PathDir,
			})

		case "git":
			fmt.Fprintf(opts.out(), "  git  %s @ %s (deferred to Phase F)\n", id, pin.GitRev)
			lock.Upsert(manifest.LockModule{
				ID:     id,
				Source: "git",
				GitRev: pin.GitRev,
			})

		case "registry":
			entry, err := installRegistryDep(reg, id, pin.Version, platform, opts)
			if err != nil {
				return nil, fmt.Errorf("install %s@%s: %w", id, pin.Version, err)
			}
			lock.Upsert(entry)

		default:
			return nil, fmt.Errorf("install %s: unknown source %q", id, pin.Source)
		}
	}

	// 4) Persist.
	if err := lock.Write(); err != nil {
		return nil, fmt.Errorf("write lock: %w", err)
	}
	hlog.Info("install complete", "lockfile", lockPath, "modules", len(lock.Modules))
	return lock, nil
}

// Sync installs exactly what's in lock, without resolving anything.
// Every binary's hash is checked against the lockfile; mismatches abort.
func Sync(lock *manifest.Lockfile, reg registry.Client, opts Opts) error {
	if lock == nil {
		return errors.New("installer: lockfile required")
	}
	if reg == nil {
		return errors.New("installer: registry client required")
	}
	platform := opts.platform()
	for _, mod := range lock.Modules {
		switch mod.Source {
		case "registry":
			if err := syncRegistryDep(reg, mod, platform, opts); err != nil {
				return fmt.Errorf("sync %s@%s: %w", mod.ID, mod.Version, err)
			}
		case "path":
			fmt.Fprintf(opts.out(), "  path %s (no install action)\n", mod.ID)
		case "git":
			fmt.Fprintf(opts.out(), "  git  %s (deferred)\n", mod.ID)
		default:
			return fmt.Errorf("sync %s: unknown source %q", mod.ID, mod.Source)
		}
	}
	return nil
}

// manifestHash returns "sha256:..." for the bytes at path. An empty
// string is returned for parse-from-bytes manifests (Path is "").
func manifestHash(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
