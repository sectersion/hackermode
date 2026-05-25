// Package manifest — lockfile (`hackermode.lock`).
//
// The lockfile records exact resolved versions and per-platform binary
// hashes for every module in the manifest's graph. It is written by the
// installer after a successful resolution and consumed by `hackermode
// sync` to install reproducibly without re-resolving.
//
// Wire format (TOML):
//
//	[meta]
//	manifest_hash = "sha256:..."
//	generated     = "2026-05-23T12:34:56Z"
//	hackermode    = ">=0.1"
//
//	[[module]]
//	id      = "acme.email"
//	version = "0.3.4"
//	source  = "registry"
//	sha256  = "abc123..."
//
//	  [[module.binaries]]
//	  platform = "linux/amd64"
//	  sha256   = "def456..."
//	  url      = "https://.../email-0.3.4-linux-amd64.tar.gz"
//
//	  [[module.binaries]]
//	  platform = "darwin/arm64"
//	  sha256   = "..."
//	  url      = "..."
//
// Path deps are excluded from the binary list — they are inherently
// non-reproducible. Git deps lock to a resolved commit SHA. Registry
// deps include the tarball hashes for every supported platform.
package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// LockFileName is the canonical filename.
const LockFileName = "hackermode.lock"

// Lockfile is the parsed lockfile.
type Lockfile struct {
	Meta    LockMeta     `toml:"meta"`
	Modules []LockModule `toml:"module"`

	Path string `toml:"-"`
}

// LockMeta holds top-level metadata.
type LockMeta struct {
	ManifestHash string    `toml:"manifest_hash"`
	Generated    time.Time `toml:"generated"`
	Hackermode   string    `toml:"hackermode"`
}

// LockModule is one resolved module.
type LockModule struct {
	ID      string `toml:"id"`
	Version string `toml:"version"`

	// Source classifies the resolution: "registry", "git", or "path".
	Source string `toml:"source"`

	// SHA256 of the canonical module package (registry source).
	SHA256 string `toml:"sha256,omitempty"`

	// GitRev is the resolved commit SHA for git deps.
	GitRev string `toml:"git_rev,omitempty"`

	// PathDir is the absolute path for path deps.
	PathDir string `toml:"path_dir,omitempty"`

	Binaries []LockBinary `toml:"binaries,omitempty"`
}

// LockBinary is one platform-specific binary tarball.
type LockBinary struct {
	Platform string `toml:"platform"` // "linux/amd64" etc.
	SHA256   string `toml:"sha256"`
	URL      string `toml:"url"`
}

// ParseLockFile loads a lockfile from disk.
func ParseLockFile(path string) (*Lockfile, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("lockfile not found at %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	l, err := ParseLock(data)
	if l != nil {
		l.Path = path
	}
	return l, err
}

// ParseLock decodes raw TOML bytes into a Lockfile.
func ParseLock(data []byte) (*Lockfile, error) {
	var l Lockfile
	if _, err := toml.Decode(string(data), &l); err != nil {
		return nil, fmt.Errorf("invalid TOML: %w", err)
	}
	return &l, nil
}

// Write serializes the lockfile to its Path, creating parent
// directories. Output is deterministic (sorted by module ID).
func (l *Lockfile) Write() error {
	if l.Path == "" {
		return errors.New("lockfile: Path is empty")
	}
	data, err := l.Marshal()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return os.WriteFile(l.Path, data, 0o644)
}

// Marshal returns the canonical TOML bytes for this lockfile.
func (l *Lockfile) Marshal() ([]byte, error) {
	cp := *l
	cp.Modules = append([]LockModule(nil), l.Modules...)
	sort.Slice(cp.Modules, func(i, j int) bool { return cp.Modules[i].ID < cp.Modules[j].ID })
	for i := range cp.Modules {
		bins := append([]LockBinary(nil), cp.Modules[i].Binaries...)
		sort.Slice(bins, func(a, b int) bool { return bins[a].Platform < bins[b].Platform })
		cp.Modules[i].Binaries = bins
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(&cp); err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Get returns the entry for an ID, or false.
func (l *Lockfile) Get(id string) (LockModule, bool) {
	for _, m := range l.Modules {
		if m.ID == id {
			return m, true
		}
	}
	return LockModule{}, false
}

// Upsert inserts or replaces an entry.
func (l *Lockfile) Upsert(m LockModule) {
	for i, existing := range l.Modules {
		if existing.ID == m.ID {
			l.Modules[i] = m
			return
		}
	}
	l.Modules = append(l.Modules, m)
}

// Remove drops an entry by ID. Missing IDs are a no-op.
func (l *Lockfile) Remove(id string) {
	out := l.Modules[:0]
	for _, m := range l.Modules {
		if m.ID == id {
			continue
		}
		out = append(out, m)
	}
	l.Modules = out
}

// NewLockfile returns an empty lockfile pointed at path.
func NewLockfile(path string) *Lockfile {
	return &Lockfile{
		Meta: LockMeta{
			Generated:  time.Now().UTC().Truncate(time.Second),
			Hackermode: ">=" + SchemaVersion,
		},
		Path: path,
	}
}

// LockfilePathFor returns the conventional lockfile path next to a
// manifest. Project / system manifests get a sibling `hackermode.lock`;
// profile manifests get `<profile>.lock` next to them.
func LockfilePathFor(manifestPath string) string {
	dir := filepath.Dir(manifestPath)
	base := filepath.Base(manifestPath)
	if base == FileName {
		return filepath.Join(dir, LockFileName)
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	return filepath.Join(dir, stem+".lock")
}
