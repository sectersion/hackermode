package keymap

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/key"
)

func TestBinding_MatchesSingleKey(t *testing.T) {
	m := New(map[string]string{Quit: "ctrl+c"})
	b := m.Binding(Quit)
	if !key.Matches(tea.KeyMsg{Type: tea.KeyCtrlC}, b) {
		t.Fatal("ctrl+c should match Quit")
	}
}

func TestBinding_MatchesAnyOfList(t *testing.T) {
	m := New(map[string]string{CommandPalette: "ctrl+p,ctrl+k"})
	b := m.Binding(CommandPalette)
	if !key.Matches(tea.KeyMsg{Type: tea.KeyCtrlP}, b) {
		t.Fatal("ctrl+p should match")
	}
	if !key.Matches(tea.KeyMsg{Type: tea.KeyCtrlK}, b) {
		t.Fatal("ctrl+k should also match")
	}
}

func TestBinding_UnknownActionDisabled(t *testing.T) {
	m := New(map[string]string{})
	b := m.Binding("nonexistent")
	if b.Enabled() {
		t.Fatal("unknown action should return a disabled binding")
	}
}

func TestHelp_ReportsFirstKeyAndDescription(t *testing.T) {
	m := New(map[string]string{Submit: "enter"})
	k, d := m.Help(Submit)
	if k != "enter" {
		t.Fatalf("expected 'enter', got %q", k)
	}
	if d == "" {
		t.Fatal("expected non-empty description for Submit")
	}
}

func TestSplit_HandlesWhitespace(t *testing.T) {
	m := New(map[string]string{NewTab: "ctrl+t, alt+t"})
	b := m.Binding(NewTab)
	if !key.Matches(tea.KeyMsg{Type: tea.KeyCtrlT}, b) {
		t.Fatal("ctrl+t should match despite space in spec")
	}
}

func TestEmptySpec_SkipsBinding(t *testing.T) {
	m := New(map[string]string{Quit: ""})
	if m.Binding(Quit).Enabled() {
		t.Fatal("empty spec should leave action unbound")
	}
}
