package catalogops

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// vaultDepsFor builds VaultDeps that serve msvc for a resolved profile. The
// profile is always supplied explicitly via the input map, so ResolveProfile
// never touches the on-disk registry.
func vaultDepsFor(msvc vault.VaultService) VaultDeps {
	return VaultDeps{
		Service: func(profileName, indexerURL string) (vault.VaultService, error) {
			return msvc, nil
		},
		ResolveIndexerURL: func() string { return "https://indexer.example.com" },
	}
}

// newVaultOps invokes a single vault catalog operation against msvc. It
// registers the Close expectation every handler satisfies.
func newVaultOps(t *testing.T, msvc vault.VaultService, name string, input map[string]any) (any, error) {
	return newVaultOpsClose(t, msvc, name, input, nil)
}

// newVaultOpsClose is newVaultOps with an optional closeSignal: when non-nil,
// the mock's Close expectation closes it on write. The non-blocking vault_flush
// handler runs its durability (and the deferred svc.Close) on a background
// goroutine, so flush tests must wait for that Close before returning — otherwise
// the goroutine races the mock's AssertExpectations cleanup.
func newVaultOpsClose(t *testing.T, msvc vault.VaultService, name string, input map[string]any, closeSignal chan struct{}) (any, error) {
	t.Helper()
	// Every vault handler defers svc.Close(); satisfy it on the mock.
	if mm, ok := msvc.(*vault.MockVaultService); ok {
		if closeSignal != nil {
			mm.On("Close").Return(nil).Run(func(mock.Arguments) { close(closeSignal) })
		} else {
			mm.On("Close").Return(nil)
		}
	}
	cat := catalog.NewCatalog()
	for _, op := range VaultOperations(vaultDepsFor(msvc)) {
		if err := cat.Add(op); err != nil {
			t.Fatalf("Add(%q): %v", op.Name(), err)
		}
	}
	return cat.Invoke(context.Background(), name, input, catalog.ActorModel)
}

// TestVaultSharePending_NoFlush guards the split between sharing and duplexing:
// sharing a not-yet-durable (pending) file must NOT block on a synchronous full
// host-set flush. It returns an actionable pending result instead of an error,
// and neither FlushPath nor Share is called.
func TestVaultSharePending_NoFlush(t *testing.T) {
	msvc := vault.NewMockVaultService(t)
	msvc.On("Stat", mock.Anything, "vault:/docs/a.txt").
		Return(&vault.StatResult{Path: "vault:/docs/a.txt", Status: vault.FileStatusPending}, nil)

	res, err := newVaultOps(t, msvc, "vault_share", map[string]any{
		"path":    "vault:/docs/a.txt",
		"profile": "work",
		"expiry":  "7d",
	})
	require.NoError(t, err, "share of a pending file must not error")
	sr, ok := res.(*VaultShareResult)
	require.True(t, ok, "want *VaultShareResult, got %T", res)
	require.Equal(t, "pending", sr.Status)
	require.Empty(t, sr.ShareURL, "no share URL for a non-durable file")
	require.Contains(t, sr.Message, "vault_flush", "message must point at the vault_flush tool")
	msvc.AssertNotCalled(t, "FlushPath", mock.Anything, mock.Anything)
	msvc.AssertNotCalled(t, "Share", mock.Anything, mock.Anything, mock.Anything)
}

// TestVaultShareDurable_IssuesLink verifies a durable file still shares normally.
func TestVaultShareDurable_IssuesLink(t *testing.T) {
	msvc := vault.NewMockVaultService(t)
	msvc.On("Stat", mock.Anything, "vault:/docs/a.txt").
		Return(&vault.StatResult{Path: "vault:/docs/a.txt", Status: vault.FileStatusOK}, nil)
	msvc.On("Share", mock.Anything, "vault:/docs/a.txt", mock.Anything).
		Return("https://indexer.example.com/shared/abc#encryption_key=K", nil)

	res, err := newVaultOps(t, msvc, "vault_share", map[string]any{
		"path":    "vault:/docs/a.txt",
		"profile": "work",
		"expiry":  "7d",
	})
	require.NoError(t, err)
	sr := res.(*VaultShareResult)
	require.Equal(t, "ok", sr.Status)
	require.Equal(t, "https://indexer.example.com/shared/abc#encryption_key=K", sr.ShareURL)
	msvc.AssertCalled(t, "Share", mock.Anything, "vault:/docs/a.txt", mock.Anything)
}

// TestVaultFlushAll verifies vault_flush without a path is non-blocking: it
// returns accepted immediately and launches a background Flush of every staged
// file (never touching FlushPath).
func TestVaultFlushAll(t *testing.T) {
	msvc := vault.NewMockVaultService(t)
	done := make(chan struct{})
	msvc.On("Flush", mock.Anything).Return(2, nil).Run(func(mock.Arguments) { close(done) })
	closeCh := make(chan struct{})

	res, err := newVaultOpsClose(t, msvc, "vault_flush", map[string]any{"profile": "work"}, closeCh)
	require.NoError(t, err)
	fr, ok := res.(*VaultFlushResult)
	require.True(t, ok, "want *VaultFlushResult, got %T", res)
	require.Equal(t, "accepted", fr.Status, "flush must return accepted, not block")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background flush did not run")
	}
	msvc.AssertCalled(t, "Flush", mock.Anything)
	msvc.AssertNotCalled(t, "FlushPath", mock.Anything, mock.Anything)
	// The background goroutine also defers svc.Close(); wait for it so the mock's
	// AssertExpectations cleanup does not race the still-running goroutine.
	select {
	case <-closeCh:
	case <-time.After(2 * time.Second):
		t.Fatal("background flush did not close the service")
	}
}

// TestVaultFlushSinglePath verifies vault_flush with a staged path is
// non-blocking: it returns accepted and launches a background FlushPath for
// that file (never touching the all-files Flush).
func TestVaultFlushSinglePath(t *testing.T) {
	msvc := vault.NewMockVaultService(t)
	msvc.On("Stat", mock.Anything, "vault:/docs/a.txt").
		Return(&vault.StatResult{Path: "vault:/docs/a.txt", Status: vault.FileStatusPending}, nil)
	done := make(chan struct{})
	msvc.On("FlushPath", mock.Anything, "vault:/docs/a.txt").Return(nil).Run(func(mock.Arguments) { close(done) })
	closeCh := make(chan struct{})

	res, err := newVaultOpsClose(t, msvc, "vault_flush", map[string]any{
		"path":    "vault:/docs/a.txt",
		"profile": "work",
	}, closeCh)
	require.NoError(t, err)
	fr := res.(*VaultFlushResult)
	require.Equal(t, "accepted", fr.Status, "flush must return accepted, not block")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background flush did not run")
	}
	msvc.AssertCalled(t, "FlushPath", mock.Anything, "vault:/docs/a.txt")
	msvc.AssertNotCalled(t, "Flush", mock.Anything)
	select {
	case <-closeCh:
	case <-time.After(2 * time.Second):
		t.Fatal("background flush did not close the service")
	}
}

// TestVaultFlushDurableNoop verifies vault_flush <path> reports idle (and skips
// FlushPath, which would be a silent no-op) when the file is already durable.
func TestVaultFlushDurableNoop(t *testing.T) {
	msvc := vault.NewMockVaultService(t)
	msvc.On("Stat", mock.Anything, "vault:/docs/a.txt").
		Return(&vault.StatResult{Path: "vault:/docs/a.txt", Status: vault.FileStatusOK}, nil)

	res, err := newVaultOps(t, msvc, "vault_flush", map[string]any{
		"path":    "vault:/docs/a.txt",
		"profile": "work",
	})
	require.NoError(t, err)
	fr := res.(*VaultFlushResult)
	require.Equal(t, "idle", fr.Status, "already-durable file must report idle")
	msvc.AssertNotCalled(t, "FlushPath", mock.Anything, mock.Anything)
	msvc.AssertNotCalled(t, "Flush", mock.Anything)
}

// TestVaultFlushLostNoop verifies vault_flush <path> reports idle for a lost
// file: it has no staged buffer, so FlushPath would be a silent no-op and no
// job should be launched.
func TestVaultFlushLostNoop(t *testing.T) {
	msvc := vault.NewMockVaultService(t)
	msvc.On("Stat", mock.Anything, "vault:/docs/a.txt").
		Return(&vault.StatResult{Path: "vault:/docs/a.txt", Status: vault.FileStatusLost, LostReason: "slabs unavailable"}, nil)

	res, err := newVaultOps(t, msvc, "vault_flush", map[string]any{
		"path":    "vault:/docs/a.txt",
		"profile": "work",
	})
	require.NoError(t, err)
	fr := res.(*VaultFlushResult)
	require.Equal(t, "idle", fr.Status, "lost file must report idle, not accepted")
	msvc.AssertNotCalled(t, "FlushPath", mock.Anything, mock.Anything)
	msvc.AssertNotCalled(t, "Flush", mock.Anything)
}

// TestVaultShareLost verifies vault_share surfaces the lost state rather than
// suggesting a flush that could never make the file shareable.
func TestVaultShareLost(t *testing.T) {
	msvc := vault.NewMockVaultService(t)
	msvc.On("Stat", mock.Anything, "vault:/docs/a.txt").
		Return(&vault.StatResult{Path: "vault:/docs/a.txt", Status: vault.FileStatusLost, LostReason: "slabs unavailable"}, nil)

	res, err := newVaultOps(t, msvc, "vault_share", map[string]any{
		"path":    "vault:/docs/a.txt",
		"profile": "work",
		"expiry":  "7d",
	})
	require.NoError(t, err)
	sr, ok := res.(*VaultShareResult)
	require.True(t, ok, "want *VaultShareResult, got %T", res)
	require.Equal(t, "lost", sr.Status)
	require.Contains(t, sr.Message, "slabs unavailable", "lost message must surface lost_reason")
	require.NotContains(t, sr.Message, "vault_flush", "lost message must not suggest a flush")
	msvc.AssertNotCalled(t, "Share", mock.Anything, mock.Anything, mock.Anything)
}
