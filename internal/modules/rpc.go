// Package modules — JSON-RPC 2.0 framing (Stage 3.2).
//
// The control channel between the host and every module runs over fd 3
// (set up by the host when spawning the subprocess). Messages are
// line-delimited JSON: one full JSON object per line. Newlines are not
// permitted inside object payloads — encoders MUST emit compact JSON.
//
// This is JSON-RPC 2.0 with two small concessions:
//
//   - Request IDs are integers (host monotonic) or strings (modules).
//   - Notifications (no `id` field) are fire-and-forget; replies are
//     skipped by the receiver.
//
// The Conn type is symmetric: both host and module use it. It carries a
// goroutine that reads lines from the underlying io.Reader and dispatches
// either to a per-request response channel (correlated by ID) or to the
// registered method handler.
//
// We deliberately do NOT implement batching, JSON-RPC 1.0 fallback, or
// the "method-not-found" auto-error for notifications — none are needed
// for hackermode's traffic and they bloat the code.
package modules

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// jsonrpcVersion is the protocol version string.
const jsonrpcVersion = "2.0"

// Request is the outbound shape for a request. Notifications omit ID.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response carries the result or error for a previously-issued request.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is the JSON-RPC error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// Standard error codes (subset).
const (
	ErrCodeParse          = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603
)

// Handler runs a single method invocation. It receives the raw params
// (may be nil) and returns either a result or an *RPCError.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// Conn is a duplex JSON-RPC connection over an io.Reader / io.Writer pair.
// One Conn per module subprocess on the host side; one Conn per module
// process on the SDK side.
type Conn struct {
	r io.Reader
	w io.Writer

	enc *json.Encoder
	dec *bufio.Scanner

	mu       sync.Mutex
	handlers map[string]Handler
	pending  map[uint64]chan rpcDelivery
	nextID   atomic.Uint64
	closed   atomic.Bool

	writeMu sync.Mutex
}

type rpcDelivery struct {
	result json.RawMessage
	err    error
}

// NewConn returns a new connection. Call Serve to start the read loop.
func NewConn(r io.Reader, w io.Writer) *Conn {
	c := &Conn{
		r:        r,
		w:        w,
		enc:      json.NewEncoder(w),
		handlers: map[string]Handler{},
		pending:  map[uint64]chan rpcDelivery{},
	}
	c.enc.SetEscapeHTML(false)
	sc := bufio.NewScanner(r)
	// Allow larger payloads than the 64KB default.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	c.dec = sc
	return c
}

// Handle registers a method handler. Replaces any existing handler for
// the same method name.
func (c *Conn) Handle(method string, h Handler) {
	c.mu.Lock()
	c.handlers[method] = h
	c.mu.Unlock()
}

// Call issues a request and blocks until the response arrives or ctx is
// done. The returned bytes are the raw `result` payload.
func (c *Conn) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c.closed.Load() {
		return nil, errors.New("rpc: connection closed")
	}
	id := c.nextID.Add(1)
	ch := make(chan rpcDelivery, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.write(Request{
		JSONRPC: jsonrpcVersion,
		ID:      &id,
		Method:  method,
		Params:  marshalParams(params),
	}); err != nil {
		return nil, err
	}

	select {
	case d := <-ch:
		return d.result, d.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Notify sends a notification (no response expected).
func (c *Conn) Notify(method string, params any) error {
	return c.write(Request{
		JSONRPC: jsonrpcVersion,
		Method:  method,
		Params:  marshalParams(params),
	})
}

// Serve runs the read loop. It returns when the underlying reader hits
// EOF or an irrecoverable error. Safe to call once per Conn.
func (c *Conn) Serve(ctx context.Context) error {
	for c.dec.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := c.dec.Bytes()
		if len(line) == 0 {
			continue
		}
		// Decide whether this is a request/notification or a response by
		// peeking for the "method" key.
		var probe struct {
			Method string  `json:"method"`
			ID     *uint64 `json:"id"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			// Best-effort log via stderr — host adds proper logging on its side.
			_ = c.writeError(nil, ErrCodeParse, "parse error")
			continue
		}
		if probe.Method != "" {
			// Inbound request or notification.
			go c.dispatchInbound(ctx, line, probe.Method, probe.ID)
			continue
		}
		// Response.
		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		c.deliverResponse(resp)
	}
	c.closed.Store(true)
	return c.dec.Err()
}

// Close marks the connection closed for future Call/Notify attempts. The
// caller is responsible for closing the underlying reader/writer.
func (c *Conn) Close() { c.closed.Store(true) }

func (c *Conn) dispatchInbound(ctx context.Context, raw []byte, method string, id *uint64) {
	c.mu.Lock()
	h, ok := c.handlers[method]
	c.mu.Unlock()
	if !ok {
		if id != nil {
			_ = c.writeError(id, ErrCodeMethodNotFound, "method not found: "+method)
		}
		return
	}
	var req Request
	_ = json.Unmarshal(raw, &req)

	result, err := h(ctx, req.Params)
	if id == nil {
		return // notification — no response
	}
	if err != nil {
		_ = c.writeError(id, ErrCodeInternal, err.Error())
		return
	}
	resultJSON, mErr := json.Marshal(result)
	if mErr != nil {
		_ = c.writeError(id, ErrCodeInternal, "marshal result: "+mErr.Error())
		return
	}
	_ = c.write(Response{
		JSONRPC: jsonrpcVersion,
		ID:      id,
		Result:  resultJSON,
	})
}

func (c *Conn) deliverResponse(resp Response) {
	if resp.ID == nil {
		return
	}
	c.mu.Lock()
	ch, ok := c.pending[*resp.ID]
	c.mu.Unlock()
	if !ok {
		return
	}
	d := rpcDelivery{result: resp.Result}
	if resp.Error != nil {
		d.err = resp.Error
	}
	select {
	case ch <- d:
	default:
	}
}

func (c *Conn) write(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed.Load() {
		return errors.New("rpc: closed")
	}
	return c.enc.Encode(v)
}

func (c *Conn) writeError(id *uint64, code int, msg string) error {
	return c.write(Response{
		JSONRPC: jsonrpcVersion,
		ID:      id,
		Error:   &RPCError{Code: code, Message: msg},
	})
}

func marshalParams(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
