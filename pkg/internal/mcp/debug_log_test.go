package mcp

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TEMP DEBUG (remove before merge): these tests guard the temporary OAuth
// request/response logging that traces the Anthropic Claude handshake over a
// tunnel. Delete together with debugLogHandler when it is stripped.

// redirectDebugWriter points debugWriter at a fresh buffer for the duration of
// fn and returns everything written to it.
func redirectDebugWriter(t *testing.T, fn func()) string {
	t.Helper()
	old := debugWriter
	defer func() { debugWriter = old }()
	buf := &bytes.Buffer{}
	debugWriter = buf
	fn()
	return buf.String()
}

func TestDebugLogHandlerLogsOAuthButNotMCP(t *testing.T) {
	// A downstream handler that echoes the request body so we can prove the
	// middleware restored it.
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("echo:" + string(b)))
	})
	h := debugLogHandler(echo)

	t.Run("oauth register is logged with body restored", func(t *testing.T) {
		body := `{"token_endpoint_auth_method":"client_secret_post"}`
		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
		rec := httptest.NewRecorder()
		captured := redirectDebugWriter(t, func() {
			h.ServeHTTP(rec, req)
		})
		require.Equal(t, http.StatusCreated, rec.Code)
		require.Equal(t, "echo:"+body, rec.Body.String(), "request body must be restored for the downstream handler")
		require.Contains(t, captured, "POST /register")
		require.Contains(t, captured, "REQ-BODY: "+body)
	})

	t.Run("unknown path passes through unlogged", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		rec := httptest.NewRecorder()
		captured := redirectDebugWriter(t, func() {
			h.ServeHTTP(rec, req)
		})
		require.Empty(t, captured, "/mcp must not be logged")
	})
}
