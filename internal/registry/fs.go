// Package registry — filesystem backend.
//
// A FS Client serves the wire-shape directly out of a directory on disk:
//
//   <root>/
//   ├── index.json
//   └── modules/
//       └── <id>/
//           ├── module.json
//           └── <version>/
//               ├── manifest.toml
//               ├── checksums.txt
//               └── <platform>.tar.gz
//
// This is the implementation used by tests, the `hackermode dev` flow,
// and the `hackermode registry serve` HTTP server (which exposes the
// same directory through HTTP). The Node.js production implementation
// replaces it with whatever backing store it chooses, but the JSON
// shapes stay identical.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// FS is the filesystem-backed Client.
type FS struct {
	Root string // absolute path to the registry root
}

// NewFS returns an FS client rooted at the given directory.
func NewFS(root string) *FS { return &FS{Root: root} }

func (f *FS) Index() (*Index, error) {
	data, err := os.ReadFile(filepath.Join(f.Root, "index.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("registry: missing index.json at %s: %w", f.Root, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("registry: parse index.json: %w", err)
	}
	if idx.Schema != "" && idx.Schema != SchemaVersion {
		return nil, fmt.Errorf("registry: schema %q unsupported (this build understands %q)",
			idx.Schema, SchemaVersion)
	}
	return &idx, nil
}

func (f *FS) Module(id string) (*Module, error) {
	path := filepath.Join(f.Root, "modules", id, "module.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("registry: module %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("registry: read module %q: %w", id, err)
	}
	var m Module
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("registry: parse module.json for %q: %w", id, err)
	}
	return &m, nil
}

func (f *FS) Checksums(id, version string) (*ChecksumsFile, error) {
	path := filepath.Join(f.Root, "modules", id, version, "checksums.txt")
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("registry: checksums for %s@%s: %w", id, version, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("registry: read checksums: %w", err)
	}
	defer file.Close()
	return ParseChecksums(file)
}

func (f *FS) FetchBinary(id, version, platform string) (io.ReadCloser, error) {
	name := tarballName(platform)
	path := filepath.Join(f.Root, "modules", id, version, name)
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("registry: %s@%s/%s: %w", id, version, platform, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("registry: open binary: %w", err)
	}
	return file, nil
}

// tarballName maps a "linux/amd64"-style platform string to the filename
// convention used in the filesystem layout.
func tarballName(platform string) string {
	// "linux/amd64" → "linux-amd64.tar.gz"
	mapped := platform
	for i, r := range mapped {
		if r == '/' {
			mapped = mapped[:i] + "-" + mapped[i+1:]
			break
		}
	}
	return mapped + ".tar.gz"
}
