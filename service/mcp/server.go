// Package mcp implements a Model Context Protocol (MCP) server for a local
// SuperDB database.  The server provides tools for listing and describing
// tables, for running SuperSQL queries, whose results may be formatted as
// JSON or as SUP, which, unlike JSON, preserves SuperDB's types, and for
// loading data into tables.  It supports the MCP stdio transport (see
// Server.Serve) and the Streamable HTTP transport (see Server.ServeHTTP).
//
// See https://modelcontextprotocol.io/specification.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/superdb/super/api"
	"github.com/superdb/super/db"
	dbapi "github.com/superdb/super/db/api"
	"github.com/superdb/super/runtime"
)

// LatestProtocolVersion is the latest MCP protocol version supported.
const LatestProtocolVersion = "2025-06-18"

// protocolVersions are the MCP protocol versions supported, latest first.
var protocolVersions = []string{LatestProtocolVersion, "2025-03-26", "2024-11-05"}

// JSON-RPC error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// Options configures a Server.
type Options struct {
	// ReadOnly causes queries that modify the database to be rejected and
	// disables loading.
	ReadOnly bool
	// Commit is the default commit message (e.g., its author) of a load.
	// For HTTP, the author may be overridden per request (see
	// ContextWithAuthor).
	Commit api.CommitMessage
	// AllowOrigin reports whether ServeHTTP accepts a request with an
	// Origin header (i.e., from a web page), which MCP requires a server
	// to check to prevent DNS rebinding attacks.  If nil, such requests
	// are rejected.  Requests without an Origin header (e.g., from an
	// agent) are always accepted.
	AllowOrigin func(*http.Request) bool
}

// Server is an MCP server for a database.  It is safe for concurrent use
// by the HTTP transport.
type Server struct {
	root    *db.Root
	db      runtime.Database
	version string
	opts    Options
}

// NewServer returns a server for the database at root, where version is
// the SuperDB version reported to clients.
func NewServer(root *db.Root, version string, opts Options) *Server {
	return &Server{
		root:    root,
		db:      dbapi.FromRoot(root),
		version: version,
		opts:    opts,
	}
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func resultResponse(id json.RawMessage, result any) *response {
	return &response{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id json.RawMessage, code int, msg string) *response {
	return &response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

// handle handles msg, a JSON-RPC message, and returns the response or nil
// if msg needs no response (i.e., it's a notification).  Cancellation is
// a transport matter (e.g., see stdio).
func (s *Server) handle(ctx context.Context, msg []byte) *response {
	var req request
	if err := json.Unmarshal(msg, &req); err != nil {
		return errorResponse(json.RawMessage("null"), codeParseError, "parse error: "+err.Error())
	}
	if isNotification(req.ID) {
		// Notifications (e.g., notifications/initialized) need no
		// response.
		return nil
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return errorResponse(req.ID, codeInvalidRequest, "invalid request")
	}
	switch req.Method {
	case "initialize":
		return s.initialize(req)
	case "ping":
		return resultResponse(req.ID, struct{}{})
	case "tools/list":
		return resultResponse(req.ID, map[string]any{"tools": s.tools()})
	case "tools/call":
		return s.toolsCall(ctx, req)
	default:
		return errorResponse(req.ID, codeMethodNotFound, "method not found: "+req.Method)
	}
}

func isNotification(id json.RawMessage) bool {
	return len(id) == 0 || string(id) == "null"
}

func (s *Server) initialize(req request) *response {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, codeInvalidParams, "invalid params: "+err.Error())
	}
	// Use the client's version if supported and otherwise our latest
	// version, which the client may reject.
	version := LatestProtocolVersion
	if slices.Contains(protocolVersions, params.ProtocolVersion) {
		version = params.ProtocolVersion
	}
	return resultResponse(req.ID, struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
		ServerInfo      map[string]any `json:"serverInfo"`
		Instructions    string         `json:"instructions"`
	}{
		ProtocolVersion: version,
		Capabilities:    map[string]any{"tools": map[string]any{"listChanged": false}},
		ServerInfo:      map[string]any{"name": "superdb", "title": "SuperDB", "version": s.version},
		Instructions:    s.instructions(),
	})
}

func (s *Server) toolsCall(ctx context.Context, req request) *response {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, codeInvalidParams, "invalid params: "+err.Error())
	}
	tool, ok := s.toolFunc(params.Name)
	if !ok {
		return errorResponse(req.ID, codeInvalidParams, "unknown tool: "+params.Name)
	}
	result, err := tool(ctx, params.Arguments)
	if errors.Is(err, errInvalidArgs) {
		return errorResponse(req.ID, codeInvalidParams, err.Error())
	}
	if err != nil {
		// A tool's failure (e.g., a query error) is a result that the
		// model can see and act on.
		result = errorResult(err)
	}
	return resultResponse(req.ID, result)
}

var errInvalidArgs = errors.New("invalid arguments")

func invalidArgs(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInvalidArgs, fmt.Sprintf(format, args...))
}

type authorKey struct{}

// ContextWithAuthor returns a context for a request whose loads are
// committed with author (e.g., the authenticated user) rather than the
// author of Options.Commit.
func ContextWithAuthor(ctx context.Context, author string) context.Context {
	return context.WithValue(ctx, authorKey{}, author)
}

func authorFromContext(ctx context.Context) string {
	author, _ := ctx.Value(authorKey{}).(string)
	return author
}
