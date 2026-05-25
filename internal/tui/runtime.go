// Package tui — runtime: a stable side-channel for parts of the model that
// must outlive a single Update tick.
//
// The Model is value-typed (Bubble Tea pattern), but we need a few things
// that genuinely live longer than one cycle:
//
//   - send: tea.Program.Send, captured at startup. Goroutines (e.g. the
//     module stdout pump) inject messages back into the event loop via
//     this closure. Storing it on Model directly is awkward because
//     Update copies; pointing at a stable Runtime struct sidesteps that.
//
// More fields may join later (a tea.Cmd queue, telemetry counters).
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Runtime is the shared side-channel.
type Runtime struct {
	// Send injects a message into the bubbletea event loop. Set by the
	// launcher after constructing the program; may be nil before Run.
	Send func(tea.Msg)
}

// AutoRunMsg asks the model to immediately run a module from a directory
// against the active tab. The launcher uses this to implement
// `hackermode dev run <dir>` so the user lands straight in a module
// session.
type AutoRunMsg struct {
	Dir string
}
