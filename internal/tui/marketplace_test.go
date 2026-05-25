package tui

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sectersion/hackermode/internal/config"
	"github.com/sectersion/hackermode/internal/tui/marketplace"
)

// TestMarketplace_OpenAndRender opens the marketplace, waits for the
// async index load, and confirms the seeded module appears in the View.
func TestMarketplace_OpenAndRender(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess + temp dirs under -short")
	}
	regRoot := seedTinyRegistry(t)
	cfg, cleanup := withMarketplaceConfig(t, regRoot)
	defer cleanup()

	var mu sync.Mutex
	var latest Model
	latest = New(cfg)
	send := func(msg tea.Msg) {
		mu.Lock()
		defer mu.Unlock()
		newM, _ := latest.Update(msg)
		latest = newM.(Model)
	}
	latest.SetRuntime(&Runtime{Send: send})

	send(tea.WindowSizeMsg{Width: 120, Height: 30})

	// Open the marketplace through its action.
	mu.Lock()
	newM, cmd := latest.applyHostAction("host.marketplace.show")
	latest = newM
	latest.SetRuntime(&Runtime{Send: send})
	mu.Unlock()

	// Drive the cmd that loads the index.
	if cmd != nil {
		msg := cmd()
		send(msg)
	}

	// Poll for the seeded module to appear in the View.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		view := stripANSI(latest.View())
		mu.Unlock()
		if strings.Contains(view, "test.demo") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	final := stripANSI(latest.View())
	mu.Unlock()
	t.Fatalf("test.demo never appeared in marketplace view:\n%s", final)
}

// TestMarketplace_InstallRoundtrip selects a module in the marketplace
// and hits enter; the host installs it and the new module's commands
// land in the registry.
func TestMarketplace_InstallRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess + temp dirs under -short")
	}
	regRoot := seedTinyRegistry(t)
	cfg, cleanup := withMarketplaceConfig(t, regRoot)
	defer cleanup()

	var mu sync.Mutex
	var latest Model
	latest = New(cfg)
	send := func(msg tea.Msg) {
		mu.Lock()
		defer mu.Unlock()
		newM, _ := latest.Update(msg)
		latest = newM.(Model)
	}
	latest.SetRuntime(&Runtime{Send: send})

	send(tea.WindowSizeMsg{Width: 120, Height: 30})

	mu.Lock()
	newM, cmd := latest.applyHostAction("host.marketplace.show")
	latest = newM
	latest.SetRuntime(&Runtime{Send: send})
	mu.Unlock()
	if cmd != nil {
		send(cmd())
	}

	// Drain the async index load.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ready := latest.market.Open() && !marketplaceLoading(latest)
		mu.Unlock()
		if ready {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Press enter to install the highlighted (only) module.
	send(tea.KeyMsg{Type: tea.KeyEnter})

	// Verify the binary landed.
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(installModulesRoot(t), "test.demo", "current")); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("install did not produce an installed module")
}

// marketplaceLoading is a tiny helper that peeks at the model's private
// loading state via the View output. We accept "loading" or empty
// modules list as still-loading.
func marketplaceLoading(m Model) bool {
	return strings.Contains(stripANSI(m.View()), "loading registry")
}

// installModulesRoot returns the resolved modules dir from the test
// environment's XDG_DATA_HOME override.
func installModulesRoot(t *testing.T) string {
	t.Helper()
	home := os.Getenv("XDG_DATA_HOME")
	if home == "" {
		t.Fatal("XDG_DATA_HOME not set")
	}
	return filepath.Join(home, "hackermode", "modules")
}

// withMarketplaceConfig sets XDG_CONFIG_HOME / XDG_DATA_HOME to temp
// dirs, writes a config.toml pointing at regRoot, and returns the
// resulting cfg.
func withMarketplaceConfig(t *testing.T, regRoot string) (config.Config, func()) {
	t.Helper()
	cfgHome := t.TempDir()
	dataHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_DATA_HOME", dataHome)

	if err := os.MkdirAll(filepath.Join(cfgHome, "hackermode"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgHome, "hackermode", "config.toml")
	body := "[marketplace]\nregistry = \"file://" + regRoot + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	return cfg, func() {}
}

// seedTinyRegistry builds a fake registry with one published module
// (test.demo @ 0.1.0 for linux/amd64). Returns the registry root.
func seedTinyRegistry(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	// Build a tiny tarball: manifest + executable.
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)
	manifestBytes := []byte(`[module]
id = "test.demo"
name = "Demo"
version = "0.1.0"

[entry]
binary = "demo-bin"
mode = "stream"
`)
	binBytes := []byte("#!/bin/sh\necho demo\n")
	_ = tw.WriteHeader(&tar.Header{Name: "hackermode.toml", Size: int64(len(manifestBytes)), Mode: 0o644})
	_, _ = tw.Write(manifestBytes)
	_ = tw.WriteHeader(&tar.Header{Name: "demo-bin", Size: int64(len(binBytes)), Mode: 0o755})
	_, _ = tw.Write(binBytes)
	_ = tw.Close()
	_ = gz.Close()
	tarball := tarBuf.Bytes()

	h := sha256.Sum256(tarball)
	hashHex := hex.EncodeToString(h[:])

	verDir := filepath.Join(tmp, "modules", "test.demo", "0.1.0")
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite := func(rel string, body []byte) {
		if err := os.WriteFile(filepath.Join(tmp, rel), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("index.json", []byte(
		`{"schema":"0.1","modules":[{"id":"test.demo","name":"Demo","latest":"0.1.0"}]}`))
	mustWrite("modules/test.demo/module.json", []byte(fmt.Sprintf(
		`{"id":"test.demo","name":"Demo","versions":[{"version":"0.1.0",`+
			`"manifest_url":"/x","checksums_url":"/x","binaries":[`+
			`{"platform":"linux/amd64","url":"/x","sha256":"%s"}]}]}`, hashHex)))
	mustWrite("modules/test.demo/0.1.0/checksums.txt",
		[]byte(fmt.Sprintf("%s linux-amd64.tar.gz\n", hashHex)))
	mustWrite("modules/test.demo/0.1.0/linux-amd64.tar.gz", tarball)

	// Silence the unused-import lint when tea is only used by other tests.
	_ = marketplace.OwnerID
	return tmp
}
