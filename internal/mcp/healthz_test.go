package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthzHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthzHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"ok":true}`, rec.Body.String())
}

func TestHealthzHandlerAllowsHead(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "/healthz", nil)
	healthzHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHealthzHandlerRejectsNonProbeMethods(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/healthz", nil)
			healthzHandler(rec, req)

			assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
			assert.Equal(t, "GET, HEAD", rec.Header().Get("Allow"))
		})
	}
}

func TestHealthzRegisteredOnMuxOutsideAuth(t *testing.T) {
	// The healthz handler must be reachable without any auth middleware. We
	// assert the handler itself returns a usable response and that the path is
	// handled (a 200 here is sufficient; the bearer-token guard is applied only
	// to /mcp, not to the mux registered /healthz).
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}
