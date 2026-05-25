package quitdialog

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sectersion/hackermode/internal/tui/theme"
)

func newModel() Model {
	th := theme.Default()
	m := New(th, theme.NewStyles(th))
	m.SetSize(80, 24)
	return m
}

func TestShowResetsToNope(t *testing.T) {
	m := newModel()
	m.Show()
	if !m.Open() {
		t.Fatal("expected open")
	}
	if !m.noSel {
		t.Fatal("expected Nope to be the default selection")
	}
}

func TestUpdate_CtrlCQuits(t *testing.T) {
	m := newModel()
	m.Show()
	res := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !res.Quit {
		t.Fatal("ctrl+c should quit immediately")
	}
	if m.Open() {
		t.Fatal("dialog should close after ctrl+c")
	}
}

func TestUpdate_EscCancels(t *testing.T) {
	m := newModel()
	m.Show()
	res := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !res.Cancel {
		t.Fatal("esc should cancel")
	}
	if m.Open() {
		t.Fatal("dialog should close after esc")
	}
}

func TestUpdate_YQuits(t *testing.T) {
	m := newModel()
	m.Show()
	res := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !res.Quit {
		t.Fatal("y should quit")
	}
}

func TestUpdate_NCancels(t *testing.T) {
	m := newModel()
	m.Show()
	res := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !res.Cancel {
		t.Fatal("n should cancel")
	}
}

func TestUpdate_LeftRightToggles(t *testing.T) {
	m := newModel()
	m.Show()
	before := m.noSel
	res := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if res.Quit || res.Cancel {
		t.Fatal("arrow should not terminate")
	}
	if m.noSel == before {
		t.Fatal("arrow should toggle selection")
	}
}

func TestUpdate_EnterOnDefaultCancels(t *testing.T) {
	m := newModel()
	m.Show()
	// Default is noSel = true → enter cancels.
	res := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !res.Cancel {
		t.Fatal("enter on default should cancel")
	}
}

func TestUpdate_EnterAfterToggleQuits(t *testing.T) {
	m := newModel()
	m.Show()
	m.Update(tea.KeyMsg{Type: tea.KeyRight}) // flip to Yep
	res := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !res.Quit {
		t.Fatal("enter after toggling to Yep should quit")
	}
}

func TestView_HiddenIsEmpty(t *testing.T) {
	m := newModel()
	if m.View() != "" {
		t.Fatal("hidden dialog should render empty")
	}
}

func TestView_ContainsQuestion(t *testing.T) {
	m := newModel()
	m.Show()
	out := m.View()
	if out == "" {
		t.Fatal("expected view content")
	}
	if !strings.Contains(out, "Are you sure") {
		t.Fatalf("expected question in view, got:\n%s", out)
	}
}
