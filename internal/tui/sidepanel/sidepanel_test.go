package sidepanel

import (
	"strings"
	"testing"
	"time"

	"github.com/sectersion/hackermode/internal/tui/theme"
)

func newModel(visible bool) Model {
	th := theme.Default()
	m := New(th, theme.NewStyles(th), visible)
	m.SetSize(20, 10)
	return m
}

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

func TestNew_DefaultsToVisibilityArg(t *testing.T) {
	if !newModel(true).Visible() {
		t.Fatal("expected visible")
	}
	if newModel(false).Visible() {
		t.Fatal("expected hidden")
	}
}

func TestToggle_FlipsVisibility(t *testing.T) {
	m := newModel(true)
	m.Toggle()
	if m.Visible() {
		t.Fatal("toggle should hide")
	}
	m.Toggle()
	if !m.Visible() {
		t.Fatal("toggle should show again")
	}
}

func TestView_HiddenIsEmpty(t *testing.T) {
	m := newModel(false)
	if m.View() != "" {
		t.Fatal("hidden panel should render empty")
	}
}

func TestView_ZeroSizeIsEmpty(t *testing.T) {
	m := newModel(true)
	m.SetSize(0, 0)
	if m.View() != "" {
		t.Fatal("zero-size panel should render empty")
	}
}

func TestView_ContainsSectionTitles(t *testing.T) {
	m := newModel(true)
	got := strip(m.View())
	if !strings.Contains(got, "modules") {
		t.Fatalf("expected 'modules' title in view:\n%s", got)
	}
	if !strings.Contains(got, "notifications") {
		t.Fatalf("expected 'notifications' title in view:\n%s", got)
	}
}

func TestView_EmptyStatesShowMutedPlaceholder(t *testing.T) {
	m := newModel(true)
	got := strip(m.View())
	if !strings.Contains(got, "none installed") {
		t.Fatalf("expected 'none installed' placeholder:\n%s", got)
	}
}

func TestAddNotification_NewestFirst(t *testing.T) {
	m := newModel(true)
	m.AddNotification(Notification{Title: "alpha"})
	m.AddNotification(Notification{Title: "beta"})
	got := strip(m.View())

	alphaIdx := strings.Index(got, "alpha")
	betaIdx := strings.Index(got, "beta")
	if alphaIdx < 0 || betaIdx < 0 {
		t.Fatalf("expected both notifications, got:\n%s", got)
	}
	if betaIdx >= alphaIdx {
		t.Fatalf("expected newest first (beta before alpha), got:\n%s", got)
	}
}

func TestAddNotification_TimestampFallback(t *testing.T) {
	m := newModel(true)
	m.AddNotification(Notification{Title: "x"}) // zero time → should default to "now"
	// Walk back to the latest notif via View; just ensure no panic + content present.
	if !strings.Contains(strip(m.View()), "x") {
		t.Fatal("notification missing")
	}
	// Sanity check: the slice should hold the zero-time placeholder filled in.
	if m.notifs[0].Time.Before(time.Now().Add(-time.Hour)) {
		t.Fatal("zero-valued notification time should be replaced with current time")
	}
}

func TestAddNotification_CapsAt20(t *testing.T) {
	m := newModel(true)
	for i := 0; i < 30; i++ {
		m.AddNotification(Notification{Title: "n"})
	}
	if len(m.notifs) != 20 {
		t.Fatalf("expected notification cap of 20, got %d", len(m.notifs))
	}
}
