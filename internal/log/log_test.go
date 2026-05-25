package log

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestEmit_WritesJSONLines(t *testing.T) {
	var buf bytes.Buffer
	InitWriter(&buf, LevelDebug)
	Info("hello", "k", 1)
	Error("nope", "code", "ABC")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("line 1 not JSON: %v", err)
	}
	if first["lvl"] != "info" || first["msg"] != "hello" {
		t.Fatalf("unexpected record: %v", first)
	}
}

func TestMinLevel_Filters(t *testing.T) {
	var buf bytes.Buffer
	InitWriter(&buf, LevelWarn)
	Debug("hidden")
	Info("hidden")
	Warn("visible")
	Error("visible")
	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Fatalf("low-level messages should be filtered, got %q", out)
	}
	if !strings.Contains(out, "visible") {
		t.Fatalf("warn+ messages should appear, got %q", out)
	}
}

func TestWith_AddsInheritedFields(t *testing.T) {
	var buf bytes.Buffer
	InitWriter(&buf, LevelDebug)
	child := With("module", "acme.email", "tab", "tab-3")
	child.Info("opened")
	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["module"] != "acme.email" {
		t.Fatalf("expected module field, got %v", rec)
	}
	if rec["tab"] != "tab-3" {
		t.Fatalf("expected tab field, got %v", rec)
	}
}

func TestWith_UnmatchedTrailingKeysDropped(t *testing.T) {
	var buf bytes.Buffer
	InitWriter(&buf, LevelDebug)
	// Odd number of kv args — trailing "extra" must not panic or appear.
	With("a", 1, "extra").Info("ok")
	var rec map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec)
	if rec["a"] != float64(1) {
		t.Fatalf("expected a=1, got %v", rec)
	}
	if _, ok := rec["extra"]; ok {
		t.Fatal("unmatched key should be dropped")
	}
}

func TestEmit_ConcurrentWritersDoNotInterleave(t *testing.T) {
	var buf bytes.Buffer
	InitWriter(&buf, LevelDebug)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			Info("msg", "i", i)
		}(i)
	}
	wg.Wait()

	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("interleaved write: %q (%v)", line, err)
		}
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]Level{
		"debug":   LevelDebug,
		"INFO":    LevelInfo,
		"":        LevelInfo,
		"warning": LevelWarn,
		"err":     LevelError,
		"bogus":   LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v; want %v", in, got, want)
		}
	}
}
