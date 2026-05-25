package input

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sectersion/hackermode/internal/tui/theme"
)

func newModel() Model {
	th := theme.Default()
	return New(th, theme.NewStyles(th))
}

// typing one rune at a time mimics what bubbletea actually sends.
func typeString(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestSubmit_PushesHistoryAndClears(t *testing.T) {
	m := newModel()
	typeString(&m, "hello")
	got := m.Submit()
	if got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
	if m.Value() != "" {
		t.Fatalf("expected reset, got %q", m.Value())
	}
}

func TestSubmit_DedupesConsecutive(t *testing.T) {
	m := newModel()
	typeString(&m, "abc")
	m.Submit()
	typeString(&m, "abc")
	m.Submit()
	// Walk back through history; we should only land on "abc" once.
	m.HistoryPrev()
	if m.Value() != "abc" {
		t.Fatalf("expected 'abc', got %q", m.Value())
	}
	m.HistoryPrev()
	if m.Value() != "abc" {
		t.Fatalf("expected to stay on only entry, got %q", m.Value())
	}
}

func TestHistory_PrevNextRoundTrip(t *testing.T) {
	m := newModel()
	typeString(&m, "one")
	m.Submit()
	typeString(&m, "two")
	m.Submit()
	typeString(&m, "draft")

	m.HistoryPrev() // "two"
	if m.Value() != "two" {
		t.Fatalf("expected 'two', got %q", m.Value())
	}
	m.HistoryPrev() // "one"
	if m.Value() != "one" {
		t.Fatalf("expected 'one', got %q", m.Value())
	}
	m.HistoryNext() // "two"
	if m.Value() != "two" {
		t.Fatalf("expected 'two', got %q", m.Value())
	}
	m.HistoryNext() // back to draft
	if m.Value() != "draft" {
		t.Fatalf("expected draft restored, got %q", m.Value())
	}
}

func TestComplete_AcceptsUniqueMatch(t *testing.T) {
	m := newModel()
	typeString(&m, ":hel")
	suggs := m.Complete()
	if suggs != nil {
		t.Fatalf("expected unique completion (no suggestions returned), got %v", suggs)
	}
	if m.Value() != ":help" {
		t.Fatalf("expected ':help', got %q", m.Value())
	}
}

func TestComplete_ReturnsSuggestionsWhenAmbiguous(t *testing.T) {
	m := newModel()
	typeString(&m, ":")
	suggs := m.Complete()
	if len(suggs) < 2 {
		t.Fatalf("expected multiple suggestions, got %d", len(suggs))
	}
}

func TestSetCompleter_Overrides(t *testing.T) {
	m := newModel()
	called := false
	m.SetCompleter(func(text string, cursor int) []string {
		called = true
		return []string{"override"}
	})
	typeString(&m, "x")
	m.Complete()
	if !called {
		t.Fatal("custom completer was not invoked")
	}
	if m.Value() != "override" {
		t.Fatalf("expected completer result applied, got %q", m.Value())
	}
}
