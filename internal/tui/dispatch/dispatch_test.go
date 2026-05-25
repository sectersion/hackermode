package dispatch

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func TestInvoke_RunsRegisteredHandler(t *testing.T) {
	d := New()
	called := false
	d.Register("host.test", func(c Context) Result {
		called = true
		if c.Source == "" {
			t.Errorf("source should default to programmatic")
		}
		return Result{Consumed: true}
	})
	res := d.Invoke("host.test", Context{})
	if !called {
		t.Fatal("handler was not invoked")
	}
	if !res.Consumed {
		t.Fatal("result should be consumed")
	}
}

func TestInvoke_UnknownActionReturnsZero(t *testing.T) {
	d := New()
	res := d.Invoke("does.not.exist", Context{})
	if res.Consumed {
		t.Fatal("unknown action should not be consumed")
	}
}

func TestHandleKey_FirstMatchWins(t *testing.T) {
	d := New()
	first := false
	second := false
	d.Register("a", func(Context) Result { first = true; return Result{Consumed: true} })
	d.Register("b", func(Context) Result { second = true; return Result{Consumed: true} })
	d.Bind("a", key.NewBinding(key.WithKeys("ctrl+t")))
	d.Bind("b", key.NewBinding(key.WithKeys("ctrl+t"))) // duplicate, should never fire
	_, id := d.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlT})
	if id != "a" {
		t.Fatalf("expected first registered binding to win, got %q", id)
	}
	if !first {
		t.Fatal("first handler not called")
	}
	if second {
		t.Fatal("second handler fired but should not have")
	}
}

func TestHandleKey_NoMatchReturnsEmptyID(t *testing.T) {
	d := New()
	d.Register("a", func(Context) Result { return Result{Consumed: true} })
	d.Bind("a", key.NewBinding(key.WithKeys("ctrl+t")))
	_, id := d.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if id != "" {
		t.Fatalf("expected no match, got %q", id)
	}
}

func TestUnregister_RemovesActionAndBindings(t *testing.T) {
	d := New()
	d.Register("a", func(Context) Result { return Result{Consumed: true} })
	d.Bind("a", key.NewBinding(key.WithKeys("ctrl+t")))
	d.Unregister("a")
	if d.HasAction("a") {
		t.Fatal("action should be gone")
	}
	_, id := d.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlT})
	if id != "" {
		t.Fatalf("binding should be gone, got %q", id)
	}
}

func TestClearBindings_KeepsActions(t *testing.T) {
	d := New()
	d.Register("a", func(Context) Result { return Result{Consumed: true} })
	d.Bind("a", key.NewBinding(key.WithKeys("ctrl+t")))
	d.ClearBindings()
	if !d.HasAction("a") {
		t.Fatal("action should still exist")
	}
	_, id := d.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlT})
	if id != "" {
		t.Fatal("bindings should be cleared")
	}
}

func TestActions_ReturnsSortedSnapshot(t *testing.T) {
	d := New()
	d.Register("c", func(Context) Result { return Result{} })
	d.Register("a", func(Context) Result { return Result{} })
	d.Register("b", func(Context) Result { return Result{} })
	got := d.Actions()
	want := []ActionID{"a", "b", "c"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("expected sorted, got %v", got)
	}
}

func TestRegister_ReplacesPreviousHandler(t *testing.T) {
	d := New()
	first := 0
	second := 0
	d.Register("a", func(Context) Result { first++; return Result{Consumed: true} })
	d.Register("a", func(Context) Result { second++; return Result{Consumed: true} })
	d.Invoke("a", Context{})
	if first != 0 || second != 1 {
		t.Fatalf("expected second handler to run (first=%d second=%d)", first, second)
	}
}
