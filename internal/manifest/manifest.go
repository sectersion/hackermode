// Package manifest is the user-facing `hackermode.toml` — the file that
// declares which modules are installed in a profile / project.
//
// This is distinct from a module's own `hackermode.toml` (handled by
// internal/modules.Manifest). They share a name and a file format because
// they describe related things, but the schemas are different and the
// loaders are deliberately decoupled.
//
// Schema (Phase C):
//
//	[hackermode]
//	version = "0.1"               # manifest schema version
//
//	[profile]
//	name        = "work"
//	description = "Daily driver"
//	extends     = ["base"]        # optional profile inheritance
//
//	[modules]
//	"acme.email"    = "^0.3"
//	"acme.ai-chat"  = "~1.2.0"
//	"local-thing"   = { path = "../my-module" }    # dev override
//	"corp.internal" = { git = "https://...", rev = "v0.4.1" }
//	"alt-mod"       = { registry = "alt", version = "^2" }
//
//	[modules.config."acme.email"]
//	default_account = "me@example.com"
//
//	[ui]
//	theme            = "tokyo-night"
//	side_panel_width = 14
//
// Each top-level [modules] entry decodes to a Dependency. The TOML form
// is either a bare version-constraint string ("^1.2") or an inline table.
// That dual form is awkward to express in a Go struct, so we decode the
// file twice: once into a top-level schema, and once into a map of
// toml.Primitive values that we walk to materialize Dependency entries.
package manifest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
)

// SchemaVersion is the manifest schema version this code understands.
const SchemaVersion = "0.1"

// Manifest is the parsed `hackermode.toml`.
type Manifest struct {
	Hackermode HackermodeSection     `toml:"hackermode"`
	Profile    ProfileSection        `toml:"profile"`
	Modules    map[string]Dependency `toml:"-"`
	UI         UISection             `toml:"ui"`

	// Path is the absolute path the manifest was loaded from (zero when
	// parsed from raw bytes). Used for relative path resolution and for
	// diagnostics.
	Path string `toml:"-"`
}

type HackermodeSection struct {
	Version string `toml:"version"`
}

type ProfileSection struct {
	Name        string   `toml:"name"`
	Description string   `toml:"description"`
	Extends     []string `toml:"extends"`
}

type UISection struct {
	Theme          string `toml:"theme"`
	SidePanel      *bool  `toml:"side_panel"`
	SidePanelWidth int    `toml:"side_panel_width"`
}

// Dependency describes a single entry in [modules]. Exactly one of
// Version / Path / Git is non-empty.
type Dependency struct {
	// Version is a semver constraint string ("^1.2", "~0.3.4", ">=1.0").
	// Empty when this dep is a path or git override.
	Version string

	// Path is a local directory containing the module's source. The host
	// uses path deps for dev workflows; they don't appear in the
	// lockfile and prevent the manifest from being published.
	Path string

	// Git is a repository URL when the dep is pinned to a specific
	// commit / tag rather than a registry version.
	Git    string
	GitRev string

	// Registry is the optional registry name when not the default.
	// Empty means "use the default registry from config.toml".
	Registry string
}

// IsPath reports whether this is a local-path dep.
func (d Dependency) IsPath() bool { return d.Path != "" }

// IsGit reports whether this is a git-pinned dep.
func (d Dependency) IsGit() bool { return d.Git != "" }

// IsRegistry reports whether this is a normal registry dep.
func (d Dependency) IsRegistry() bool {
	return d.Version != "" && d.Path == "" && d.Git == ""
}

// String returns a stable representation for logs and lockfile comments.
func (d Dependency) String() string {
	switch {
	case d.IsPath():
		return fmt.Sprintf("path = %q", d.Path)
	case d.IsGit():
		return fmt.Sprintf("git = %q rev = %q", d.Git, d.GitRev)
	case d.Registry != "":
		return fmt.Sprintf("registry = %q version = %q", d.Registry, d.Version)
	default:
		return fmt.Sprintf("version = %q", d.Version)
	}
}

// ParseFile loads a manifest from disk. The result's Path is set even on
// parse failure to ease error reporting.
func ParseFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("manifest not found at %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	m, err := Parse(data)
	if m != nil {
		m.Path = path
	}
	return m, err
}

// rawManifest captures the parts of the file that need bespoke decoding.
// The [modules] values can be either a string or an inline table; we
// capture them as toml.Primitive and decode each in a second pass.
type rawManifest struct {
	Hackermode HackermodeSection         `toml:"hackermode"`
	Profile    ProfileSection            `toml:"profile"`
	UI         UISection                 `toml:"ui"`
	Modules    map[string]toml.Primitive `toml:"modules"`
}

// Parse decodes the bytes into a Manifest.
func Parse(data []byte) (*Manifest, error) {
	var raw rawManifest
	meta, err := toml.Decode(string(data), &raw)
	if err != nil {
		return nil, fmt.Errorf("invalid TOML: %w", err)
	}
	m := &Manifest{
		Hackermode: raw.Hackermode,
		Profile:    raw.Profile,
		UI:         raw.UI,
		Modules:    make(map[string]Dependency, len(raw.Modules)),
	}
	for id, primitive := range raw.Modules {
		// Skip the [modules.config] sub-table — that's not a dep, it's
		// per-module non-sensitive config defaults. We don't currently
		// surface it, but accepting it without erroring future-proofs
		// the schema.
		if id == "config" {
			continue
		}
		dep, derr := parseDep(meta, primitive)
		if derr != nil {
			return nil, fmt.Errorf("[modules].%q: %w", id, derr)
		}
		m.Modules[id] = dep
	}
	return m, nil
}

// parseDep normalizes a TOML value into a Dependency. The value is
// either a bare string (version constraint) or an inline table.
func parseDep(meta toml.MetaData, raw toml.Primitive) (Dependency, error) {
	// Try string first.
	var s string
	if err := meta.PrimitiveDecode(raw, &s); err == nil {
		if s == "" {
			return Dependency{}, errors.New("empty version constraint")
		}
		return Dependency{Version: s}, nil
	}
	// Fall back to inline table.
	var t depTable
	if err := meta.PrimitiveDecode(raw, &t); err != nil {
		return Dependency{}, fmt.Errorf("must be a version string or an inline table: %w", err)
	}
	d := Dependency{
		Version:  t.Version,
		Path:     t.Path,
		Git:      t.Git,
		GitRev:   t.Rev,
		Registry: t.Registry,
	}
	if err := validateDep(d); err != nil {
		return Dependency{}, err
	}
	return d, nil
}

type depTable struct {
	Version  string `toml:"version"`
	Path     string `toml:"path"`
	Git      string `toml:"git"`
	Rev      string `toml:"rev"`
	Registry string `toml:"registry"`
}

func validateDep(d Dependency) error {
	switch {
	case d.IsPath():
		if d.Version != "" || d.Git != "" {
			return errors.New("path dep can not be combined with version/git")
		}
	case d.IsGit():
		if d.Version != "" {
			return errors.New("git dep can not be combined with version")
		}
		if d.GitRev == "" {
			return errors.New("git dep needs a rev")
		}
	default:
		if d.Version == "" {
			return errors.New("dep must specify version, path, or git")
		}
	}
	return nil
}

// Validate runs structural checks. Each returned error is independent.
func (m *Manifest) Validate() []error {
	var errs []error
	if m.Hackermode.Version != "" && m.Hackermode.Version != SchemaVersion {
		errs = append(errs, fmt.Errorf(
			"hackermode.version %q unsupported (this build understands %q)",
			m.Hackermode.Version, SchemaVersion))
	}
	for id := range m.Modules {
		if id == "" {
			errs = append(errs, errors.New("[modules] has an empty key"))
		}
	}
	return errs
}
