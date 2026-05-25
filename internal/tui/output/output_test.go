package output

import (
	"strings"
	"testing"

	"github.com/sectersion/hackermode/internal/tui/theme"
)

func newModel() Model {
	th := theme.Default()
	m := New(theme.NewStyles(th))
	m.SetSize(40, 10)
	return m
}

func TestAppend_BuffersPerTab(t *testing.T) {
	m := newModel()
	m.SetActive("tab-a")
	m.Append("tab-a", "hello\n")
	m.Append("tab-b", "different\n") // not active

	if !strings.Contains(m.View(), "hello") {
		t.Fatalf("expected tab-a content visible:\n%s", m.View())
	}
	if strings.Contains(m.View(), "different") {
		t.Fatalf("tab-b content leaked into active view:\n%s", m.View())
	}
	m.SetActive("tab-b")
	if !strings.Contains(m.View(), "different") {
		t.Fatalf("expected tab-b content after switch:\n%s", m.View())
	}
}

func TestForget_DropsBuffer(t *testing.T) {
	m := newModel()
	m.SetActive("doomed")
	m.Append("doomed", "x")
	m.Forget("doomed")
	// Re-activating gives a fresh empty buffer rather than restoring old content.
	m.SetActive("doomed")
	if strings.Contains(m.View(), "x") {
		t.Fatalf("forgotten content reappeared:\n%s", m.View())
	}
}

func TestAppendln_AddsNewline(t *testing.T) {
	m := newModel()
	m.SetActive("t")
	m.Appendln("t", "line")
	if !strings.Contains(m.View(), "line") {
		t.Fatalf("missing line:\n%s", m.View())
	}
}
