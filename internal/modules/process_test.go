package modules

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// buildTestModule compiles the in-repo examples/echo-stream-min test
// helper to a temp binary. The helper sits in this same package so we
// don't need to ship cross-package test fixtures.
//
// We do this by writing a tiny Go program, building it, and pointing
// Spawn at the resulting binary. This exercises the full fd-3 pathway.
const helperSource = `
package main

import (
	"bufio"
	"encoding/json"
	"os"
)

func main() {
	// fd 3 is provided as a single pipe in (rpc child read) and fd 4 out (rpc child write).
	in := os.NewFile(3, "rpc-in")
	out := os.NewFile(4, "rpc-out")
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		var req map[string]any
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		id, hasID := req["id"]
		method, _ := req["method"].(string)
		switch method {
		case "init":
			if !hasID {
				continue
			}
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"module_id": "test.echo",
					"version":   "0.0.1",
					"commands":  []any{},
				},
			})
		case "shutdown":
			return
		default:
			if hasID {
				_ = enc.Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"error":   map[string]any{"code": -32601, "message": "method not found"},
				})
			}
		}
	}
}
`

func buildHelper(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(helperSource), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "helper")
	cmd := exec.Command("go", "build", "-o", bin, src)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build helper: %v", err)
	}
	return bin
}

func writeManifest(t *testing.T, dir, binary string) *Manifest {
	t.Helper()
	manifestPath := filepath.Join(dir, ManifestFileName)
	src := `
[module]
id      = "test.echo"
name    = "Echo"
version = "0.0.1"

[entry]
binary = "` + binary + `"
mode   = "stream"
`
	if err := os.WriteFile(manifestPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := ParseFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSpawnAndHandshake(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess spawn under -short")
	}
	bin := buildHelper(t)
	dir := filepath.Dir(bin)
	m := writeManifest(t, dir, filepath.Base(bin))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	proc, err := Spawn(ctx, m, ProcessOpts{Dir: dir})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	info, err := proc.Handshake(ctx, InitParams{TabID: "tab-1", Mode: ModeStream})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if info.ModuleID != "test.echo" {
		t.Fatalf("module id: %q", info.ModuleID)
	}
	if proc.State() != StateRunning {
		t.Fatalf("expected running, got %v", proc.State())
	}

	if err := proc.Shutdown(ctx, 2*time.Second); err != nil && !errors.Is(err, context.Canceled) {
		// Wait may surface a non-zero exit; that's fine for stream-mode helper.
		t.Logf("shutdown err (non-fatal): %v", err)
	}
}

func TestManager_StartAndStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess spawn under -short")
	}
	bin := buildHelper(t)
	dir := filepath.Dir(bin)
	m := writeManifest(t, dir, filepath.Base(bin))

	mgr := NewManager()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	proc, info, err := mgr.Start(ctx, StartOpts{
		TabID: "tab-1", Manifest: m, Dir: dir,
		Init: InitParams{TabID: "tab-1", Mode: ModeStream},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if info.ModuleID != "test.echo" {
		t.Fatalf("info: %+v", info)
	}
	if mgr.Get("test.echo", "tab-1") == nil {
		t.Fatal("expected process tracked")
	}

	if err := mgr.Stop(ctx, "test.echo", "tab-1", 2*time.Second); err != nil {
		t.Logf("stop err (non-fatal): %v", err)
	}
	<-proc.Done()
	// Cleanup runs in a goroutine after Done — poll briefly.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.Get("test.echo", "tab-1") == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected process untracked after shutdown")
}

func TestManager_DuplicateStartRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess spawn under -short")
	}
	bin := buildHelper(t)
	dir := filepath.Dir(bin)
	m := writeManifest(t, dir, filepath.Base(bin))

	mgr := NewManager()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, _, err := mgr.Start(ctx, StartOpts{TabID: "tab-x", Manifest: m, Dir: dir,
		Init: InitParams{TabID: "tab-x", Mode: ModeStream}})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = mgr.Start(ctx, StartOpts{TabID: "tab-x", Manifest: m, Dir: dir,
		Init: InitParams{TabID: "tab-x", Mode: ModeStream}})
	if err == nil {
		t.Fatal("expected duplicate start to fail")
	}
	_ = mgr.Stop(ctx, "test.echo", "tab-x", 2*time.Second)
}
