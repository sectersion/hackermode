package commands

import (
	"testing"
)

func TestRegister_InfersOwnerFromNamespace(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Command{ID: "email.compose", Title: "Compose"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	c, ok := r.Get("email.compose")
	if !ok {
		t.Fatal("not found")
	}
	if c.Owner != "email" {
		t.Fatalf("expected owner 'email', got %q", c.Owner)
	}
}

func TestRegister_DuplicateDifferentOwnerRejected(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(Command{ID: "x.do", Owner: "a", Title: "T"})
	err := r.Register(Command{ID: "x.do", Owner: "b", Title: "T"})
	if err != ErrDuplicate {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestRegister_SameOwnerUpdatesEntry(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(Command{ID: "x.do", Owner: "a", Title: "old"})
	_ = r.Register(Command{ID: "x.do", Owner: "a", Title: "new"})
	c, _ := r.Get("x.do")
	if c.Title != "new" {
		t.Fatalf("expected title to update, got %q", c.Title)
	}
}

func TestUnregister_RemovesEntry(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(Command{ID: "x.do", Title: "T"})
	r.Unregister("x.do")
	if _, ok := r.Get("x.do"); ok {
		t.Fatal("expected removed")
	}
}

func TestUnregisterOwner_DropsAll(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(Command{ID: "a.one", Title: "T"})
	_ = r.Register(Command{ID: "a.two", Title: "T"})
	_ = r.Register(Command{ID: "b.one", Title: "T"})
	dropped := r.UnregisterOwner("a")
	if dropped != 2 {
		t.Fatalf("expected 2 dropped, got %d", dropped)
	}
	if len(r.All()) != 1 {
		t.Fatalf("expected 1 remaining, got %d", len(r.All()))
	}
}

func TestVisible_FiltersByWhen(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(Command{ID: "host.help", Title: "Help"})
	_ = r.Register(Command{ID: "email.compose", Title: "Compose",
		When: `tab.module == "email"`})
	_ = r.Register(Command{ID: "email.list", Title: "List",
		When: `tab.module == "email"`})

	visible := r.Visible(Context{"tab.module": "host"})
	if len(visible) != 1 || visible[0].ID != "host.help" {
		t.Fatalf("expected only host.help, got %+v", visible)
	}

	visible = r.Visible(Context{"tab.module": "email"})
	if len(visible) != 3 {
		t.Fatalf("expected all 3, got %+v", visible)
	}
}

func TestVisible_SortedByOwnerThenTitle(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(Command{ID: "b.zeta", Title: "zeta"})
	_ = r.Register(Command{ID: "a.beta", Title: "beta"})
	_ = r.Register(Command{ID: "a.alpha", Title: "alpha"})

	got := r.All()
	if got[0].ID != "a.alpha" || got[1].ID != "a.beta" || got[2].ID != "b.zeta" {
		t.Fatalf("unexpected order: %+v", got)
	}
}

func TestSubscribe_FiresOnChange(t *testing.T) {
	r := NewRegistry()
	ch := r.Subscribe()
	defer r.Unsubscribe(ch)

	_ = r.Register(Command{ID: "x.do"})
	select {
	case <-ch:
	default:
		t.Fatal("expected change notification after Register")
	}

	r.Unregister("x.do")
	select {
	case <-ch:
	default:
		t.Fatal("expected change notification after Unregister")
	}
}

func TestSubscribe_NoEventOnMissingUnregister(t *testing.T) {
	r := NewRegistry()
	ch := r.Subscribe()
	defer r.Unsubscribe(ch)
	r.Unregister("missing")
	select {
	case <-ch:
		t.Fatal("expected no notification for missing unregister")
	default:
	}
}
