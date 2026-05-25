package modules

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// duplex creates two Conn instances bridged by in-memory pipes, simulating
// fd 3 between host and module.
func duplex(t *testing.T) (host, mod *Conn, cleanup func()) {
	t.Helper()
	hostR, modW := io.Pipe()
	modR, hostW := io.Pipe()
	host = NewConn(hostR, hostW)
	mod = NewConn(modR, modW)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = host.Serve(ctx) }()
	go func() { defer wg.Done(); _ = mod.Serve(ctx) }()

	cleanup = func() {
		cancel()
		_ = hostR.Close()
		_ = hostW.Close()
		_ = modR.Close()
		_ = modW.Close()
		wg.Wait()
	}
	return host, mod, cleanup
}

func TestRPC_RoundTrip(t *testing.T) {
	host, mod, done := duplex(t)
	defer done()

	mod.Handle("echo", func(ctx context.Context, params json.RawMessage) (any, error) {
		var p struct{ Msg string }
		_ = json.Unmarshal(params, &p)
		return map[string]string{"echo": p.Msg}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	raw, err := host.Call(ctx, "echo", map[string]string{"msg": "hi"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var out map[string]string
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out["echo"] != "hi" {
		t.Fatalf("unexpected payload: %v", out)
	}
}

func TestRPC_MethodNotFound(t *testing.T) {
	host, _, done := duplex(t)
	defer done()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := host.Call(ctx, "missing", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %T", err)
	}
	if rpcErr.Code != ErrCodeMethodNotFound {
		t.Fatalf("code: %d", rpcErr.Code)
	}
}

func TestRPC_HandlerErrorReturnedAsInternal(t *testing.T) {
	host, mod, done := duplex(t)
	defer done()
	mod.Handle("boom", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return nil, errors.New("kaboom")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := host.Call(ctx, "boom", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != ErrCodeInternal {
		t.Fatalf("expected internal error, got %v", err)
	}
}

func TestRPC_NotificationsNoResponse(t *testing.T) {
	host, mod, done := duplex(t)
	defer done()
	got := make(chan string, 1)
	mod.Handle("ping", func(ctx context.Context, p json.RawMessage) (any, error) {
		got <- string(p)
		return nil, nil
	})
	if err := host.Notify("ping", map[string]int{"x": 1}); err != nil {
		t.Fatal(err)
	}
	select {
	case payload := <-got:
		if payload == "" {
			t.Fatal("expected params delivered")
		}
	case <-time.After(time.Second):
		t.Fatal("notification not delivered")
	}
}

func TestRPC_CallAfterCloseFails(t *testing.T) {
	host, _, done := duplex(t)
	host.Close()
	defer done()
	_, err := host.Call(context.Background(), "anything", nil)
	if err == nil {
		t.Fatal("expected error after close")
	}
}
