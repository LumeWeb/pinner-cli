// Package mcptest provides contract-accurate fake HTTP servers for the Pinner
// account and content APIs, generated from the same vendored swagger specs the
// official SDKs consume. They let pinner-cli's end-to-end tests exercise the
// full stack (CLI/MCP -> SDK client -> HTTP -> API) against a deterministic
// upstream double instead of the real Pinner.xyz service.
package mcptest

import (
	"net/http"
	"net/http/httptest"
	"strings"

	"go.lumeweb.com/pinner-cli/internal/mcptest/account"
	"go.lumeweb.com/pinner-cli/internal/mcptest/ipfs"
)

// Server bundles the fake account + content API doubles behind one
// http.Handler, so a single httptest server serves both contracts.
type Server struct {
	account *account.Server
	ipfs    *ipfs.Server
}

// New returns a fake Pinner API double with empty state.
func New() *Server {
	return &Server{
		account: account.NewServer(),
		ipfs:    ipfs.NewServer(),
	}
}

// Account returns the account double (for direct token injection etc).
func (s *Server) Account() *account.Server { return s.account }

// IPFS returns the content double.
func (s *Server) IPFS() *ipfs.Server { return s.ipfs }

// Handler returns a dispatcher that routes the account API's prefixes
// (/api/auth/, /api/account/) to the account double and everything else
// (content /api/* and the /pins pinning service) to the content double.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/api/auth/"), strings.HasPrefix(p, "/api/account/"):
			account.Handler(s.account).ServeHTTP(w, r)
		default:
			ipfs.Handler(s.ipfs).ServeHTTP(w, r)
		}
	})
}

// Start boots an httptest server for the fake API and returns it.
func (s *Server) Start() *httptest.Server {
	return httptest.NewServer(s.Handler())
}
