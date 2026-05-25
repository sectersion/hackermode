package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sectersion/hackermode/internal/config"
)

// Smoke test: model handles a window-size message, renders without panic,
// and produces non-empty output containing the default tab label.
func TestModel_RenderAfterResize(t *testing.T) {
	m := New(config.Default())

	// Simulate initial resize.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	mm := updated.(Model)

	view := mm.View()
	if view == "" {
		t.Fatal("empty view after resize")
	}
	if !strings.Contains(view, "new") {
		t.Fatalf("view missing default tab label:\n%s", view)
	}
}

// First ctrl+c opens the quit confirm dialog; a second ctrl+c quits.
func TestModel_QuitConfirmFlow(t *testing.T) {
	m := New(config.Default())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	mm := updated.(Model)

	// First press: opens the dialog, no quit yet.
	updated, cmd := mm.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	mm = updated.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, ok := msg.(tea.QuitMsg); ok {
				t.Fatal("first ctrl+c should not quit")
			}
		}
	}
	if !mm.quit.Open() {
		t.Fatal("first ctrl+c should open the quit dialog")
	}

	// Second press: quit.
	_, cmd = mm.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit cmd on second ctrl+c")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected QuitMsg from second ctrl+c")
	}
}

// Pressing y while the dialog is open quits.
func TestModel_QuitDialogYes(t *testing.T) {
	m := New(config.Default())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	mm := updated.(Model)

	updated, _ = mm.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	mm = updated.(Model)
	if !mm.quit.Open() {
		t.Fatal("expected dialog open")
	}
	_, cmd := mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("expected quit cmd from 'y'")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected QuitMsg")
	}
}

// Esc cancels the dialog without quitting.
func TestModel_QuitDialogCancel(t *testing.T) {
	m := New(config.Default())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	mm := updated.(Model)

	updated, _ = mm.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	mm = updated.(Model)

	updated, cmd := mm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mm = updated.(Model)
	if mm.quit.Open() {
		t.Fatal("esc should close the dialog")
	}
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("esc should not quit")
		}
	}
}

// Enter on the default selection ("Nope") cancels rather than quits.
func TestModel_QuitDialogEnterDefaultsToNope(t *testing.T) {
	m := New(config.Default())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	mm := updated.(Model)

	updated, _ = mm.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	mm = updated.(Model)

	updated, cmd := mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm = updated.(Model)
	if mm.quit.Open() {
		t.Fatal("enter should close the dialog")
	}
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("enter on default Nope should not quit")
		}
	}
}

// New tab shortcut increases tab count.
func TestModel_NewTab(t *testing.T) {
	m := New(config.Default())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	mm := updated.(Model)
	before := mm.tabs.Count()

	updated2, _ := mm.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	mm2 := updated2.(Model)
	if mm2.tabs.Count() != before+1 {
		t.Fatalf("expected %d tabs, got %d", before+1, mm2.tabs.Count())
	}
}

// Toggle panel hides/shows.
func TestModel_TogglePanel(t *testing.T) {
	m := New(config.Default())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	mm := updated.(Model)
	visible := mm.panel.Visible()

	updated2, _ := mm.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	mm2 := updated2.(Model)
	if mm2.panel.Visible() == visible {
		t.Fatal("panel visibility did not toggle")
	}
}
