//go:build integration

// Package service integration test: drives the REAL systemd user manager
// (systemctl --user) to install, start, verify, stop, and uninstall a pinner
// MCP http service, then confirms the underlying pinner MCP server actually
// comes up and serves the OAuth-protected endpoint.
//
// This is deliberately NOT part of `go test ./...` — it needs a live systemd
// user manager (a systemd-as-PID1 container or a host with an init user
// session) and a compiled pinner binary. Run it inside the systemd integration
// container via scripts/systemd-integration-test.sh, or directly with:
//
//	PINNER_BIN=/path/to/pinner go test -tags integration -run TestSystemdUserService -v ./internal/service/
package service

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSystemdUserServiceInstallServesOAuth proves the managed MCP service
// lifecycle against real systemd: Install + Start bring up a pinner mcp --http
// server that binds loopback, runs with OAuth on (MCP_OAUTH=true), and serves
// the OAuth authorization-server discovery endpoint while rejecting an
// unauthenticated /mcp request. It then stops and uninstalls cleanly.
func TestSystemdUserServiceInstallServesOAuth(t *testing.T) {
	// Guard: only meaningful on Linux with a working systemd user manager. When
	// REQUIRE_SYSTEMD=1 (set by the CI job) an unavailable manager is a hard
	// FAIL so a regression can never hide behind a silent skip; otherwise it
	// skips gracefully so local runs on non-systemd hosts are harmless.
	if !systemdAvailable(t) {
		if os.Getenv("REQUIRE_SYSTEMD") != "" {
			t.Fatal("REQUIRE_SYSTEMD is set but no systemd --user manager is reachable; the systemd-integration CI job cannot run here")
		}
		t.Skip("systemd --user manager not available; run in a systemd container")
	}

	pinnerBin := os.Getenv("PINNER_BIN")
	if pinnerBin == "" {
		t.Fatal("PINNER_BIN env required: path to a compiled pinner binary")
	}
	abs, err := filepath.Abs(pinnerBin)
	if err != nil {
		t.Fatalf("resolve PINNER_BIN: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("PINNER_BIN %q not found: %v", abs, err)
	}

	const (
		name     = "pinner-mcp-integration-test"
		port     = "18999"
		authTok  = "integration-test-secret"
		baseURL  = "http://127.0.0.1:" + port
		envfile  = "mcp-integration.env"
	)

	cfg := Config{
		Name:        name,
		Description: "pinner integration test managed MCP service",
		ExecPath:    abs,
		Arguments:   []string{"mcp", "--http"},
		EnvFile:     filepath.Join(t.TempDir(), envfile),
		UserMode:    true,
	}

	// Hermetic env: OAuth on, loopback bind, no external tunnel needed.
	if err := WriteEnvironment(cfg.EnvFile, map[string]string{
		"MCP_OAUTH":     "true",
		"MCP_AUTH_TOKEN": authTok,
		"MCP_HOST":      "127.0.0.1",
		"MCP_PORT":      port,
	}); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	svc, err := New(cfg)
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}
	ctx := context.Background()

	// Cleanup in reverse order on any path, including a mid-test failure.
	t.Cleanup(func() {
		// Stop a running service first, then uninstall (disable --now).
		_ = svc.Stop(ctx)
		_ = svc.Uninstall(ctx)
	})

	// Install/enable the user unit.
	if err := svc.Install(ctx); err != nil {
		t.Fatalf("install service: %v", err)
	}

	// The unit file must exist on disk under the user config dir.
	unitPath := filepath.Join(mustUserConfigDir(t), "systemd", "user", name+".service")
	if _, err := os.Stat(unitPath); err != nil {
		t.Fatalf("expected unit file at %s: %v", unitPath, err)
	}

	// Start it and wait for systemd to report active.
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("start service: %v", err)
	}
	waitActive(t, svc, name)

	// The pinner MCP server must actually be listening on the bind port.
	// OAuth discovery (the well-known endpoint) is served without needing
	// credentials.
	oauthURL := baseURL + "/.well-known/oauth-authorization-server"
	if !waitHTTP(t, http.MethodGet, oauthURL, http.StatusOK) {
		t.Fatalf("OAuth discovery endpoint not served: GET %s", oauthURL)
	}

	// Unauthenticated /mcp must be rejected (Bearer/OAuth auth enforced).
	if code := httpGetStatus(t, baseURL+"/mcp"); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /mcp status = %d, want 401", code)
	}
}

// systemdAvailable reports whether a usable systemd user manager exists. It
// polls because the user bus can take a moment to come up after `systemd
// --user` forks. Returns false (skip) rather than failing so the test tolerates
// hosts without one, but still hard-fails on a manager that cannot talk to the
// bus.
func systemdAvailable(t *testing.T) bool {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		// is-system-running exits non-zero while the manager is still starting
		// (or degraded) but IS reachable; both mean the user bus is up, which
		// is all we need. Only a connect error (no bus / no manager) means
		// "not available".
		_, err := runCommandOutput(context.Background(), "systemctl", "--user", "--no-pager", "list-units")
		if err == nil {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func mustUserConfigDir(t *testing.T) string {
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir: %v", err)
	}
	return dir
}

func waitActive(t *testing.T, svc Service, name string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	ctx := context.Background()
	for time.Now().Before(deadline) {
		st, err := svc.Status(ctx)
		if err == nil && st.Active {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	// Dump the unit status for debuggability before failing.
	_, _ = runCommandOutput(context.Background(), "systemctl", "--user", "status", name+".service")
	t.Fatalf("service %s did not become active within 30s", name)
}

func waitHTTP(t *testing.T, method, url string, want int) bool {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == want {
				return true
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func httpGetStatus(t *testing.T, url string) int {
	t.Helper()
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(url)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}
