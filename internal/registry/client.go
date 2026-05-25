// Package registry — Client interface and helpers shared across backends.
//
// A Client knows nothing about how to install or where to put files —
// that's the installer's job. It only knows how to read the registry's
// wire shape and stream binary blobs.
package registry

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// ErrNotFound is returned by Client methods when a module / version is
// missing.
var ErrNotFound = errors.New("registry: not found")

// Client is the read-only registry interface.
//
// Both the filesystem-backed and (eventually) HTTP-backed registries
// implement this. The resolver wraps a Client behind a VersionIndex
// adapter; the installer uses it directly to fetch tarballs.
type Client interface {
	// Index returns the global module catalogue.
	Index() (*Index, error)

	// Module returns the full metadata for one module ID.
	Module(id string) (*Module, error)

	// Checksums returns the parsed checksums.txt for a (module, version).
	Checksums(id, version string) (*ChecksumsFile, error)

	// FetchBinary streams the binary tarball for (id, version, platform).
	// Callers must close the reader.
	FetchBinary(id, version, platform string) (io.ReadCloser, error)
}

// ParseChecksums parses the "sha256 filename" plain-text format used by
// checksums.txt. The reader is read to EOF but not closed.
func ParseChecksums(r io.Reader) (*ChecksumsFile, error) {
	out := &ChecksumsFile{Entries: map[string]string{}}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		out.Entries[fields[1]] = fields[0]
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
