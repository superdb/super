package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// Serve runs the MCP stdio transport, reading newline-delimited JSON-RPC
// messages from r and writing responses to w until r reaches EOF or ctx is
// canceled.  Tool calls run one at a time, in order, while other requests
// (e.g., ping) and notifications (e.g., a cancellation) are handled as they
// arrive, so a long-running query can be canceled.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	t := &stdio{
		server: s,
		out:    json.NewEncoder(w),
		calls:  make(map[string]*call),
		queue:  make(chan func(), 64),
	}
	// SUP types like <int64> are more readable without HTML escaping.
	t.out.SetEscapeHTML(false)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for f := range t.queue {
			f()
		}
	}()
	// Finish pending tool calls before returning.
	defer func() {
		close(t.queue)
		<-done
	}()
	in := bufio.NewReader(r)
	for {
		line, err := in.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			t.handle(ctx, line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

type stdio struct {
	server *Server
	mu     sync.Mutex // protects out and calls
	out    *json.Encoder
	calls  map[string]*call // pending tool calls by request ID
	queue  chan func()      // tool calls, which run in order
}

type call struct {
	cancel   context.CancelFunc
	canceled bool // by a notifications/cancelled from the client
}

func (t *stdio) handle(ctx context.Context, line []byte) {
	var req request
	if json.Unmarshal(line, &req) == nil {
		switch {
		case isNotification(req.ID) && req.Method == "notifications/cancelled":
			t.cancel(req)
			return
		case !isNotification(req.ID) && req.Method == "tools/call":
			t.enqueue(ctx, req.ID, line)
			return
		}
	}
	if resp := t.server.handle(ctx, line); resp != nil {
		t.write(resp)
	}
}

// enqueue queues the tool call in line with request ID id.
func (t *stdio) enqueue(ctx context.Context, id []byte, line []byte) {
	ctx, cancel := context.WithCancel(ctx)
	key := string(id)
	c := &call{cancel: cancel}
	t.mu.Lock()
	t.calls[key] = c
	t.mu.Unlock()
	t.queue <- func() {
		defer cancel()
		resp := t.server.handle(ctx, line)
		t.mu.Lock()
		delete(t.calls, key)
		canceled := c.canceled
		t.mu.Unlock()
		// The spec says not to respond to a canceled request.
		if !canceled && resp != nil {
			t.write(resp)
		}
	}
}

func (t *stdio) cancel(req request) {
	var params struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(req.Params, &params) != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if c, ok := t.calls[string(params.RequestID)]; ok {
		c.canceled = true
		c.cancel()
	}
}

func (t *stdio) write(resp *response) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// An encoding error can't be reported to the client, which reads
	// from the stream we failed to write.
	_ = t.out.Encode(resp)
}
