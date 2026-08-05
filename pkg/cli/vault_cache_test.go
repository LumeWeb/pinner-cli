package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
)

// TestVaultCacheClear_NoDB asserts 'cache clear' on a profile with no cache
// DB is a no-op that reports no cache (does not error).
func TestVaultCacheClear_NoDB(t *testing.T) {
	home, err := os.MkdirTemp("", "vault-cache-clear")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	overrideHome(t, home)

	if err := vault.SaveRegistry(&vault.VaultRegistry{
		Profiles: map[string]vault.ProfileConfig{"personal": {VaultID: "vault:aaa"}},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	root := NewRootCommand()
	var buf bytes.Buffer
	root.Writer = &buf
	if err := root.Run(context.Background(), []string{"pinner", "vault", "cache", "clear", "--profile", "personal"}); err != nil {
		t.Fatalf("vault cache clear failed: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("No cache")) {
		t.Fatalf("expected no-cache message, got:\n%s", buf.String())
	}
}

// TestVaultCacheClear_RemovesDB asserts 'cache clear' removes the cache DB.
func TestVaultCacheClear_RemovesDB(t *testing.T) {
	home, err := os.MkdirTemp("", "vault-cache-clear")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	overrideHome(t, home)

	if err := vault.SaveRegistry(&vault.VaultRegistry{
		Profiles: map[string]vault.ProfileConfig{"personal": {VaultID: "vault:aaa"}},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	// Create a fake cache DB file.
	dbPath := vault.ProfileDBPath("personal")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		t.Fatalf("mkdir db dir: %v", err)
	}
	if err := os.WriteFile(dbPath, []byte("cache"), 0600); err != nil {
		t.Fatalf("write fake db: %v", err)
	}

	root := NewRootCommand()
	var buf bytes.Buffer
	root.Writer = &buf
	if err := root.Run(context.Background(), []string{"pinner", "vault", "cache", "clear", "--profile", "personal"}); err != nil {
		t.Fatalf("vault cache clear failed: %v", err)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("cache DB was not removed (stat err=%v)", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("Cache cleared")) {
		t.Fatalf("expected cleared message, got:\n%s", buf.String())
	}
}

// cacheSyncStub is a VaultService whose Sync drives the passed counter and
// full flag sequence so the rebuild loop can be exercised deterministically.
type cacheSyncStub struct {
	full    []bool // per-call full results
	n       []int  // per-call applied counts
	syncErr error  // if set, Sync returns this error
	i       int
}

func (s *cacheSyncStub) CheckReady(context.Context) error { return nil }
func (s *cacheSyncStub) Put(context.Context, io.Reader, int64, string, map[string]any) (*vault.File, error) {
	return nil, nil
}
func (s *cacheSyncStub) Get(context.Context, string, io.Writer) error { return nil }
func (s *cacheSyncStub) List(context.Context, string) ([]vault.ListItem, error) {
	return nil, nil
}
func (s *cacheSyncStub) Stat(context.Context, string) (*vault.StatResult, error) {
	return nil, nil
}
func (s *cacheSyncStub) Cat(context.Context, string, io.Writer) error { return nil }
func (s *cacheSyncStub) Verify(context.Context, string) (*vault.VerifyResult, error) {
	return nil, nil
}
func (s *cacheSyncStub) VerifyDeep(context.Context, string) (*vault.VerifyResult, error) {
	return nil, nil
}
func (s *cacheSyncStub) Remove(context.Context, string) error { return nil }
func (s *cacheSyncStub) Share(context.Context, string, time.Time) (string, error) {
	return "", nil
}
func (s *cacheSyncStub) Sync(context.Context) (int, bool, error) {
	if s.syncErr != nil {
		return 0, false, s.syncErr
	}
	i := s.i
	s.i++
	return s.n[i], s.full[i], nil
}
func (s *cacheSyncStub) Status(context.Context) (*vault.StatusResult, error) { return nil, nil }
func (s *cacheSyncStub) Close() error                                        { return nil }

// TestVaultCacheRebuild_LoopsOnFull asserts the rebuild drains until a
// non-full batch, summing applied counts (the convergence property).
func TestVaultCacheRebuild_LoopsOnFull(t *testing.T) {
	home, err := os.MkdirTemp("", "vault-cache-rebuild")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	overrideHome(t, home)

	if err := vault.SaveRegistry(&vault.VaultRegistry{
		Default: "personal",
		Profiles: map[string]vault.ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
		},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	orig := vaultServiceFactory
	t.Cleanup(func() { vaultServiceFactory = orig })

	// Three Sync calls: two full batches (of skips — count 0 but cursor moves)
	// then one non-full batch with 5 applied. Convergence requires looping past
	// the full-skip batches.
	stub := &cacheSyncStub{
		full: []bool{true, true, false},
		n:    []int{0, 0, 5},
	}
	vaultServiceFactory = func(profileName, indexerURL string) (vault.VaultService, error) {
		return stub, nil
	}

	root := NewRootCommand()
	var buf bytes.Buffer
	root.Writer = &buf
	if err := root.Run(context.Background(), []string{"pinner", "vault", "cache", "rebuild"}); err != nil {
		t.Fatalf("vault cache rebuild failed: %v", err)
	}
	if stub.i != 3 {
		t.Fatalf("Sync called %d times, want 3 (converge past full-skip batches)", stub.i)
	}
	if !bytes.Contains(buf.Bytes(), []byte("5 changes synced")) {
		t.Fatalf("expected summed count 5, got:\n%s", buf.String())
	}
}

// TestVaultCacheRebuild_RestoresOnFailure asserts that when the service cannot
// be recreated (e.g. missing app key), the moved-aside cache is restored so the
// user's prior index is not destroyed.
func TestVaultCacheRebuild_RestoresOnFailure(t *testing.T) {
	home, err := os.MkdirTemp("", "vault-cache-rebuild-restore")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	overrideHome(t, home)

	if err := vault.SaveRegistry(&vault.VaultRegistry{
		Default: "personal",
		Profiles: map[string]vault.ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
		},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	// Seed an existing cache DB.
	dbPath := vault.ProfileDBPath("personal")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		t.Fatalf("mkdir db dir: %v", err)
	}
	if err := os.WriteFile(dbPath, []byte("existing-cache"), 0600); err != nil {
		t.Fatalf("write fake db: %v", err)
	}

	orig := vaultServiceFactory
	t.Cleanup(func() { vaultServiceFactory = orig })
	// Service recreation fails — the rebuild must roll back, not orphan the DB.
	vaultServiceFactory = func(profileName, indexerURL string) (vault.VaultService, error) {
		return nil, errors.New("missing app key")
	}

	root := NewRootCommand()
	var buf bytes.Buffer
	root.Writer = &buf
	err = root.Run(context.Background(), []string{"pinner", "vault", "cache", "rebuild"})
	if err == nil {
		t.Fatalf("expected rebuild to fail when service recreation fails")
	}

	// The original cache must be restored at its original path.
	data, rerr := os.ReadFile(dbPath)
	if rerr != nil {
		t.Fatalf("original cache was not restored (read err=%v)", rerr)
	}
	if string(data) != "existing-cache" {
		t.Fatalf("restored cache content mismatch: %q", string(data))
	}
	// No stale .old file should remain.
	if _, rerr := os.Stat(dbPath + ".old"); !os.IsNotExist(rerr) {
		t.Fatalf("stale .old cache left behind after restore (stat err=%v)", rerr)
	}
}

// TestVaultCacheRebuild_RestoresOnSyncError asserts that a sync failure during
// rebuild restores the moved-aside prior cache instead of leaving an empty
// fresh DB at dbPath and stranding the good cache at .old.
func TestVaultCacheRebuild_RestoresOnSyncError(t *testing.T) {
	home, err := os.MkdirTemp("", "vault-cache-rebuild-syncerr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	overrideHome(t, home)

	if err := vault.SaveRegistry(&vault.VaultRegistry{
		Default: "personal",
		Profiles: map[string]vault.ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
		},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	// Seed an existing complete cache DB.
	dbPath := vault.ProfileDBPath("personal")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		t.Fatalf("mkdir db dir: %v", err)
	}
	if err := os.WriteFile(dbPath, []byte("complete-cache"), 0600); err != nil {
		t.Fatalf("write fake db: %v", err)
	}

	orig := vaultServiceFactory
	t.Cleanup(func() { vaultServiceFactory = orig })
	// Service recreates fine, but the first sync hits a transient network error.
	vaultServiceFactory = func(profileName, indexerURL string) (vault.VaultService, error) {
		return &cacheSyncStub{syncErr: errors.New("indexer unreachable")}, nil
	}

	root := NewRootCommand()
	var buf bytes.Buffer
	root.Writer = &buf
	err = root.Run(context.Background(), []string{"pinner", "vault", "cache", "rebuild"})
	if err == nil {
		t.Fatalf("expected rebuild to fail when sync fails")
	}

	// The prior complete cache must be restored (the fresh DB replaced).
	data, rerr := os.ReadFile(dbPath)
	if rerr != nil {
		t.Fatalf("original cache was not restored on sync failure (read err=%v)", rerr)
	}
	if string(data) != "complete-cache" {
		t.Fatalf("restored cache content mismatch on sync failure: %q", string(data))
	}
	// No stale .old file should remain after rollback.
	if _, rerr := os.Stat(dbPath + ".old"); !os.IsNotExist(rerr) {
		t.Fatalf("stale .old cache left behind after sync-failure restore (stat err=%v)", rerr)
	}
}
