package transfer

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCORSOriginOpaqueNull verifies that the token-gated transfer routes
// (corsUpload / corsDownload) reflect the SERIALIZED OPAQUE ORIGIN "null".
// An MCP host renders a ui:// app inside a sandboxed double-iframe whose
// Origin (without allow-same-origin) is the opaque origin, serialized to the
// literal string "null"; the host-rendered upload app therefore issue its
// cross-origin presigned PUT from that origin. The response must grant CORS
// for that exact opaque origin, while an arbitrary attacker origin is still
// refused — the route stays gated by the unguessable single-use token.
func TestCORSOriginOpaqueNull(t *testing.T) {
	// allowed() mirrors a coordinator whose own origin plus a configured host
	// origin are trusted. The opaque "null" origin is NOT in this list; it must
	// still be reflected via shouldReflectOrigin.
	allowed := func() []string {
		return []string{"https://server.example.com", "https://apps.example.com"}
	}

	h := corsUpload(allowed, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"upload_handle":"h"}`))
	})
	srv := httptest.NewServer(http.HandlerFunc(h))
	defer srv.Close()

	// Preflight from the opaque origin must return the reflecting header + PUT.
	pre, err := http.NewRequest(http.MethodOptions, srv.URL, nil)
	if err != nil {
		t.Fatalf("preflight req: %v", err)
	}
	pre.Header.Set("Origin", opaqueOrigin)
	pre.Header.Set("Access-Control-Request-Method", "PUT")
	preResp, err := http.DefaultClient.Do(pre)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	preResp.Body.Close()
	if preResp.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", preResp.StatusCode)
	}
	if got := preResp.Header.Get("Access-Control-Allow-Origin"); got != opaqueOrigin {
		t.Fatalf("preflight Allow-Origin = %q, want %q", got, opaqueOrigin)
	}
	if cc := preResp.Header.Get("Access-Control-Allow-Credentials"); cc == "true" {
		t.Fatalf("opaque-origin upload must not send credentials")
	}

	// The PUT from the opaque origin must carry the reflecting header + 202.
	put, err := http.NewRequest(http.MethodPut, srv.URL, strings.NewReader("body"))
	if err != nil {
		t.Fatalf("PUT req: %v", err)
	}
	put.Header.Set("Origin", opaqueOrigin)
	putResp, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusAccepted {
		t.Fatalf("PUT status = %d, want 202", putResp.StatusCode)
	}
	if got := putResp.Header.Get("Access-Control-Allow-Origin"); got != opaqueOrigin {
		t.Fatalf("PUT Allow-Origin = %q, want %q", got, opaqueOrigin)
	}

	// An arbitrary attacker origin is still refused.
	evil, err := http.NewRequest(http.MethodOptions, srv.URL, nil)
	if err != nil {
		t.Fatalf("evil req: %v", err)
	}
	evil.Header.Set("Origin", "https://evil.example.com")
	evil.Header.Set("Access-Control-Request-Method", "PUT")
	evilResp, err := http.DefaultClient.Do(evil)
	if err != nil {
		t.Fatalf("evil: %v", err)
	}
	evilResp.Body.Close()
	if got := evilResp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("arbitrary origin reflected = %q, want empty", got)
	}
}

// TestCORSDownloadOpaqueNull mirrors TestCORSOriginOpaqueNull for the filedrop
// GET route, which a host-rendered app iframe also reads cross-origin from the
// opaque "null" origin.
func TestCORSDownloadOpaqueNull(t *testing.T) {
	allowed := func() []string {
		return []string{"http://127.0.0.1:1"}
	}
	h := corsDownload(allowed, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("bytes"))
	})
	srv := httptest.NewServer(http.HandlerFunc(h))
	defer srv.Close()

	pre, err := http.NewRequest(http.MethodOptions, srv.URL, nil)
	if err != nil {
		t.Fatalf("preflight req: %v", err)
	}
	pre.Header.Set("Origin", opaqueOrigin)
	pre.Header.Set("Access-Control-Request-Method", "GET")
	preResp, err := http.DefaultClient.Do(pre)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	preResp.Body.Close()
	if got := preResp.Header.Get("Access-Control-Allow-Origin"); got != opaqueOrigin {
		t.Fatalf("preflight Allow-Origin = %q, want %q", got, opaqueOrigin)
	}

	get, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("GET req: %v", err)
	}
	get.Header.Set("Origin", opaqueOrigin)
	getResp, err := http.DefaultClient.Do(get)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getResp.StatusCode)
	}
	if got := getResp.Header.Get("Access-Control-Allow-Origin"); got != opaqueOrigin {
		t.Fatalf("GET Allow-Origin = %q, want %q", got, opaqueOrigin)
	}
}
