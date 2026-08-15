package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveCORS runs a request through corsHandler and returns the response recorder.
func serveCORS(t *testing.T, method, origin string, requestHeaders http.Header) *httptest.ResponseRecorder {
	t.Helper()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	req := httptest.NewRequest(method, "/mcp", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for k, vs := range requestHeaders {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	corsHandler(next).ServeHTTP(rec, req)
	return rec
}

func TestCORSHandlerReflectsOrigin(t *testing.T) {
	for _, origin := range []string{
		"https://chat.example.com",
		"http://localhost:3000",
		"https://app.dev.internal:8443",
	} {
		rec := serveCORS(t, http.MethodGet, origin, nil)
		// The request Origin is echoed back dynamically, not "*".
		assert.Equal(t, origin, rec.Header().Get("Access-Control-Allow-Origin"))
		assert.Contains(t, rec.Header().Values("Vary"), "Origin")
		assert.Equal(t, http.StatusOK, rec.Code)
	}
}

func TestCORSHandlerAllowedHeadersAndMethods(t *testing.T) {
	origin := "https://client.example.com"
	rec := serveCORS(t, http.MethodGet, origin, http.Header{
		"Authorization":        {"Bearer abc"},
		"Mcp-Session-Id":       {"sess-123"},
		"MCP-Protocol-Version": {"2025-06-18"},
		"Last-Event-ID":        {"evt-1"},
	})
	assert.Equal(t, origin, rec.Header().Get("Access-Control-Allow-Origin"))
	// Exposed header lets the browser read the MCP session id.
	assert.Equal(t, "Mcp-Session-Id", rec.Header().Get("Access-Control-Expose-Headers"))
}

func TestCORSHandlerAnswersPreflight(t *testing.T) {
	origin := "https://client.example.com"
	rec := serveCORS(t, http.MethodOptions, origin, http.Header{
		"Access-Control-Request-Method": {"POST"},
		// Fetch-spec conformance: browsers send Access-Control-Request-Headers
		// lower-cased, sorted, and deduplicated.
		"Access-Control-Request-Headers": {"authorization, content-type, mcp-session-id"},
	})
	assert.Equal(t, origin, rec.Header().Get("Access-Control-Allow-Origin"))
	// Spec-conformant: the preflight response echoes the requested method and
	// (lower-cased) requested headers; the allow-list never appears wholesale.
	assert.Equal(t, "POST", rec.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "authorization, content-type, mcp-session-id", rec.Header().Get("Access-Control-Allow-Headers"))
	// Preflight is answered with 204 No Content (rs/cors default).
	assert.Equal(t, http.StatusNoContent, rec.Code)
	require.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Methods"))
}

// TestCORSHandlerAdmitsAllActualMethods verifies the reflected origin applies to
// real (non-preflight) requests across the full allowed method set, not just the
// method echoed in a preflight.
func TestCORSHandlerAdmitsAllActualMethods(t *testing.T) {
	origin := "https://client.example.com"
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		rec := serveCORS(t, m, origin, nil)
		assert.Equal(t, origin, rec.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, http.StatusOK, rec.Code)
	}
}
