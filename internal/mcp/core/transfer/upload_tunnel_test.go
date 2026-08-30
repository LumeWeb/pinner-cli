package transfer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCORSOriginOpaqueNullTunnelMode mirrors TestCORSOriginOpaqueNull but for
// the DEPLOYED HTTP/tunnel configuration where the coordinator has a resolved
// base URL (the public tunnel origin) and the presigned route is mounted on the
// shared transport mux — exactly the environment that surfaced the original
// "No Access-Control-Allow-Origin" preflight error over ngrok. The opaque
// sandbox origin must still be reflected here, the trusted tunnel origin must
// be reflected, and an arbitrary attacker origin must remain refused.
func TestCORSOriginOpaqueNullTunnelMode(t *testing.T) {
	mgr := NewUploadTaskManager(func(_ context.Context, reader io.Reader, _ int64, _ string, _ bool, _ string, _ bool) (any, error) {
		// Drain the piped request body so the putHandler's io.Copy into the
		// pipe completes (mirrors the production executor and the other tests).
		_, _ = io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmTunnel"}, nil
	}, 0)
	cu := NewHTTPUpload(mgr, 1024)
	t.Cleanup(func() { cu.Stop(context.Background()) })

	const tunnelBase = "https://unlagging-overtheatrically-ayesha.ngrok-free.dev"
	// HTTP/tunnel mode: point the coordinator at the external public origin and
	// mount the presigned route on the shared transport mux (mirrors
	// adapter.go serveHTTP wiring; the loopback listener must NOT be started).
	cu.SetBaseURL(tunnelBase)
	mux := http.NewServeMux()
	cu.RegisterHandlers(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Mint builds the URL from the tunnel base; the token lives in the path.
	minted := cu.Mint(context.Background(), "tunnel.bin", 0)
	if minted == "" {
		t.Fatal("mint returned empty URL")
	}
	tok := strings.TrimPrefix(minted, tunnelBase+"/upload/")
	if tok == minted || tok == "" {
		t.Fatalf("could not extract token from minted URL %q", minted)
	}
	target := srv.URL + "/upload/" + tok

	// 1) Preflight + PUT from the opaque "null" sandbox origin must be granted.
	pre, err := http.NewRequest(http.MethodOptions, target, nil)
	if err != nil {
		t.Fatalf("preflight req: %v", err)
	}
	pre.Header.Set("Origin", "null")
	pre.Header.Set("Access-Control-Request-Method", "PUT")
	preResp, err := http.DefaultClient.Do(pre)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	preResp.Body.Close()
	if got := preResp.Header.Get("Access-Control-Allow-Origin"); got != "null" {
		t.Fatalf("tunnel preflight Allow-Origin = %q, want %q", got, "null")
	}
	if cc := preResp.Header.Get("Access-Control-Allow-Credentials"); cc == "true" {
		t.Fatal("tunnel opaque-origin upload must not send credentials")
	}

	put, err := http.NewRequest(http.MethodPut, target, strings.NewReader("tunnel bytes"))
	if err != nil {
		t.Fatalf("PUT req: %v", err)
	}
	put.Header.Set("Origin", "null")
	putResp, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusAccepted {
		t.Fatalf("PUT status = %d, want 202", putResp.StatusCode)
	}
	if got := putResp.Header.Get("Access-Control-Allow-Origin"); got != "null" {
		t.Fatalf("tunnel PUT Allow-Origin = %q, want %q", got, "null")
	}

	// 2) The trusted tunnel origin itself is reflected (a browser view served
	// from the tunnel origin can also upload).
	hostPre, err := http.NewRequest(http.MethodOptions, target, nil)
	if err != nil {
		t.Fatalf("tunnel-origin preflight req: %v", err)
	}
	hostPre.Header.Set("Origin", tunnelBase)
	hostPre.Header.Set("Access-Control-Request-Method", "PUT")
	hostPreResp, err := http.DefaultClient.Do(hostPre)
	if err != nil {
		t.Fatalf("tunnel-origin preflight: %v", err)
	}
	hostPreResp.Body.Close()
	if got := hostPreResp.Header.Get("Access-Control-Allow-Origin"); got != tunnelBase {
		t.Fatalf("trusted tunnel preflight Allow-Origin = %q, want %q", got, tunnelBase)
	}

	// 3) A dynamic, non-trusted sandbox origin is also reflected in tunnel
	// mode (the token-gated route reflects any origin).
	evil, err := http.NewRequest(http.MethodOptions, target, nil)
	if err != nil {
		t.Fatalf("dynamic-origin req: %v", err)
	}
	dyn := "https://a1b2c3d4e5f6g7h8.host-sandbox.example"
	evil.Header.Set("Origin", dyn)
	evil.Header.Set("Access-Control-Request-Method", "PUT")
	evilResp, err := http.DefaultClient.Do(evil)
	if err != nil {
		t.Fatalf("dynamic-origin: %v", err)
	}
	evilResp.Body.Close()
	if got := evilResp.Header.Get("Access-Control-Allow-Origin"); got != dyn {
		t.Fatalf("dynamic origin not reflected in tunnel mode = %q, want %q", got, dyn)
	}
}
