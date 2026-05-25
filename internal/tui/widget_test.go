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

// TestPanelWidget_EndToEnd builds examples/clock-widget and verifies its
// SetPanelWidget call surfaces a widget in the side panel.
//
// We drive the model manually rather than spinning a real tea.Program;
// the test's "Send" closure routes inbound RPC-derived messages through
// the same Update path the real loop would.
func TestPanelWidget_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess spawn under -short")
	}
	dir := clockWidgetDir(t)
	buildClockWidget(t, dir)

	var (
		mu     sync.Mutex
		latest Model
	)
	latest = New(config.Default())

	send := func(msg tea.Msg) {
		mu.Lock()
		defer mu.Unlock()
		newM, _ := latest.Update(msg)
		latest = newM.(Model)
	}
	latest.SetRuntime(&Runtime{Send: send})

	send(tea.WindowSizeMsg{Width: 120, Height: 30})

	// Run :dev run synchronously so spawn / RPC handlers are installed
	// against the same model the test inspects.
	mu.Lock()
	newM, _ := latest.dispatchCommand(":dev run " + dir)
	latest = newM
	latest.SetRuntime(&Runtime{Send: send}) // re-attach (Model is value-copied)
	mu.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		view := latest.View()
		mu.Unlock()
		plain := stripANSI(view)
		if strings.Contains(plain, "widgets") && strings.Contains(plain, "now") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	final := stripANSI(latest.View())
	mu.Unlock()
	t.Fatalf("widget did not appear; last view:\n%s", final)
}

func clockWidgetDir(t *testing.T) string {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
	return filepath.Join(repoRoot, "examples", "clock-widget")
}

func buildClockWidget(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", "clock-widget", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build clock-widget: %v\n%s", err, out)
	}
}
