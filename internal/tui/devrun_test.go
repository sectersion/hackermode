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
	"github.com/sectersion/hackermode/internal/tui/streambridge"
)

// TestDevRun_EndToEnd builds the examples/echo-stream binary, points
// :dev run at it via Model.dispatchCommand, then feeds "hello" via the
// input box and verifies that an "echo: hello" ChunkMsg shows up.
// Skipped under -short.
func TestDevRun_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess spawn under -short")
	}
	exampleDir := exampleEchoStreamDir(t)
	buildEchoStream(t, exampleDir)

	m := New(config.Default())

	// Capture messages the model would send back to the program. The
	// streambridge goroutine calls this with ChunkMsg/ClosedMsg.
	var (
		mu      sync.Mutex
		chunks  [][]byte
		closedC = make(chan string, 1)
	)
	send := func(msg tea.Msg) {
		switch x := msg.(type) {
		case streambridge.ChunkMsg:
			mu.Lock()
			chunks = append(chunks, x.Data)
			mu.Unlock()
		case streambridge.ClosedMsg:
			select {
			case closedC <- x.TabID:
			default:
			}
		}
	}
	m.SetRuntime(&Runtime{Send: send})
	// Window size message — needed so Update doesn't no-op the size.
	if mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30}); mm != nil {
		m = mm.(Model)
	}

	// Start the module.
	newM, _ := m.dispatchCommand(":dev run " + exampleDir)
	m = newM
	tab, ok := m.tabs.Active()
	if !ok {
		t.Fatal("expected active tab")
	}
	if tab.Session.IsHost() {
		t.Fatal("expected session transitioned to module")
	}

	// Wait for the module banner ("echo-stream ready ...") to arrive.
	if !waitForChunk(t, &mu, &chunks, "echo-stream ready", 3*time.Second) {
		t.Fatalf("did not receive banner chunk; got: %s", joinChunks(&mu, &chunks))
	}

	// Send a line to the module's stdin via the input box path.
	proc := m.manager.Get(tab.Session.Module(), tab.ID)
	if proc == nil {
		t.Fatalf("expected manager to track process for module=%q tab=%q; session state=%v; tracked=%d",
			tab.Session.Module(), tab.ID, tab.Session.State(), len(m.manager.All()))
	}
	if _, err := proc.Stdin().Write([]byte("hello world\n")); err != nil {
		t.Fatalf("write stdin: %v", err)
	}

	if !waitForChunk(t, &mu, &chunks, "echo: hello world", 3*time.Second) {
		t.Fatalf("did not receive echo chunk; got: %s", joinChunks(&mu, &chunks))
	}

	// Tear down: close stdin so the helper EOFs and exits.
	_ = proc.Stdin().Close()
	select {
	case <-closedC:
	case <-time.After(3 * time.Second):
		t.Fatal("module did not emit ClosedMsg after stdin close")
	}
}

func waitForChunk(t *testing.T, mu *sync.Mutex, chunks *[][]byte, needle string, dl time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(dl)
	for time.Now().Before(deadline) {
		mu.Lock()
		joined := string(joinBytes(*chunks))
		mu.Unlock()
		if strings.Contains(joined, needle) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func joinChunks(mu *sync.Mutex, chunks *[][]byte) string {
	mu.Lock()
	defer mu.Unlock()
	return string(joinBytes(*chunks))
}

func joinBytes(in [][]byte) []byte {
	var out []byte
	for _, b := range in {
		out = append(out, b...)
	}
	return out
}

// exampleEchoStreamDir returns the absolute path of examples/echo-stream
// relative to this test file.
func exampleEchoStreamDir(t *testing.T) string {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	// here = .../internal/tui/devrun_test.go → go up two dirs → repo root.
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
	dir := filepath.Join(repoRoot, "examples", "echo-stream")
	return dir
}

func buildEchoStream(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", "echo-stream", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build echo-stream: %v\n%s", err, out)
	}
}
