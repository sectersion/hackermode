package tui

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sectersion/hackermode/internal/config"
)

// TestTUIRun_EndToEnd builds the examples/echo-tui binary, points
// :dev run at it via Model.dispatchCommand, then polls the renderer's
// snapshot until the program's title text shows up. Skipped under -short.
func TestTUIRun_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess spawn under -short")
	}
	exampleDir := exampleEchoTUIDir(t)
	buildEchoTUI(t, exampleDir)

	m := New(config.Default())

	var mu sync.Mutex
	send := func(msg tea.Msg) {
		// Drain whatever the model wants to send back; we don't assert on
		// individual messages here.
		mu.Lock()
		defer mu.Unlock()
		_ = msg
	}
	m.SetRuntime(&Runtime{Send: send})

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)

	newM, _ := m.dispatchCommand(":dev run " + exampleDir)
	m = newM

	tab, ok := m.tabs.Active()
	if !ok || tab.Session == nil || tab.Session.IsHost() {
		t.Fatalf("expected tui session bound; tab=%+v", tab)
	}

	// The active tab should now have a renderer.
	r := m.renderers[tab.ID]
	if r == nil {
		t.Fatal("expected renderer for tui-mode tab")
	}

	// Poll the snapshot for the program's title.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap := stripANSI(r.Snapshot())
		if strings.Contains(snap, "echo-tui") {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("never saw 'echo-tui' in snapshot:\n%s", stripANSI(r.Snapshot()))
}

func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			in = true
		case in:
			if (r >= 0x40 && r <= 0x7e) && r != '[' && r != ';' {
				in = false
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func exampleEchoTUIDir(t *testing.T) string {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
	return filepath.Join(repoRoot, "examples", "echo-tui")
}

func buildEchoTUI(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", "echo-tui", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build echo-tui: %v\n%s", err, out)
	}
}
