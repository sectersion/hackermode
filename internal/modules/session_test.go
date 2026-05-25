package modules

import (
	"testing"
)

func TestNewHostSession(t *testing.T) {
	s := NewHostSession("tab-1", "new")
	if s.ID != "tab-1" {
		t.Fatalf("id mismatch: %q", s.ID)
	}
	if !s.IsHost() {
		t.Fatal("new host session should report IsHost")
	}
	if s.State() != StateIdle {
		t.Fatalf("expected idle, got %v", s.State())
	}
	if s.Title() != "new" {
		t.Fatalf("title: %q", s.Title())
	}
}

func TestTransition_BindsAndSpawns(t *testing.T) {
	s := NewHostSession("tab-1", "new")
	s.Transition("acme.email")
	if s.IsHost() {
		t.Fatal("should no longer be host session")
	}
	if s.Module() != "acme.email" {
		t.Fatalf("module: %q", s.Module())
	}
	if s.State() != StateSpawning {
		t.Fatalf("expected spawning after transition, got %v", s.State())
	}
}

func TestTransition_BackToHostResetsState(t *testing.T) {
	s := NewHostSession("tab-1", "new")
	s.Transition("acme.email")
	s.SetState(StateRunning)
	s.SetPermissions([]string{"network"})

	s.Transition(HostModuleID)
	if !s.IsHost() {
		t.Fatal("should be host again")
	}
	if s.State() != StateIdle {
		t.Fatalf("expected idle, got %v", s.State())
	}
	if len(s.Permissions) != 0 {
		t.Fatalf("permissions should clear, got %v", s.Permissions)
	}
}

func TestSetState_ClosedIsTerminal(t *testing.T) {
	s := NewHostSession("tab-1", "new")
	s.Close()
	s.SetState(StateRunning) // ignored
	if s.State() != StateClosed {
		t.Fatalf("closed should stick, got %v", s.State())
	}
}

func TestTransition_OnClosedIsNoOp(t *testing.T) {
	s := NewHostSession("tab-1", "new")
	s.Close()
	s.Transition("acme.email")
	if s.Module() != HostModuleID {
		t.Fatalf("transition after close should be ignored, got module %q", s.Module())
	}
}

func TestSubscribe_FiresOnChanges(t *testing.T) {
	s := NewHostSession("tab-1", "new")
	ch := s.Subscribe()
	defer s.Unsubscribe(ch)

	s.SetTitle("title")
	select {
	case <-ch:
	default:
		t.Fatal("expected title-change notification")
	}

	s.Transition("acme.email")
	select {
	case <-ch:
	default:
		t.Fatal("expected transition notification")
	}

	s.SetState(StateRunning)
	select {
	case <-ch:
	default:
		t.Fatal("expected state-change notification")
	}
}

func TestSubscribe_CoalescesPendingEvents(t *testing.T) {
	s := NewHostSession("tab-1", "new")
	ch := s.Subscribe()
	defer s.Unsubscribe(ch)

	// Two changes before reading should coalesce into one pending event.
	s.SetTitle("a")
	s.SetTitle("b")
	<-ch
	select {
	case <-ch:
		t.Fatal("should be empty after coalesced drain")
	default:
	}
}

func TestSetTitle_NoEventIfUnchanged(t *testing.T) {
	s := NewHostSession("tab-1", "same")
	ch := s.Subscribe()
	defer s.Unsubscribe(ch)
	s.SetTitle("same")
	select {
	case <-ch:
		t.Fatal("expected no-op for identical title")
	default:
	}
}

func TestSetState_PrevReturned(t *testing.T) {
	s := NewHostSession("tab-1", "new")
	prev := s.SetState(StateRunning)
	if prev != StateIdle {
		t.Fatalf("expected idle as prev, got %v", prev)
	}
}

func TestStateString(t *testing.T) {
	cases := map[State]string{
		StateIdle:      "idle",
		StateSpawning:  "spawning",
		StateRunning:   "running",
		StateSuspended: "suspended",
		StateErrored:   "errored",
		StateClosed:    "closed",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("%d → %q, want %q", s, got, want)
		}
	}
}
