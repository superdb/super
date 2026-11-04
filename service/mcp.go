package service

import (
	"net/http"

	"github.com/rs/cors"
	"github.com/superdb/super/api"
	"github.com/superdb/super/db"
	"github.com/superdb/super/service/auth"
	"github.com/superdb/super/service/mcp"
)

// newMCPServer returns the MCP server for the /mcp endpoint.  A request
// from a web page (i.e., with an Origin header) is allowed only from an
// origin allowed by -cors.origin, which MCP requires to prevent DNS
// rebinding attacks.
func newMCPServer(root *db.Root, conf Config) *mcp.Server {
	origins := cors.New(cors.Options{AllowedOrigins: conf.CORSAllowedOrigins})
	return mcp.NewServer(root, conf.Version, mcp.Options{
		ReadOnly: conf.MCPReadOnly,
		Commit:   api.CommitMessage{Author: "mcp"},
		AllowOrigin: func(r *http.Request) bool {
			return len(conf.CORSAllowedOrigins) > 0 && origins.OriginAllowed(r)
		},
	})
}

func handleMCP(c *Core, w *ResponseWriter, r *Request) {
	ctx := r.Context()
	// With authentication, a load is committed with the authenticated user
	// as its author.  (Without it, the context has a placeholder identity.)
	if c.auth != nil {
		if ident := auth.IdentityFromContext(ctx); ident.UserID != "" {
			ctx = mcp.ContextWithAuthor(ctx, string(ident.UserID))
		}
	}
	c.mcp.ServeHTTP(w.ResponseWriter, r.Request.WithContext(ctx))
}
