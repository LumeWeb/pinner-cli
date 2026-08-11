package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// retryOnWindowsRename runs fn, retrying on the transient Windows file-lock
// race where an atomic os.Rename fails with ERROR_ACCESS_DENIED because the
// destination is momentarily held open by another handle (e.g. an armed
// fsnotify watcher on the same path). On every other platform, or for any
// non-rename access-denied error, it returns the first error immediately.
//
// This is test-only resilience: the production persist path (configmanager's
// temp-file + os.Rename) is correct but can hit Windows' exclusive-open rename
// semantics during concurrent live-reload watchers.
func retryOnWindowsRename(fn func() error) error {
	var err error
	attempts := 1
	if runtime.GOOS == "windows" {
		attempts = 5
	}
	for i := 0; i < attempts; i++ {
		err = fn()
		if err == nil || !isWindowsRenameAccessDenied(err) {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return err
}

// isWindowsRenameAccessDenied reports whether err is the Windows rename
// "Access is denied" error that arises from renaming over an open destination.
// os.Rename failures are *os.LinkError whose Err is a syscall.Errno; on Windows
// the value is ERROR_ACCESS_DENIED (Errno 5, which os maps to ErrPermission).
func isWindowsRenameAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	var le *os.LinkError
	if !errors.As(err, &le) {
		return false
	}
	if errors.Is(le.Err, syscall.EACCES) || errors.Is(le.Err, syscall.EPERM) {
		return true
	}
	if runtime.GOOS == "windows" {
		return errors.Is(le.Err, syscall.Errno(5)) // ERROR_ACCESS_DENIED
	}
	return false
}

// writeConfig writes cfg to path atomically (temp file + rename), mirroring how
// configmanager persists config (`pinner login`, SetAuthToken). Atomic replace
// produces a single filesystem Create event on the target path that koanf/fsnotify
// reliably delivers; in-place truncate+write is timing-sensitive on slow CI
// runners and has occasionally caused a flaky live-reload test.
func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := retryOnWindowsRename(func() error {
		tmp, err := os.CreateTemp(dir, ".config_tmp_*.yaml")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		if _, err := tmp.WriteString(content); err != nil {
			tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		return os.Rename(tmp.Name(), path)
	}); err != nil {
		t.Fatalf("write config over %s: %v", path, err)
	}
}

// liveReloadAwaitTimeout bounds how long a watcher-driven reload may take to
// observe an externally-written config. The poll loop itself is deterministic
// (it returns the instant the value matches); the timeout is only a fail-fast
// upper bound, so a generous value (15s) absorbs fsnotify + goroutine-scheduling
// latency on slow/loaded CI runners without weakening the live-reload assertion.
const liveReloadAwaitTimeout = 15 * time.Second

// awaitEndpoint polls m.Config().GetBaseEndpoint() until it equals want or the
// deadline passes, returning true on success. This is the deterministic way to
// observe a file-watcher-driven reload without sleeping on wall-clock guesses.
func awaitEndpoint(m Manager, want string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if m.Config().GetBaseEndpoint() == want {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// newTestManager creates a Manager over a fresh temp config file seeded with
// valid YAML, then Load()s it (which arms the configmanager file watcher).
func newTestManager(t *testing.T, seed string) (Manager, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if seed != "" {
		writeConfig(t, path, seed)
	}
	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return m, path
}

// TestManagerLiveReloadOnFileChange verifies the core live-reload property: a
// long-lived Manager (as a running pinner server holds) picks up a config file
// rewrite -- e.g. `pinner login` or a config edit writing the file from another
// process -- WITHOUT a restart. The configmanager file source arms an fsnotify
// watcher on Load, and Config() re-reads the live manager state each call.
func TestManagerLiveReloadOnFileChange(t *testing.T) {
	m, path := newTestManager(t, "base_endpoint: \"https://before.example\"\nauth_token: \"\"\nsecure: true\n")

	if got := m.Config().GetBaseEndpoint(); got != "https://before.example" {
		t.Fatalf("seed not loaded: got %q", got)
	}

	// Simulate an external writer (a separate `pinner login` process) rewriting
	// the file with a new token and endpoint.
	writeConfig(t, path, "base_endpoint: \"https://after.example\"\nauth_token: \"tok-after\"\nsecure: true\n")

	if !awaitEndpoint(m, "https://after.example", liveReloadAwaitTimeout) {
		t.Fatalf("live reload did not pick up new endpoint; still %q", m.Config().GetBaseEndpoint())
	}
	if got := m.Config().AuthToken; got != "tok-after" {
		t.Fatalf("live reload token = %q, want tok-after", got)
	}
}

// TestManagerSetAuthTokenLiveReload verifies the systemd-MCP scenario concretely:
// one "server" Manager is live and watching, while a separate "login" Manager
// writes a token via SetAuthToken (the in-process equivalent of `pinner login`
// persisting a new credential). The long-lived server Manager must observe the
// new token without a restart.
func TestManagerSetAuthTokenLiveReload(t *testing.T) {
	server, path := newTestManager(t, "auth_token: \"\"\nbase_endpoint: \"https://srv.example\"\n")

	login, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager(login): %v", err)
	}
	if err := login.SetAuthToken("tok-live"); err != nil {
		t.Fatalf("SetAuthToken: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if server.Config().AuthToken == "tok-live" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server manager did not observe token written by login manager; got %q", server.Config().AuthToken)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestManagerSyntaxErrorPreservesConfig verifies the guard rail for the
// "assuming no syntax errors" contract: a malformed edit must NOT clobber the
// running in-memory config (the file source returns before touching the manager
// on a parse error) and must not crash the manager. The last-good state stays
// authoritative until a valid edit lands.
func TestManagerSyntaxErrorPreservesConfig(t *testing.T) {
	m, path := newTestManager(t, "base_endpoint: \"https://good.example\"\nauth_token: \"\"\nsecure: true\n")

	// First land a valid change so we know the watcher is live and working.
	writeConfig(t, path, "base_endpoint: \"https://good2.example\"\nauth_token: \"\"\nsecure: true\n")
	if !awaitEndpoint(m, "https://good2.example", 5*time.Second) {
		t.Fatalf("precondition: watcher did not pick up good2; %q", m.Config().GetBaseEndpoint())
	}

	// Now write BROKEN YAML. The last-good state (good2) must be preserved.
	writeConfig(t, path, "base_endpoint: [unclosed\nauth_token: \"\"\n")

	// Give the watcher time to (attempt to) process the bad file.
	time.Sleep(500 * time.Millisecond)

	if got := m.Config().GetBaseEndpoint(); got != "https://good2.example" {
		t.Fatalf("syntax error clobbered config: got %q, want https://good2.example", got)
	}
}

// TestManagerConfigReflectsLiveManagersAcrossInstances verifies that two
// managers over the same file converge through the shared on-disk state: a
// write via one is seen by the other through the watcher, and reads always
// reflect the latest rewritten file (never a stale in-memory copy).
func TestManagerConfigReflectsLiveManagersAcrossInstances(t *testing.T) {
	m1, path := newTestManager(t, "base_endpoint: \"https://one.example\"\nauth_token: \"\"\n")
	m2, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager(m2): %v", err)
	}
	if err := m2.Load(); err != nil {
		t.Fatalf("Load(m2): %v", err)
	}

	// m2 rewrites the file; m1 (the long-lived watcher) must converge.
	// SetBaseEndpoint persists via configmanager's temp-file + os.Rename, which
	// on Windows can transiently hit "Access is denied" while m1's watcher holds
	// the destination open; retry the benign lock race.
	if err := retryOnWindowsRename(func() error { return m2.SetBaseEndpoint("https://two.example") }); err != nil {
		t.Fatalf("SetBaseEndpoint: %v", err)
	}
	if !awaitEndpoint(m1, "https://two.example", 5*time.Second) {
		t.Fatalf("m1 did not converge to m2's write; %q", m1.Config().GetBaseEndpoint())
	}
}
