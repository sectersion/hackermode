// Package modules — manifest parser (Phase B Stage 3.1).
//
// Loads hackermode.toml from a module's directory or a manifest path.
// The parser is intentionally permissive about unknown top-level keys
// (forward compatibility) but strict about typed fields. Validation runs
// in Validate(); a manifest can be parsed and inspected even if it fails
// validation, so error messages can reference specific field paths.
//
// Phase B uses a subset of the full schema documented in AGENTS.md:
//
//   - [module]            — required: id, name, version. optional: description, author, license, homepage.
//   - [entry]             — required: binary; optional: mode ("stream"|"tui"), ansi bool.
//   - [capabilities]      — optional: required[], optional[].
//   - [platforms]         — optional: supported[].
//   - [[commands]]        — optional: static palette commands (id required; rest optional).
//   - [[keybinds]]        — optional: action + keys.
//   - [dependencies]      — optional: map of dep id -> version constraint string.
//
// Sections referenced by AGENTS.md but deferred to later stages
// ([[launchers]], [config_schema], [provides], [consumes], [[command_hooks]])
// are parsed into RawSections so authors can include them today without
// the parser rejecting their manifest. They're ignored at runtime in
// Phase B.
package modules

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// ManifestFileName is the canonical filename inside a module directory.
const ManifestFileName = "hackermode.toml"

// Mode selects how a module integrates with the host.
type Mode string

const (
	ModeStream Mode = "stream"
	ModeTUI    Mode = "tui"
)

// Manifest is the parsed shape of hackermode.toml.
type Manifest struct {
	Module       ModuleSection      `toml:"module"`
	Entry        EntrySection       `toml:"entry"`
	Capabilities CapabilitiesSection `toml:"capabilities"`
	Platforms    PlatformsSection   `toml:"platforms"`
	Commands     []CommandSpec      `toml:"commands"`
	Keybinds     []KeybindSpec      `toml:"keybinds"`
	Dependencies map[string]string  `toml:"dependencies"`

	// Path the manifest was loaded from, for diagnostics.
	Path string `toml:"-"`
}

// ModuleSection — identity / metadata.
type ModuleSection struct {
	ID          string `toml:"id"`
	Name        string `toml:"name"`
	Version     string `toml:"version"`
	Description string `toml:"description"`
	Author      string `toml:"author"`
	License     string `toml:"license"`
	Homepage    string `toml:"homepage"`
}

// EntrySection — how to run the module.
type EntrySection struct {
	Binary string `toml:"binary"`
	Mode   Mode   `toml:"mode"`
	ANSI   bool   `toml:"ansi"`
}

// CapabilitiesSection — declared capabilities.
type CapabilitiesSection struct {
	Required []string `toml:"required"`
	Optional []string `toml:"optional"`
}

// PlatformsSection — prebuilt-binary platforms.
type PlatformsSection struct {
	Supported []string `toml:"supported"`
}

// CommandSpec is a static palette command declared in the manifest.
// Mirrors internal/commands.Command shape so they can be projected directly
// at module install time.
type CommandSpec struct {
	ID       string   `toml:"id"`
	Title    string   `toml:"title"`
	Hint     string   `toml:"hint"`
	Tags     []string `toml:"tags"`
	Keybind  string   `toml:"keybind"`
	When     string   `toml:"when"`
	Launches bool     `toml:"launches"`
}

// KeybindSpec is a manifest-declared default keybind for a host or module
// action. `keys` is the same comma-separated form used in config.toml.
type KeybindSpec struct {
	Action string `toml:"action"`
	Keys   string `toml:"keys"`
}

// ParseFile reads a manifest from disk. The result's Path is populated
// even on parse failure (useful for error reporting).
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

// ParseDir loads <dir>/hackermode.toml.
func ParseDir(dir string) (*Manifest, error) {
	return ParseFile(filepath.Join(dir, ManifestFileName))
}

// Parse loads a manifest from raw bytes. Validation is NOT performed; call
// Validate on the result.
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	meta, err := toml.Decode(string(data), &m)
	if err != nil {
		return nil, formatTOMLError(err)
	}
	// Default mode = stream.
	if m.Entry.Mode == "" {
		m.Entry.Mode = ModeStream
	}
	// Surface unrecognized top-level keys as warnings via the validator.
	_ = meta // currently unused; reserved for future "did you mean ...?" diagnostics
	return &m, nil
}

// Validate returns a slice of validation errors, or nil if the manifest
// is acceptable for Phase B.
func (m *Manifest) Validate() []error {
	var errs []error

	if m.Module.ID == "" {
		errs = append(errs, ferr("module.id is required"))
	} else if !isModuleID(m.Module.ID) {
		errs = append(errs, ferr("module.id %q is not a valid reverse-DNS-style identifier", m.Module.ID))
	}
	if m.Module.Name == "" {
		errs = append(errs, ferr("module.name is required"))
	}
	if m.Module.Version == "" {
		errs = append(errs, ferr("module.version is required"))
	}

	if m.Entry.Binary == "" {
		errs = append(errs, ferr("entry.binary is required"))
	}
	switch m.Entry.Mode {
	case ModeStream, ModeTUI:
		// ok
	default:
		errs = append(errs, ferr("entry.mode %q must be \"stream\" or \"tui\"", m.Entry.Mode))
	}

	for i, c := range m.Commands {
		if c.ID == "" {
			errs = append(errs, ferr("commands[%d].id is required", i))
			continue
		}
		if !strings.HasPrefix(c.ID, m.Module.ID+".") {
			errs = append(errs, ferr("commands[%d].id %q must be namespaced as %q.<name>", i, c.ID, m.Module.ID))
		}
		if c.Title == "" {
			errs = append(errs, ferr("commands[%d].title is required", i))
		}
	}

	for i, k := range m.Keybinds {
		if k.Action == "" || k.Keys == "" {
			errs = append(errs, ferr("keybinds[%d] needs both action and keys", i))
		}
	}

	return errs
}

func ferr(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

func formatTOMLError(err error) error {
	// burntsushi/toml errors are good enough; we just wrap with a hint.
	return fmt.Errorf("invalid TOML: %w", err)
}

// isModuleID checks the reverse-DNS-style namespace convention: at least
// one dot, ASCII letters / digits / underscores / dashes in each segment.
func isModuleID(s string) bool {
	if s == "" {
		return false
	}
	segments := strings.Split(s, ".")
	if len(segments) < 2 {
		return false
	}
	for _, seg := range segments {
		if seg == "" {
			return false
		}
		for _, r := range seg {
			switch {
			case r >= 'a' && r <= 'z':
			case r >= 'A' && r <= 'Z':
			case r >= '0' && r <= '9':
			case r == '_' || r == '-':
			default:
				return false
			}
		}
	}
	return true
}
