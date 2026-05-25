// Package commands — registry: thread-safe Command store with visibility
// filtering and event hooks for the palette / slash-command surfaces.
//
// The registry deliberately does not invoke commands itself. Invocation
// is the dispatcher's job (internal/tui/dispatch). The registry owns:
//
//   - storage and dedup by Command.ID
//   - lookup by ID
//   - filtered listing (visible-for-context) sorted by Owner then Title
//   - change notification so the palette can refresh when a module
//     registers / updates commands at runtime
//
// Conflict policy: registering an ID that already exists with a different
// Owner is rejected (returns ErrDuplicate). The same Owner re-registering
// the same ID replaces the existing entry — modules can update titles,
// hints, and when-clauses dynamically.
package commands

import (
	"errors"
	"sort"
	"sync"

	hlog "github.com/sectersion/hackermode/internal/log"
)

// ErrDuplicate is returned by Register when a command with that ID is
// already owned by a different module.
var ErrDuplicate = errors.New("command ID already registered by a different owner")

// Registry is the central command store.
type Registry struct {
	mu        sync.RWMutex
	byID      map[string]*Command
	listeners []chan struct{}
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]*Command)}
}

// Register adds or replaces a command. Returns ErrDuplicate only when a
// command with the same ID already exists and is owned by a different
// module.
func (r *Registry) Register(c Command) error {
	if c.ID == "" {
		return errors.New("commands: ID required")
	}
	if c.Owner == "" {
		c.Owner = c.Namespace()
		if c.Owner == "" {
			c.Owner = "host" // fallback for un-namespaced legacy IDs
		}
	}

	r.mu.Lock()
	if existing, ok := r.byID[c.ID]; ok && existing.Owner != c.Owner {
		r.mu.Unlock()
		hlog.Warn("command duplicate", "id", c.ID, "existing_owner", existing.Owner, "new_owner", c.Owner)
		return ErrDuplicate
	}
	parsed := parseWhen(c.When)
	if parsed.op == whenInvalid && c.When != "" {
		hlog.Warn("command when-clause unparseable; commands will be hidden",
			"id", c.ID, "when", c.When)
	}
	c.parsedWhen = &parsed
	r.byID[c.ID] = &c
	r.mu.Unlock()
	r.notify()
	return nil
}

// Unregister removes a command by ID. Missing IDs are a no-op.
func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	if _, ok := r.byID[id]; ok {
		delete(r.byID, id)
		r.mu.Unlock()
		r.notify()
		return
	}
	r.mu.Unlock()
}

// UnregisterOwner removes every command owned by the given module ID. Used
// when a module is shut down.
func (r *Registry) UnregisterOwner(owner string) int {
	r.mu.Lock()
	dropped := 0
	for id, c := range r.byID {
		if c.Owner == owner {
			delete(r.byID, id)
			dropped++
		}
	}
	r.mu.Unlock()
	if dropped > 0 {
		r.notify()
	}
	return dropped
}

// Get returns the command for an ID (zero value + false if missing).
func (r *Registry) Get(id string) (Command, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.byID[id]
	if !ok {
		return Command{}, false
	}
	return *c, true
}

// All returns every command, sorted by (owner, title).
func (r *Registry) All() []Command {
	r.mu.RLock()
	out := make([]Command, 0, len(r.byID))
	for _, c := range r.byID {
		out = append(out, *c)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Owner != out[j].Owner {
			return out[i].Owner < out[j].Owner
		}
		return out[i].Title < out[j].Title
	})
	return out
}

// Visible returns the subset of All() whose when-clauses evaluate to true
// in ctx. Unparseable when-clauses are treated as false. The result is
// sorted the same way as All().
func (r *Registry) Visible(ctx Context) []Command {
	r.mu.RLock()
	out := make([]Command, 0, len(r.byID))
	for _, c := range r.byID {
		if c.parsedWhen == nil {
			parsed := parseWhen(c.When)
			c.parsedWhen = &parsed
		}
		if !c.parsedWhen.eval(ctx) {
			continue
		}
		out = append(out, *c)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Owner != out[j].Owner {
			return out[i].Owner < out[j].Owner
		}
		return out[i].Title < out[j].Title
	})
	return out
}

// Subscribe returns a channel that receives a value whenever the registry
// changes. The channel is buffered (1); subsequent change events are
// coalesced into a single pending notification. Callers should drain on
// each receive. Close the returned channel via Unsubscribe.
func (r *Registry) Subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	r.mu.Lock()
	r.listeners = append(r.listeners, ch)
	r.mu.Unlock()
	return ch
}

// Unsubscribe removes a previously-subscribed channel.
func (r *Registry) Unsubscribe(ch chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, c := range r.listeners {
		if c == ch {
			r.listeners = append(r.listeners[:i], r.listeners[i+1:]...)
			close(c)
			return
		}
	}
}

func (r *Registry) notify() {
	r.mu.RLock()
	subs := append([]chan struct{}(nil), r.listeners...)
	r.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default:
			// already has a pending event; coalesce.
		}
	}
}
