package transfer

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sandboxOrigin is a stand-in for the MCP host's dynamically generated
// per-session sandbox origin: a fresh <hash> subdomain on the host's content
// CDN that is issued at connection time and therefore cannot be pre-enumerated
// in a static allow-list. It is fictional and place-holder only.
const sandboxOrigin = "https://a1b2c3d4e5f6g7h8.host-sandbox.example"

// TestCORSOriginReflectsAnyOrigin verifies that the token-gated transfer routes
// (corsUpload / corsDownload) reflect ANY request Origin — a real, dynamically
// generated MCP-host sandbox origin as well as the serialized opaque origin
// "null". These routes are gated by an unguessable, expiring, single-use token
// in the path and never send credentials, so the reflected Origin is not the
// access-control boundary; a static allow-list (which cannot enumerate the
// host's dynamic sandbox origins) is what previously blocked the host-rendered
// upload app's cross-origin XHR PUT with "No 'Access-Control-Allow-Origin'
// header".
func TestCORSOriginReflectsAnyOrigin(t *testing.T) {
	h := corsUpload(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"upload_handle":"h"}`))
	})
	srv := httptest.NewServer(http.HandlerFunc(h))
	defer srv.Close()

	// The dynamic sandbox origin (non-null, non-loopback, not pre-enumerable) is
	// reflected, and the preflight is answered with 204.
	pre, err := http.NewRequest(http.MethodOptions, srv.URL, nil)
	if err != nil {
		t.Fatalf("preflight req: %v", err)
	}
	pre.Header.Set("Origin", sandboxOrigin)
	pre.Header.Set("Access-Control-Request-Method", "PUT")
	preResp, err := http.DefaultClient.Do(pre)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	preResp.Body.Close()
	if preResp.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", preResp.StatusCode)
	}
	if got := preResp.Header.Get("Access-Control-Allow-Origin"); got != sandboxOrigin {
		t.Fatalf("preflight Allow-Origin = %q, want %q", got, sandboxOrigin)
	}
	if cc := preResp.Header.Get("Access-Control-Allow-Credentials"); cc == "true" {
		t.Fatalf("sandbox-origin upload must not send credentials")
	}

	// The actual PUT from that sandbox origin carries the reflecting header + 202.
	put, err := http.NewRequest(http.MethodPut, srv.URL, strings.NewReader("body"))
	if err != nil {
		t.Fatalf("PUT req: %v", err)
	}
	put.Header.Set("Origin", sandboxOrigin)
	putResp, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusAccepted {
		t.Fatalf("PUT status = %d, want 202", putResp.StatusCode)
	}
	if got := putResp.Header.Get("Access-Control-Allow-Origin"); got != sandboxOrigin {
		t.Fatalf("PUT Allow-Origin = %q, want %q", got, sandboxOrigin)
	}
}

// TestCORSOriginReflectsOpaqueNull is the regression test for the sandbox
// double-iframe case: an MCP host renders the app in a frame whose Origin is
// the opaque origin, serialized to the literal string "null". That origin must
// also be reflected so the app's Uppy XHR can PUT cross-origin.
func TestCORSOriginReflectsOpaqueNull(t *testing.T) {
	h := corsUpload(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"upload_handle":"h"}`))
	})
	srv := httptest.NewServer(http.HandlerFunc(h))
	defer srv.Close()

	const opaque = "null"
	pre, err := http.NewRequest(http.MethodOptions, srv.URL, nil)
	if err != nil {
		t.Fatalf("preflight req: %v", err)
	}
	pre.Header.Set("Origin", opaque)
	pre.Header.Set("Access-Control-Request-Method", "PUT")
	preResp, err := http.DefaultClient.Do(pre)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	preResp.Body.Close()
	if got := preResp.Header.Get("Access-Control-Allow-Origin"); got != opaque {
		t.Fatalf("preflight Allow-Origin = %q, want %q", got, opaque)
	}
	if cc := preResp.Header.Get("Access-Control-Allow-Credentials"); cc == "true" {
		t.Fatalf("opaque-origin upload must not send credentials")
	}

	put, err := http.NewRequest(http.MethodPut, srv.URL, strings.NewReader("body"))
	if err != nil {
		t.Fatalf("PUT req: %v", err)
	}
	put.Header.Set("Origin", opaque)
	putResp, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	putResp.Body.Close()
	if got := putResp.Header.Get("Access-Control-Allow-Origin"); got != opaque {
		t.Fatalf("PUT Allow-Origin = %q, want %q", got, opaque)
	}
}

// TestCORSDownloadReflectsAnyOrigin mirrors TestCORSOriginReflectsAnyOrigin for
// the filedrop GET route: a dynamic sandbox origin is reflected across the
// preflight and the actual GET.
func TestCORSDownloadReflectsAnyOrigin(t *testing.T) {
	h := corsDownload(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("bytes"))
	})
	srv := httptest.NewServer(http.HandlerFunc(h))
	defer srv.Close()

	pre, err := http.NewRequest(http.MethodOptions, srv.URL, nil)
	if err != nil {
		t.Fatalf("preflight req: %v", err)
	}
	pre.Header.Set("Origin", sandboxOrigin)
	pre.Header.Set("Access-Control-Request-Method", "GET")
	preResp, err := http.DefaultClient.Do(pre)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	preResp.Body.Close()
	if got := preResp.Header.Get("Access-Control-Allow-Origin"); got != sandboxOrigin {
		t.Fatalf("preflight Allow-Origin = %q, want %q", got, sandboxOrigin)
	}

	get, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("GET req: %v", err)
	}
	get.Header.Set("Origin", sandboxOrigin)
	getResp, err := http.DefaultClient.Do(get)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getResp.StatusCode)
	}
	if got := getResp.Header.Get("Access-Control-Allow-Origin"); got != sandboxOrigin {
		t.Fatalf("GET Allow-Origin = %q, want %q", got, sandboxOrigin)
	}
}
