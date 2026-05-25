package tabs

import (
	"strings"
	"testing"

	"github.com/sectersion/hackermode/internal/tui/theme"
)

func newModel() Model {
	th := theme.Default()
	return New(th, theme.NewStyles(th))
}

func TestNew_HasDefaultTab(t *testing.T) {
	m := newModel()
	if m.Count() != 1 {
		t.Fatalf("expected 1 tab, got %d", m.Count())
	}
	tab, ok := m.Active()
	if !ok {
		t.Fatal("expected active tab")
	}
	if tab.Title() != "new" {
		t.Fatalf("expected new, got %q", tab.Title())
	}
}

func TestNew_AttachesHostSession(t *testing.T) {
	m := newModel()
	tab, _ := m.Active()
	if tab.Session == nil {
		t.Fatal("expected attached session")
	}
	if !tab.Session.IsHost() {
		t.Fatal("default session should be host")
	}
}

func TestNew_AppendsAndActivates(t *testing.T) {
	m := newModel()
	a := m.New("a")
	if !strings.HasPrefix(a.ID, "tab-") {
		t.Fatalf("unexpected ID: %q", a.ID)
	}
	if m.Count() != 2 {
		t.Fatalf("expected 2 tabs, got %d", m.Count())
	}
	if m.ActiveIdx() != 1 {
		t.Fatalf("new tab should be active, got idx %d", m.ActiveIdx())
	}
}

func TestClose_AdjustsActive(t *testing.T) {
	m := newModel()
	m.New("a")
	m.New("b")
	if !m.Close() {
		t.Fatal("expected close to succeed")
	}
	if m.Count() != 2 {
		t.Fatalf("expected 2 tabs after close, got %d", m.Count())
	}
	// After closing the last one, active should clamp to the previous.
	if m.ActiveIdx() != 1 {
		t.Fatalf("expected active idx 1, got %d", m.ActiveIdx())
	}
}

func TestClose_LastTabReturnsTrue(t *testing.T) {
	m := newModel()
	if !m.Close() {
		t.Fatal("close should report a closure")
	}
	if m.Count() != 0 {
		t.Fatalf("expected 0 tabs, got %d", m.Count())
	}
	if _, ok := m.Active(); ok {
		t.Fatal("Active() should report false with no tabs")
	}
	// Closing an empty list reports false.
	if m.Close() {
		t.Fatal("close on empty should report false")
	}
}

func TestNextPrev_Wrap(t *testing.T) {
	m := newModel()
	m.New("a")
	m.New("b") // 3 tabs total, active=2
	m.Next()
	if m.ActiveIdx() != 0 {
		t.Fatalf("Next from last should wrap to 0, got %d", m.ActiveIdx())
	}
	m.Prev()
	if m.ActiveIdx() != 2 {
		t.Fatalf("Prev from 0 should wrap to last, got %d", m.ActiveIdx())
	}
}

func TestSetActive_Bounds(t *testing.T) {
	m := newModel()
	m.New("a")
	m.SetActive(99) // out of range — ignored
	if m.ActiveIdx() != 1 {
		t.Fatalf("out-of-range SetActive should be ignored, got %d", m.ActiveIdx())
	}
	m.SetActive(-5)
	if m.ActiveIdx() != 1 {
		t.Fatalf("negative SetActive should be ignored, got %d", m.ActiveIdx())
	}
	m.SetActive(0)
	if m.ActiveIdx() != 0 {
		t.Fatalf("expected idx 0, got %d", m.ActiveIdx())
	}
}

func TestHitTest_MapsColumnToIndex(t *testing.T) {
	m := newModel()
	m.New("longer-title")
	// Tab 0 label is " new "              → width 5
	// Tab 1 label is " longer-title "     → width 14
	if got := m.HitTest(0); got != 0 {
		t.Fatalf("col 0 → tab 0, got %d", got)
	}
	if got := m.HitTest(4); got != 0 {
		t.Fatalf("col 4 → tab 0, got %d", got)
	}
	if got := m.HitTest(5); got != 1 {
		t.Fatalf("col 5 → tab 1, got %d", got)
	}
	if got := m.HitTest(500); got != -1 {
		t.Fatalf("out-of-range col should return -1, got %d", got)
	}
}
