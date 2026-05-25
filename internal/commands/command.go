// Package commands is the host-wide registry of palette / slash / scripted
// commands. Both built-in host commands and modules write into it.
//
// A Command is a stable, namespaced action description with optional
// metadata (hint, tags, default keybind, when-clause). The registry
// deduplicates by ID, evaluates when-clauses to filter visibility for the
// palette, and routes invocations through the dispatch package.
//
// This file defines the data types. Filtering / invocation lives in
// registry.go; when-clause parsing in when.go.
package commands

import (
	"strings"
)

// HostOwner is the reserved owner string for built-in host commands.
// Mirrors modules.HostModuleID; duplicated here so this package doesn't
// import internal/modules.
const HostOwner = "host"

// Command describes a single invocable action.
type Command struct {
	// ID is namespaced by owning module — "host.tab.new", "acme.email.compose".
	ID string

	// Owner is the module ID that registered the command. "host" for built-ins.
	// Used for lazy-spawn routing in Stage 5.
	Owner string

	// Title is the human-readable label shown in the palette.
	Title string

	// Hint is the secondary right-aligned text in the palette (keybind,
	// account name, etc.).
	Hint string

	// Tags are extra search tokens that augment fuzzy matching beyond Title.
	Tags []string

	// Keybind is the module-suggested default keybind, in keymap.go spec
	// form ("ctrl+alt+m"). User config can override. Empty means no default.
	Keybind string

	// When is the visibility expression source. Empty == "always".
	// Parsed lazily on first evaluation.
	When string

	// Launches is true for commands that should open a new tab + session
	// when invoked, instead of transitioning the current session. Useful
	// for "Open X" entries.
	Launches bool

	// parsed when-clause cache, lazily filled.
	parsedWhen *whenExpr
}

// Namespace returns the prefix before the first dot in the ID (the owning
// module). Returns "" for malformed IDs without a dot.
func (c Command) Namespace() string {
	if i := strings.IndexByte(c.ID, '.'); i > 0 {
		return c.ID[:i]
	}
	return ""
}
