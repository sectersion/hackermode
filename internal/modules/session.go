// Package modules holds the per-tab session model and (in later stages of
// Phase B) the module process manager.
//
// A Session is the binding between a tab and whatever drives it. The empty
// "scratch" tab is not a special case: it owns a session with
// Module == "host". When the user invokes a module command from a host
// session, the same tab transitions to the module (same Tab ID, new
// Module). Commands marked as launchers spawn a new tab + session
// instead — that's the host's job, not the session's.
//
// Why a session and not "just the tab"?
//
//   - Lifecycle: tabs become permanent (close → new) but sessions move
//     through spawning/running/idle/suspended/errored. The state machine
//     belongs on the binding, not the tab.
//   - Module process attachment: when Phase 3 lands, each non-host session
//     gets a *Process handle. Tab has no business knowing about that.
//   - Future persistence: Phase E will serialize sessions across restarts;
//     keeping them as their own type means tab UI code stays oblivious.
package modules

import (
	"sync"
	"time"
)

// HostModuleID is the reserved owner ID for sessions not bound to any
// installed module (the empty / scratch tab). Mirrors the namespace used
// by host-owned commands (host.tab.new, host.help.show, ...).
const HostModuleID = "host"

// State enumerates session lifecycle states.
type State int

const (
	StateIdle     State = iota // bound to a module but no work in flight; host sessions live here
	StateSpawning              // process being created
	StateRunning               // module is running and responsive
	StateSuspended             // module is alive but advised it is unfocused
	StateErrored               // module crashed or failed handshake
	StateClosed                // session torn down; do not reuse
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateSpawning:
		return "spawning"
	case StateRunning:
		return "running"
	case StateSuspended:
		return "suspended"
	case StateErrored:
		return "errored"
	case StateClosed:
		return "closed"
	default:
		return "?"
	}
}

// Session ties a tab to the module (or host) currently driving it.
type Session struct {
	mu sync.RWMutex

	// ID is unique within a process lifetime. Equals the owning tab's ID
	// in Phase B; in Phase E sessions may outlive a single tab.
	ID string

	// Module is the owner. "host" for the scratch / empty tab; otherwise
	// a module ID like "acme.email".
	module string

	// State is the lifecycle state. host sessions stay in StateIdle.
	state State

	// Title is the tab label. Modules update via set_title; host updates
	// via launcher / transition.
	title string

	// CreatedAt / UpdatedAt are timestamps for diagnostics and persistence.
	CreatedAt time.Time
	UpdatedAt time.Time

	// Permissions held by the bound module. Empty for host sessions.
	// Strings to avoid pulling in the manifest/capabilities package until
	// Stage 3.1; populated by the module manager when it spawns.
	Permissions []string

	// listeners are notified whenever Module / State / Title change.
	listeners []chan struct{}
}

// NewHostSession returns a session bound to the host (scratch tab) with
// the given ID and title.
func NewHostSession(id, title string) *Session {
	now := time.Now()
	return &Session{
		ID:        id,
		module:    HostModuleID,
		state:     StateIdle,
		title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Module returns the currently-bound module ID.
func (s *Session) Module() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.module
}

// State returns the current lifecycle state.
func (s *Session) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Title returns the current tab title.
func (s *Session) Title() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.title
}

// IsHost reports whether the session is bound to the host (no module).
func (s *Session) IsHost() bool { return s.Module() == HostModuleID }

// SetTitle updates the tab title and notifies listeners.
func (s *Session) SetTitle(title string) {
	s.mu.Lock()
	changed := s.title != title
	s.title = title
	if changed {
		s.UpdatedAt = time.Now()
	}
	s.mu.Unlock()
	if changed {
		s.notify()
	}
}

// SetState transitions the session to a new state. Returns the previous
// state. Transitions to/from StateClosed are one-way.
func (s *Session) SetState(next State) State {
	s.mu.Lock()
	prev := s.state
	if prev == StateClosed && next != StateClosed {
		s.mu.Unlock()
		return prev // closed is terminal
	}
	s.state = next
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
	if prev != next {
		s.notify()
	}
	return prev
}

// Transition rebinds the session to a new module. The tab is unchanged.
// Resets state to StateSpawning (module manager will lift to Running) and
// drops permissions; the caller supplies the new permission set via
// SetPermissions once the module is spawned.
//
// Transitioning back to "host" returns the session to an idle host
// session (clears permissions, state Idle).
func (s *Session) Transition(newModule string) {
	s.mu.Lock()
	if s.state == StateClosed {
		s.mu.Unlock()
		return
	}
	s.module = newModule
	if newModule == HostModuleID {
		s.state = StateIdle
	} else {
		s.state = StateSpawning
	}
	s.Permissions = nil
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
	s.notify()
}

// SetPermissions overwrites the session's capability set. The module
// manager calls this after spawn handshake.
func (s *Session) SetPermissions(caps []string) {
	s.mu.Lock()
	s.Permissions = append([]string(nil), caps...)
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
}

// Close terminates the session. Idempotent; subsequent state changes
// are ignored.
func (s *Session) Close() {
	s.SetState(StateClosed)
}

// Subscribe returns a buffered channel (cap 1) that is signaled when the
// session's module / state / title changes. Drain on each receive.
// Multiple events between drains coalesce into one notification.
func (s *Session) Subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.listeners = append(s.listeners, ch)
	s.mu.Unlock()
	return ch
}

// Unsubscribe removes a previously-subscribed channel.
func (s *Session) Unsubscribe(ch chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.listeners {
		if c == ch {
			s.listeners = append(s.listeners[:i], s.listeners[i+1:]...)
			close(c)
			return
		}
	}
}

func (s *Session) notify() {
	s.mu.RLock()
	subs := append([]chan struct{}(nil), s.listeners...)
	s.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
