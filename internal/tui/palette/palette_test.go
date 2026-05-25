package palette

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sectersion/hackermode/internal/tui/theme"
)

func newModel() Model {
	th := theme.Default()
	m := New(th, theme.NewStyles(th))
	m.SetSize(80, 24)
	m.SetCommands([]Command{
		{ID: "tab.new", Title: "New tab", Tags: []string{"tab"}},
		{ID: "tab.close", Title: "Close tab", Tags: []string{"tab"}},
		{ID: "panel.toggle", Title: "Toggle side panel", Hint: "ctrl+b", Tags: []string{"panel"}},
		{ID: "app.quit", Title: "Quit", Tags: []string{"exit"}},
	})
	return m
}

func type_(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestShowHide(t *testing.T) {
	m := newModel()
	if m.Open() {
		t.Fatal("expected closed by default")
	}
	m.Show()
	if !m.Open() {
		t.Fatal("expected open after Show")
	}
	m.Hide()
	if m.Open() {
		t.Fatal("expected closed after Hide")
	}
}

func TestFilter_NarrowsResults(t *testing.T) {
	m := newModel()
	m.Show()
	type_(&m, "tab")
	if len(m.filtered) != 2 {
		t.Fatalf("expected 2 results for 'tab', got %d (%v)", len(m.filtered), m.filtered)
	}
}

func TestFilter_TagsMatch(t *testing.T) {
	m := newModel()
	m.Show()
	type_(&m, "exit")
	if len(m.filtered) != 1 || m.filtered[0].ID != "app.quit" {
		t.Fatalf("expected only app.quit, got %v", m.filtered)
	}
}

func TestEnter_ReturnsSelection(t *testing.T) {
	m := newModel()
	m.Show()
	type_(&m, "panel")
	res, consumed, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !consumed {
		t.Fatal("enter should be consumed")
	}
	if res.Canceled {
		t.Fatal("enter on a match should not be canceled")
	}
	if res.Command.ID != "panel.toggle" {
		t.Fatalf("expected panel.toggle, got %q", res.Command.ID)
	}
	if m.Open() {
		t.Fatal("palette should auto-close after enter")
	}
}

func TestEsc_Cancels(t *testing.T) {
	m := newModel()
	m.Show()
	res, consumed, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !consumed {
		t.Fatal("esc should be consumed while open")
	}
	if !res.Canceled {
		t.Fatal("esc should cancel")
	}
	if m.Open() {
		t.Fatal("esc should close palette")
	}
}

func TestCursorBounds(t *testing.T) {
	m := newModel()
	m.Show()
	// Up at the top should be a no-op.
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Fatalf("cursor should clamp at 0, got %d", m.cursor)
	}
	// Down past the end should clamp at len-1.
	for i := 0; i < 50; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.cursor != len(m.filtered)-1 {
		t.Fatalf("cursor should clamp at end, got %d", m.cursor)
	}
}
