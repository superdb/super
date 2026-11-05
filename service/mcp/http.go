package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
)

// MaxHTTPBody is the maximum size of a POST body (e.g., a load's inline
// data) accepted by ServeHTTP.
const MaxHTTPBody = 256 << 20

// ServeHTTP implements the MCP Streamable HTTP transport for a single MCP
// endpoint.  Each POST carries one JSON-RPC message.  A request's response
// is returned as application/json, and a notification (e.g., a client's
// notifications/initialized) is accepted with status 202 and no body.  The
// server is stateless: it doesn't assign session IDs or offer a stream of
// server-initiated messages, so GET and DELETE aren't allowed.  A request
// is canceled if its connection closes.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" && (s.opts.AllowOrigin == nil || !s.opts.AllowOrigin(r)) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed: this MCP endpoint accepts only POST", http.StatusMethodNotAllowed)
		return
	}
	if v := r.Header.Get("MCP-Protocol-Version"); v != "" && !slices.Contains(protocolVersions, v) {
		http.Error(w, "unsupported MCP-Protocol-Version: "+v, http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxHTTPBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	body = bytes.TrimSpace(body)
	var resp *response
	if len(body) > 0 && body[0] == '[' {
		// MCP 2025-06-18 removed JSON-RPC batching.
		resp = errorResponse(json.RawMessage("null"), codeInvalidRequest, "batches are not supported")
	} else {
		resp = s.handle(r.Context(), body)
	}
	if resp == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	// SUP types like <int64> are more readable without HTML escaping.
	enc.SetEscapeHTML(false)
	_ = enc.Encode(resp)
}
