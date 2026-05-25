package statusline

import (
	"strings"
	"testing"

	"github.com/sectersion/hackermode/internal/tui/keymap"
	"github.com/sectersion/hackermode/internal/tui/theme"
)

// strip removes ANSI escapes for plain-text assertions.
func strip(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEsc = true
		case inEsc:
			if (r >= 0x40 && r <= 0x7e) && r != '[' && r != ';' {
				inEsc = false
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func defaultKeys() keymap.Map {
	return keymap.New(map[string]string{
		keymap.Quit:           "ctrl+c",
		keymap.Submit:         "enter",
		keymap.Complete:       "tab",
		keymap.HistoryPrev:    "up",
		keymap.HistoryNext:    "down",
		keymap.NewTab:         "ctrl+t",
		keymap.CloseTab:       "ctrl+w",
		keymap.NextTab:        "ctrl+tab",
		keymap.PrevTab:        "ctrl+shift+tab",
		keymap.TogglePanel:    "ctrl+b",
		keymap.CommandPalette: "ctrl+p",
		keymap.FocusInput:     "esc",
		keymap.ScrollUp:       "pgup",
		keymap.ScrollDown:     "pgdn",
	})
}

func newModel() Model {
	th := theme.Default()
	m := New(defaultKeys(), th, theme.NewStyles(th))
	m.SetWidth(120)
	return m
}

func TestView_FocusInputShowsSubmitAndPalette(t *testing.T) {
	m := newModel()
	m.SetFocus(FocusInput)
	got := strip(m.View())
	for _, want := range []string{"enter", "ctrl+p", "ctrl+t", "ctrl+c"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in focus=input statusline, got:\n%s", want, got)
		}
	}
}

func TestView_FocusOutputShowsScrollHints(t *testing.T) {
	m := newModel()
	m.SetFocus(FocusOutput)
	got := strip(m.View())
	if !strings.Contains(got, "pgup") {
		t.Fatalf("expected scroll hint in focus=output, got:\n%s", got)
	}
}

func TestView_AppendsExtraHints(t *testing.T) {
	m := newModel()
	m.SetExtraHints([]Hint{{Key: "?", Desc: "module help"}})
	got := strip(m.View())
	if !strings.Contains(got, "module help") {
		t.Fatalf("expected extra hint description, got:\n%s", got)
	}
}

func TestView_RightAlignsMessage(t *testing.T) {
	m := newModel()
	m.SetFocus(FocusInput)
	m.SetMessage("status-msg")
	got := strip(m.View())
	if !strings.Contains(got, "status-msg") {
		t.Fatalf("expected message in view, got:\n%s", got)
	}
	// The message should appear after the hints — check it lives in the
	// rightmost half of the line.
	idx := strings.Index(got, "status-msg")
	if idx*2 < len(got) {
		t.Fatalf("expected message right-aligned, got idx %d of line len %d", idx, len(got))
	}
}

func TestView_NarrowWidthDoesNotPanic(t *testing.T) {
	m := newModel()
	m.SetWidth(5)
	m.SetFocus(FocusInput)
	_ = m.View()
}
