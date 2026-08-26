// Package mcptest provides contract-accurate fake HTTP servers for the Pinner
// account and content APIs, generated from the same vendored swagger specs the
// official SDKs consume. They let pinner-cli's end-to-end tests exercise the
// full stack (CLI/MCP -> SDK client -> HTTP -> API) against a deterministic
// upstream double instead of the real Pinner.xyz service.
package mcptest

import (
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

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

// Seed registers a deterministic account on the account double and authorizes
// the same token on the content double, so both contracts accept the returned
// bearer token. It seeds one IPNS key so the ipns_keys_list/get tools have
// data for the default token. It returns the token.
func (s *Server) Seed(email, firstName, lastName string) string {
	tok := s.account.Seed(email, firstName, lastName)
	s.ipfs.AuthorizeToken(tok)
	s.ipfs.SeedIPNSKey("seed-key")
	s.account.SeedOperations()
	return tok
}

// routeTarget is which fake double serves a request.
type routeTarget int

const (
	// targetContent serves the content API (/api/* pinning/content routes and
	// the /pins pinning service).
	targetContent routeTarget = iota
	// targetAccount serves the account API (/api/auth/, /api/account/) and the
	// account-only /api/billing/, /api/operations and /api/upload-limit routes.
	targetAccount
)

func (t routeTarget) String() string {
	switch t {
	case targetAccount:
		return "account"
	default:
		return "content"
	}
}

// routeTargetOf maps a request path to the double that serves it. It is the
// single source of truth for routing, used by both the dispatcher and the
// access log so they can never disagree about where a request went.
//
// Routing is decided on exact path segments, not crude prefix matching:
// /api/<area>/... with <area> in the account set routes to the account double,
// the Boxo pinning service at /pins and every other /api/* route go to the
// content double.
func routeTargetOf(path string) routeTarget {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if len(segs) == 0 || segs[0] == "" {
		return targetContent
	}
	if segs[0] != "api" {
		// The only non-/api top-level route is the Boxo pinning service.
		return targetContent
	}
	if len(segs) >= 2 {
		switch segs[1] {
		case "account", "auth", "billing", "operations", "upload-limit":
			return targetAccount
		}
	}
	return targetContent
}

// Handler returns a dispatcher that routes the account API's prefixes to the
// account double and everything else (content /api/* and the /pins pinning
// service) to the content double, wrapped in an access log.
//
// The e2e harness can fail silently with empty `{"status":"ok","value":{}}`
// reads, which is untraceable without server-side visibility. The access log
// writes one line per request (method, path, target double, auth presence,
// response status, duration) to stderr, so a misrouted or unauthenticated call
// is diagnosable from the CI log instead of appearing as a mystery empty read.
func (s *Server) Handler() http.Handler {
	dispatcher := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch routeTargetOf(r.URL.Path) {
		case targetAccount:
			account.Handler(s.account).ServeHTTP(w, r)
		default:
			ipfs.Handler(s.ipfs).ServeHTTP(w, r)
		}
	})
	return accessLog(dispatcher)
}

// statusRecorder captures the response status code so the access log can report
// how the fake actually answered, not just what was asked.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// accessLog wraps next with one log line per request to stderr: method, path,
// routed double, whether a bearer token was presented, the response status,
// and the elapsed time.
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		auth := "none"
		if r.Header.Get("Authorization") != "" {
			auth = "bearer"
		}
		start := time.Now()
		next.ServeHTTP(rec, r)
		log.Printf("mcptest: %s %s [%s] auth=%s status=%d elapsed=%s\n",
			r.Method, r.URL.Path, routeTargetOf(r.URL.Path), auth, rec.status, time.Since(start))
	})
}

// Start boots an httptest server for the fake API and returns it.
func (s *Server) Start() *httptest.Server {
	return httptest.NewServer(s.Handler())
}
