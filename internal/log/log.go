// Package log is hackermode's structured logger. It writes JSONL records to
// ~/.cache/hackermode/log.jsonl (one JSON object per line) and is safe to
// call from many goroutines.
//
// Why JSONL and not slog/zap/logrus? Because the host is a TUI: we cannot
// write to stdout/stderr during normal operation without corrupting the
// rendered frame. Logging strictly to a file (or a ring buffer in the
// future) is required. JSONL is also trivial to tail (`tail -f log.jsonl`),
// pipe through `jq`, and ingest for later diagnostics.
//
// The package keeps a single process-wide logger. Callers obtain a tagged
// logger via With(module="acme.email"); all records emitted through that
// logger inherit those fields. RPC traffic and module lifecycle events
// flow through here in Phase B.
package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sectersion/hackermode/internal/paths"
)

// Level is the severity of a record. Higher is more important.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "?"
	}
}

// ParseLevel turns a string (case-insensitive) into a Level. Unknown values
// return LevelInfo.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "info", "":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error", "err":
		return LevelError
	default:
		return LevelInfo
	}
}

// Logger writes structured records to an underlying io.Writer.
type Logger struct {
	mu     *sync.Mutex
	w      io.Writer
	min    Level
	fields map[string]any // inherited fields (e.g. module ID)
}

// global state.
var (
	def *Logger = nopLogger() // until Init is called
)

// Init opens the log file and replaces the default logger. Safe to call
// multiple times; later calls replace the destination. The minimum level
// can be overridden via the HACKERMODE_LOG env var (e.g. "debug").
func Init() error {
	dir := paths.CacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir cache dir: %w", err)
	}
	path := filepath.Join(dir, "log.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	min := ParseLevel(os.Getenv("HACKERMODE_LOG"))
	def = &Logger{
		mu:     &sync.Mutex{},
		w:      f,
		min:    min,
		fields: map[string]any{},
	}
	return nil
}

// InitWriter is the test-friendly counterpart to Init: it accepts any
// writer (e.g. a bytes.Buffer) and a minimum level.
func InitWriter(w io.Writer, min Level) {
	def = &Logger{
		mu:     &sync.Mutex{},
		w:      w,
		min:    min,
		fields: map[string]any{},
	}
}

// Default returns the process-wide logger.
func Default() *Logger { return def }

// With returns a child logger with additional fields baked in. The fields
// argument should alternate keys and values: With("module", "acme.email").
// Unmatched trailing values are dropped.
func (l *Logger) With(kv ...any) *Logger {
	if l == nil {
		return def
	}
	fields := make(map[string]any, len(l.fields)+len(kv)/2)
	for k, v := range l.fields {
		fields[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			continue
		}
		fields[k] = kv[i+1]
	}
	return &Logger{mu: l.mu, w: l.w, min: l.min, fields: fields}
}

// With is the package-level convenience for Default().With(...).
func With(kv ...any) *Logger { return def.With(kv...) }

// Debug / Info / Warn / Error emit a record at the corresponding level.
// The kv list is "key", value, "key", value, ... — same shape as With.
func (l *Logger) Debug(msg string, kv ...any) { l.emit(LevelDebug, msg, kv) }
func (l *Logger) Info(msg string, kv ...any)  { l.emit(LevelInfo, msg, kv) }
func (l *Logger) Warn(msg string, kv ...any)  { l.emit(LevelWarn, msg, kv) }
func (l *Logger) Error(msg string, kv ...any) { l.emit(LevelError, msg, kv) }

// Package-level shortcuts.
func Debug(msg string, kv ...any) { def.Debug(msg, kv...) }
func Info(msg string, kv ...any)  { def.Info(msg, kv...) }
func Warn(msg string, kv ...any)  { def.Warn(msg, kv...) }
func Error(msg string, kv ...any) { def.Error(msg, kv...) }

// Close flushes/closes the underlying writer if it's a Closer. Called on
// process shutdown. Safe to call when the logger is the no-op default.
func Close() error {
	if c, ok := def.w.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (l *Logger) emit(lv Level, msg string, kv []any) {
	if l == nil || l.w == nil || lv < l.min {
		return
	}
	rec := make(map[string]any, 4+len(l.fields)+len(kv)/2)
	rec["t"] = time.Now().UTC().Format(time.RFC3339Nano)
	rec["lvl"] = lv.String()
	rec["msg"] = msg
	for k, v := range l.fields {
		rec[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			continue
		}
		rec[k] = kv[i+1]
	}
	buf, err := json.Marshal(rec)
	if err != nil {
		return
	}
	buf = append(buf, '\n')
	l.mu.Lock()
	_, _ = l.w.Write(buf)
	l.mu.Unlock()
}

// nopLogger returns a logger that drops everything; used before Init runs.
func nopLogger() *Logger {
	return &Logger{mu: &sync.Mutex{}, w: io.Discard, min: LevelError + 1, fields: map[string]any{}}
}
