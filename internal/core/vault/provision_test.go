package vault

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"go.sia.tech/core/types"

	"github.com/stretchr/testify/require"
)

// isolateVaultPaths redirects HOME/XDG_CONFIG/XDG_CACHE (and Windows dirs) to a
// temp dir so registry/seed/db writes never touch the real user config.
func isolateVaultPaths(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	return home
}

// validAppKeyHex returns a real ed25519 private-key hex (a valid VaultID input),
// distinct per call via a random seed.
func validAppKeyHex(t *testing.T) string {
	t.Helper()
	seed := make([]byte, 32)
	_, err := rand.Read(seed)
	require.NoError(t, err)
	return hex.EncodeToString(types.NewPrivateKeyFromSeed(seed))
}

// stubConn is a ConnectionFlow that returns a canned app-key hex and records
// whether the approval flow was exercised.
type stubConn struct {
	appKeyHex string
	requests  int
	waitErr   error
}

func (s *stubConn) Request(ctx context.Context) (string, error) {
	s.requests++
	return "http://approve", nil
}
func (s *stubConn) WaitAndRegister(ctx context.Context) (string, error) {
	if s.waitErr != nil {
		return "", s.waitErr
	}
	return s.appKeyHex, nil
}

// provisionStubSvc implements VaultService minimally for the post-restore cache
// rebuild path (only Sync/Close are exercised); embedding the interface left
// unimplemented methods nil-panicking if ever called, which is acceptable here.
type provisionStubSvc struct {
	VaultService
	closed bool
}

func (m *provisionStubSvc) Sync(ctx context.Context) (int, bool, error) { return 3, false, nil }
func (m *provisionStubSvc) Close() error                                { m.closed = true; return nil }

func TestProvisionerCreatePending(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()

	res, err := p.CreatePending(CreateRequest{Profile: "testpend"})
	require.NoError(t, err)
	require.Equal(t, "testpend", res.Profile)
	require.NotEmpty(t, res.Seed, "a fresh mnemonic must be generated")
	require.NotEqual(t, "testpend", res.Seed)

	// The 0600 seed file must exist and contain the mnemonic.
	seedPath := SeedPath("testpend")
	b, err := os.ReadFile(seedPath)
	require.NoError(t, err)
	require.Equal(t, strings.TrimSpace(res.Seed), strings.TrimSpace(string(b)))
	// 0600 perms (Unix only; Windows reports inherited ACLs, not POSIX bits).
	if runtime.GOOS != "windows" {
		info, err := os.Stat(seedPath)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	}

	// The profile must be registered as pending (empty VaultID).
	reg, err := LoadRegistry()
	require.NoError(t, err)
	prof, ok := reg.Profiles["testpend"]
	require.True(t, ok, "pending profile must be registered")
	require.Empty(t, prof.VaultID, "pending profile has no vault ID yet")

	// Re-creating must fail rather than overwrite the pending seed.
	_, err = p.CreatePending(CreateRequest{Profile: "testpend"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "pending recovery seed already exists")
}

func TestProvisionerCreatePendingRejectsBadProfile(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()
	_, err := p.CreatePending(CreateRequest{Profile: "../escape"})
	require.Error(t, err)
}

func TestProvisionerRestoreNoSync(t *testing.T) {
	isolateVaultPaths(t)

	// First, provision a pending profile so restore completes it.
	p := NewProvisioner()
	pend, err := p.CreatePending(CreateRequest{Profile: "testrestore"})
	require.NoError(t, err)

	appKeyHex := validAppKeyHex(t)
	vcalls := 0
	res, err := p.Restore(context.Background(), RestoreRequest{
		Profile:       "testrestore",
		Mnemonic:      pend.Seed,
		IndexerURL:    "http://indexer",
		DeviceName:    "dev1",
		NoSync:        true,
		NewConnection: func(_, _ string) ConnectionFlow { return &stubConn{appKeyHex: appKeyHex} },
		NewService:    func(_, _ string) (VaultService, error) { vcalls++; return nil, nil },
	})
	require.NoError(t, err)
	require.Equal(t, "testrestore", res.Profile)
	require.Equal(t, VaultID(appKeyHex), res.VaultID)
	require.NotEmpty(t, res.VaultID, "VaultID must derive non-empty from a valid app key")
	require.Equal(t, "dev1", res.Device)
	require.Equal(t, "skipped", res.Cache)
	require.Zero(t, vcalls, "NoSync must skip the cache rebuild service")

	// The pending seed file must be consumed.
	_, err = os.Stat(SeedPath("testrestore"))
	require.Error(t, err, "one-time seed must be removed on successful restore")

	// Profile now has a real VaultID.
	reg, err := LoadRegistry()
	require.NoError(t, err)
	require.Equal(t, res.VaultID, reg.Profiles["testrestore"].VaultID)
}

func TestProvisionerRestoreRebuildsCache(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()
	pend, err := p.CreatePending(CreateRequest{Profile: "testcache"})
	require.NoError(t, err)

	svc := &provisionStubSvc{}
	res, err := p.Restore(context.Background(), RestoreRequest{
		Profile:       "testcache",
		Mnemonic:      pend.Seed,
		IndexerURL:    "http://indexer",
		NoSync:        false,
		NewConnection: func(_, _ string) ConnectionFlow { return &stubConn{appKeyHex: validAppKeyHex(t)} },
		NewService:    func(_, _ string) (VaultService, error) { return svc, nil },
	})
	require.NoError(t, err)
	require.Equal(t, "ready", res.Cache)
	require.NotEmpty(t, res.VaultID)
	require.True(t, svc.closed, "cache rebuild service must be closed after use")
}

func TestProvisionerRestoreRequiresMnemonicAndIndexer(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()

	_, err := p.Restore(context.Background(), RestoreRequest{Profile: "x", Mnemonic: "  ", IndexerURL: "http://i"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "mnemonic is required")

	_, err = p.Restore(context.Background(), RestoreRequest{Profile: "x", Mnemonic: "seed", IndexerURL: ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "indexer URL is required")
}

func TestProvisionerRestoreSurfacesApprovalError(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()
	pend, err := p.CreatePending(CreateRequest{Profile: "fail"})
	require.NoError(t, err)

	_, err = p.Restore(context.Background(), RestoreRequest{
		Profile:       "fail",
		Mnemonic:      pend.Seed,
		IndexerURL:    "http://indexer",
		NewConnection: func(_, _ string) ConnectionFlow { return &stubConn{appKeyHex: "x", waitErr: errors.New("no approval")} },
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "approval/registration failed")
}

// TestProvisionerCreateActivatesAndReturnsSeed verifies Create drives the Sia
// approval + registration (like restore) but GENERATES a fresh seed and keeps
// it on disk for backup, then activates the profile atomically. The seed is
// returned host-side for a seed_url yet never touches the channel.
func TestProvisionerCreateActivatesAndReturnsSeed(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()
	appKeyHex := validAppKeyHex(t)

	var approvalURLs []string
	conn := &stubConn{appKeyHex: appKeyHex}
	res, err := p.Create(context.Background(), CreateRequest{
		Profile:       "created",
		IndexerURL:    "http://indexer",
		NewConnection: func(_, _ string) ConnectionFlow { return conn },
		OnApprovalURL: func(url string) { approvalURLs = append(approvalURLs, url) },
	})
	require.NoError(t, err)
	require.Equal(t, "created", res.Profile)
	require.NotEmpty(t, res.Seed, "create must generate a fresh seed to deliver out")
	require.Equal(t, 1, conn.requests, "create must issue exactly one connection request")
	require.Equal(t, []string{"http://approve"}, approvalURLs, "the approval URL must be surfaced")

	// The vault must be ACTIVE immediately (non-empty VaultID), not pending.
	reg, err := LoadRegistry()
	require.NoError(t, err)
	prof, ok := reg.Profiles["created"]
	require.True(t, ok, "created profile must be registered")
	require.Equal(t, VaultID(appKeyHex), prof.VaultID, "create must activate the vault immediately")

	// The seed file must persist for backup (KeepSeed), unlike restore.
	b, err := os.ReadFile(SeedPath("created"))
	require.NoError(t, err)
	require.Equal(t, strings.TrimSpace(res.Seed), strings.TrimSpace(string(b)))
}

// TestProvisionerCreateSurfacesApprovalError verifies a failed approval cleans
// up the freshly generated seed so a later retry is not blocked by an orphaned
// pending seed, and no active profile is left behind.
func TestProvisionerCreateSurfacesApprovalError(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()

	_, err := p.Create(context.Background(), CreateRequest{
		Profile:       "failcreate",
		IndexerURL:    "http://indexer",
		NewConnection: func(_, _ string) ConnectionFlow { return &stubConn{appKeyHex: "x", waitErr: errors.New("no approval")} },
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "approval/registration failed")

	// The generated seed must be rolled back so a retry is possible.
	_, statErr := os.Stat(SeedPath("failcreate"))
	require.True(t, os.IsNotExist(statErr), "a failed create must not leave an orphaned seed blocking a retry")
	reg, err := LoadRegistry()
	require.NoError(t, err)
	_, ok := reg.Profiles["failcreate"]
	require.False(t, ok, "a failed create must not register a profile")
}

// TestProvisionerCreateRejectsActiveProfile verifies create fails fast on an
// already-active profile without spending a browser approval.
func TestProvisionerCreateRejectsActiveProfile(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()
	// Activate a profile via restore first.
	pend, err := p.CreatePending(CreateRequest{Profile: "act"})
	require.NoError(t, err)
	_, err = p.Restore(context.Background(), RestoreRequest{
		Profile:       "act",
		Mnemonic:      pend.Seed,
		IndexerURL:    "http://indexer",
		NewConnection: func(_, _ string) ConnectionFlow { return &stubConn{appKeyHex: validAppKeyHex(t)} },
		NoSync:        true,
	})
	require.NoError(t, err)

	conn := &stubConn{appKeyHex: validAppKeyHex(t)}
	_, err = p.Create(context.Background(), CreateRequest{
		Profile:       "act",
		IndexerURL:    "http://indexer",
		NewConnection: func(_, _ string) ConnectionFlow { return conn },
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists as an active vault")
	require.Equal(t, 0, conn.requests, "no approval should be spent on a known-active profile")
}

// TestProvisionerCreateRollsBackSeedOnActivationFailure verifies that when the
// create's local activation (finishRestoreLocked) fails AFTER the Sia approval
// succeeded, the freshly generated seed is removed — mirroring the driveApproval
// failure cleanup. Otherwise the pending-seed guard would block any retry of
// that profile and leave a plaintext mnemonic for a never-activated vault on
// disk. finishRestoreLocked is forced to fail by poisoning the profile DB path.
func TestProvisionerCreateRollsBackSeedOnActivationFailure(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()
	appKeyHex := validAppKeyHex(t)

	// Poison the profile's SQLite path so OpenDB inside finishRestoreLocked
	// fails after driveApproval already succeeded (activation is the failure).
	dbPath := ProfileDBPath("actfail")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0700))
	require.NoError(t, os.WriteFile(dbPath, []byte("this is not a sqlite db"), 0600))

	conn := &stubConn{appKeyHex: appKeyHex}
	_, err := p.Create(context.Background(), CreateRequest{
		Profile:       "actfail",
		IndexerURL:    "http://indexer",
		NewConnection: func(_, _ string) ConnectionFlow { return conn },
	})
	require.Error(t, err, "activation must fail on a poisoned DB path")
	require.Equal(t, 1, conn.requests, "the approval must have been spent before activation")
	require.Contains(t, err.Error(), "failed to initialize vault database")

	// The generated seed must be rolled back so a retry of the profile is not
	// blocked by the pending-seed guard, and no active profile is left behind.
	_, statErr := os.Stat(SeedPath("actfail"))
	require.True(t, os.IsNotExist(statErr), "a failed activation must not leave an orphaned seed blocking a retry")
	reg, err := LoadRegistry()
	require.NoError(t, err)
	_, ok := reg.Profiles["actfail"]
	require.False(t, ok, "a failed activation must not register an active profile")
}

// TestProvisionerCreateSeedSurvivesReconcile verifies that a create (KeepSeed)
// backup seed is NOT deleted by reconcileLocked when a subsequent activation of
// another profile runs. reconcileLocked treats only *consumed* restore seeds as
// residue to remove; an intentional create-backup seed is the durable recovery
// copy and must survive across activations until explicitly removed.
func TestProvisionerCreateSeedSurvivesReconcile(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()

	// Create an active profile with KeepSeed (the durable-backup create flow).
	_, err := p.Create(context.Background(), CreateRequest{
		Profile:       "keepme",
		IndexerURL:    "http://indexer",
		NewConnection: func(_, _ string) ConnectionFlow { return &stubConn{appKeyHex: validAppKeyHex(t)} },
	})
	require.NoError(t, err)
	seedBytes, err := os.ReadFile(SeedPath("keepme"))
	require.NoError(t, err, "create must persist the durable backup seed")
	require.NotEmpty(t, strings.TrimSpace(string(seedBytes)))

	// Restore a second profile; Restore's finishRestoreLocked runs
	// reconcileLocked over ALL registry-known profiles, which must not remove
	// the keepme backup seed.
	pend, err := p.CreatePending(CreateRequest{Profile: "other"})
	require.NoError(t, err)
	_, err = p.Restore(context.Background(), RestoreRequest{
		Profile:       "other",
		Mnemonic:      pend.Seed,
		IndexerURL:    "http://indexer",
		DeviceName:    "dev1",
		NoSync:        true,
		NewConnection: func(_, _ string) ConnectionFlow { return &stubConn{appKeyHex: validAppKeyHex(t)} },
	})
	require.NoError(t, err)

	// The KeepSeed backup must survive the reconcile.
	after, err := os.ReadFile(SeedPath("keepme"))
	require.NoError(t, err, "a kept create-backup seed must survive a later activation's reconcile")
	require.Equal(t, strings.TrimSpace(string(seedBytes)), strings.TrimSpace(string(after)),
		"the kept seed must be byte-identical after reconcile")

	// The restore seed (consumed) must have been removed, confirming reconcile
	// still cleans up consumed restore residue.
	_, statErr := os.Stat(SeedPath("other"))
	require.True(t, os.IsNotExist(statErr), "a consumed restore seed must still be removed by reconcile")
}

// TestProvisionerCreatePendingRollsBackSeedOnRegistryFailure verifies that a
// failed registry write does not leave an orphaned seed file that would brick
// the profile (the stat guard treats a residual seed as an existing pending
// profile). The seed must be rolled back so a retry can succeed.
func TestProvisionerCreatePendingRollsBackSeedOnRegistryFailure(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()

	// Make the registry file path collide with a directory so SaveRegistry's
	// atomic rename (temp file -> vaults.yaml) fails after the seed is written,
	// while lockRegistry (which locks .vaults.lock, a different file) still
	// succeeds. Renaming a file onto a directory fails on every platform, so
	// this deterministically exercises the rollback of an already-written seed.
	cfgDir := filepath.Dir(RegistryPath())
	require.NoError(t, os.MkdirAll(cfgDir, 0700))
	regPath := RegistryPath()
	require.NoError(t, os.MkdirAll(regPath, 0700)) // a directory at the registry file path

	_, err := p.CreatePending(CreateRequest{Profile: "rollback"})
	require.Error(t, err, "registry save must fail when the registry path is a directory")

	// The orphaned seed must be rolled back so the profile is not permanently
	// unrecreatable.
	seedPath := SeedPath("rollback")
	_, statErr := os.Stat(seedPath)
	require.True(t, os.IsNotExist(statErr), "seed must be rolled back when the registry write fails; got %v", statErr)

	// Remove the blocking directory and confirm a retry succeeds (the profile
	// was not bricked).
	require.NoError(t, os.Remove(regPath))
	res, err := p.CreatePending(CreateRequest{Profile: "rollback"})
	require.NoError(t, err)
	require.NotEmpty(t, res.Seed)
}

// TestProvisionerCreatePendingSerializesConcurrentCreates verifies that
// concurrent creates for the same profile are serialized: exactly one
// succeeds, the losers fail on the seed-existence guard, and the winning seed
// file survives (a loser's rollback must not delete the winner's seed).
func TestProvisionerCreatePendingSerializesConcurrentCreates(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()

	const n = 16
	var wg sync.WaitGroup
	results := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, results[idx] = p.CreatePending(CreateRequest{Profile: "raceprofile"})
		}(i)
	}
	wg.Wait()

	var successes int
	for _, err := range results {
		if err == nil {
			successes++
			continue
		}
		require.Contains(t, err.Error(), "pending recovery seed already exists", "losers must fail on the existence guard")
	}
	require.Equal(t, 1, successes, "exactly one concurrent create must win")

	// The winning seed must still exist on disk (not deleted by any rollback).
	seedPath := SeedPath("raceprofile")
	b, err := os.ReadFile(seedPath)
	require.NoError(t, err, "winning seed must survive concurrent creates")
	require.NotEmpty(t, strings.TrimSpace(string(b)))
}

// TestProvisionerCreatePendingSerializesAcrossInstances verifies the same
// serialization holds when concurrent creates use SEPARATE Provisioner
// instances, matching how production call sites construct a fresh instance per
// invocation (CLI, OOB runner, catalog). The seed write and registry upsert
// must be serialized by the shared cross-process registry lock, not a
// per-instance mutex.
func TestProvisionerCreatePendingSerializesAcrossInstances(t *testing.T) {
	isolateVaultPaths(t)

	const n = 12
	var wg sync.WaitGroup
	results := make([]error, n)
	// Each goroutine builds its own Provisioner, as the real call sites do.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p := NewProvisioner()
			_, results[idx] = p.CreatePending(CreateRequest{Profile: "xinst"})
		}(i)
	}
	wg.Wait()

	var successes int
	for _, err := range results {
		if err == nil {
			successes++
			continue
		}
		require.Contains(t, err.Error(), "pending recovery seed already exists", "losers must fail on the existence guard")
	}
	require.Equal(t, 1, successes, "exactly one concurrent create must win across separate instances")

	_, statErr := os.Stat(SeedPath("xinst"))
	require.NoError(t, statErr, "winning seed must survive concurrent creates across instances")
}

// TestProvisionerCreatePendingRejectsActiveProfile verifies that CreatePending
// refuses to overwrite a completed (non-pending) profile whose seed was already
// consumed, protecting the active vault's VaultID and credentials.
func TestProvisionerCreatePendingRejectsActiveProfile(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()

	// Register a completed profile (non-empty VaultID), as a real restore
	// would leave behind.
	require.NoError(t, AddProfile("active", ProfileConfig{
		VaultID:    "aabbccddeeff00112233445566778899",
		CachePath:  ProfileDBPath("active"),
		AppKeyRef:  ProfileStatePath("active"),
		DeviceName: "dev",
	}))

	_, err := p.CreatePending(CreateRequest{Profile: "active"})
	require.Error(t, err, "create must reject an already-active profile")
	require.Contains(t, err.Error(), "already exists as an active vault")

	// The seed must not have been written.
	_, statErr := os.Stat(SeedPath("active"))
	require.True(t, os.IsNotExist(statErr), "no seed may be written for a rejected active profile")
}

// TestProvisionerRestoreRejectsActiveProfile verifies Restore refuses to
// overwrite an already-active profile at completion time (the counterpart to
// the mint-time guard in resolveRestoreProfile), closing the checkout-to-submit
// TOCTOU window. A pending profile (empty VaultID) is still restorable.
func TestProvisionerRestoreRejectsActiveProfile(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()

	// Register an active profile (non-empty VaultID).
	require.NoError(t, AddProfile("activerestore", ProfileConfig{
		VaultID:    "aabbccddeeff00112233445566778899",
		CachePath:  ProfileDBPath("activerestore"),
		AppKeyRef:  ProfileStatePath("activerestore"),
		DeviceName: "dev",
	}))

	_, err := p.Restore(context.Background(), RestoreRequest{
		Profile:    "activerestore",
		Mnemonic:   strings.TrimSpace("abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"),
		IndexerURL: "http://indexer",
		NoSync:     true,
		NewConnection: func(_, _ string) ConnectionFlow {
			return &stubConn{appKeyHex: "1122334455667788990011223344556677889900aabbccddeeff00112233445566"}
		},
	})
	require.Error(t, err, "restore must reject an already-active profile")
	require.Contains(t, err.Error(), "already exists as an active vault")
}

// TestProvisionerRestoreSerializesConcurrentCompletion verifies the
// pending->active transition is atomic across concurrent restores of the same
// profile: exactly one wins and the loser is rejected, and the surviving
// registry entry is the winner's, never a clobbered mix. This locks the fix for
// the check-then-act race where a pre-lock guard could pass, then a separate
// upsert could overwrite a concurrently-completed profile.
func TestProvisionerRestoreSerializesConcurrentCompletion(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()

	pend, err := p.CreatePending(CreateRequest{Profile: "conc"})
	require.NoError(t, err)

	keyA := validAppKeyHex(t)
	keyB := validAppKeyHex(t)
	require.NotEqual(t, keyA, keyB, "two distinct app keys so the vaults differ")

	results := make([]error, 2)
	var wg sync.WaitGroup
	for i, key := range []string{keyA, keyB} {
		wg.Add(1)
		go func(i int, key string) {
			defer wg.Done()
			_, err := p.Restore(context.Background(), RestoreRequest{
				Profile:    "conc",
				Mnemonic:   pend.Seed,
				IndexerURL: "http://indexer",
				NoSync:     true,
				NewConnection: func(_, _ string) ConnectionFlow {
					return &stubConn{appKeyHex: key}
				},
			})
			results[i] = err
		}(i, key)
	}
	wg.Wait()

	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
			continue
		}
		require.Contains(t, err.Error(), "already exists as an active vault", "loser must be rejected by the under-lock guard, not a partial-write")
	}
	require.Equal(t, 1, successes, "exactly one concurrent restore must win")

	// The surviving registry entry exactly matches that winner.
	reg, err := LoadRegistry()
	require.NoError(t, err)
	wantVaultID := ""
	if results[0] == nil {
		wantVaultID = VaultID(keyA)
	} else {
		wantVaultID = VaultID(keyB)
	}
	require.Equal(t, wantVaultID, reg.Profiles["conc"].VaultID)

	// State.json must be consistent with the winner (no partial clobber).
	state, err := LoadProfileState("conc")
	require.NoError(t, err)
	require.Equal(t, wantVaultID, VaultID(state.AppKey), "state.json must be consistent with the winning registry VaultID")
}

// TestProvisionerRestoreCrossProfileDedup verifies that two concurrent restores
// of different profiles deriving the same vault ID (same app key) cannot both
// commit. The pre-approval dedup runs against a stale snapshot, so the
// authoritative dedup must run under the lock in finishRestoreLocked against a
// fresh snapshot. Exactly one profile owns the vault; the other is rejected.
func TestProvisionerRestoreCrossProfileDedup(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()

	key := validAppKeyHex(t)

	results := make([]error, 2)
	var wg sync.WaitGroup
	for i, profile := range []string{"prof-a", "prof-b"} {
		wg.Add(1)
		go func(i int, profile string) {
			defer wg.Done()
			_, err := p.Restore(context.Background(), RestoreRequest{
				Profile:    profile,
				Mnemonic:   "alpha beta gamma delta epsilon zeta",
				IndexerURL: "http://indexer",
				NoSync:     true,
				NewConnection: func(_, _ string) ConnectionFlow {
					return &stubConn{appKeyHex: key}
				},
			})
			results[i] = err
		}(i, profile)
	}
	wg.Wait()

	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
			continue
		}
		require.Contains(t, err.Error(), "already configured locally as profile", "loser must be rejected by the under-lock cross-profile dedup")
	}
	require.Equal(t, 1, successes, "exactly one profile must own the vault")

	reg, err := LoadRegistry()
	require.NoError(t, err)
	wantID := VaultID(key)
	owners := 0
	for name, prof := range reg.Profiles {
		if prof.VaultID == wantID {
			owners++
			require.Contains(t, []string{"prof-a", "prof-b"}, name)
		}
	}
	require.Equal(t, 1, owners, "the vault must be registered under exactly one profile name")
}

// TestProvisionerRestoreReconcilesPartialState verifies the crash-recovery
// invariant: the registry commit is the singular source of truth for an active
// vault, and restore self-heals the one safe residue from an interrupted prior
// completion (a consumed seed for an active profile). Reconciliation is
// conservative: directories with no registry entry are never touched (so
// unregistered/profile-adjacent data is not destroyed), and a pending profile's
// seed and any partial restore state are preserved.
func TestProvisionerRestoreReconcilesPartialState(t *testing.T) {
	isolateVaultPaths(t)
	p := NewProvisioner()

	// Establish an active profile "alph" via a real create+restore.
	pend, err := p.CreatePending(CreateRequest{Profile: "alph"})
	require.NoError(t, err)
	_, err = p.Restore(context.Background(), RestoreRequest{
		Profile:    "alph",
		Mnemonic:   pend.Seed,
		IndexerURL: "http://indexer",
		NoSync:     true,
		NewConnection: func(_, _ string) ConnectionFlow {
			return &stubConn{appKeyHex: validAppKeyHex(t)}
		},
	})
	require.NoError(t, err)

	// Simulate a crash after the registry commit but before the seed removal:
	// the active profile is left with a consumed recovery.seed on disk.
	alphSeed := SeedPath("alph")
	require.NoError(t, os.WriteFile(alphSeed, []byte("consumed mnemonic\n"), 0600))
	if runtime.GOOS != "windows" {
		require.FileExists(t, alphSeed)
	}

	// An unregistered directory (no registry entry) with restore-looking files
	// must be left untouched: reconciliation never treats arbitrary directories
	// as profiles.
	orphanState := ProfileStatePath("orphan")
	orphanDB := ProfileDBPath("orphan")
	require.NoError(t, os.MkdirAll(filepath.Dir(orphanState), 0700))
	require.NoError(t, os.WriteFile(orphanState, []byte(`{"AppKey":"abc"}`), 0600))
	require.NoError(t, os.MkdirAll(filepath.Dir(orphanDB), 0700))
	require.NoError(t, os.WriteFile(orphanDB, []byte("sqlite"), 0600))

	// A pending profile "bett" with a seed must keep that seed.
	pendB, err := p.CreatePending(CreateRequest{Profile: "bett"})
	require.NoError(t, err)
	bettSeed := SeedPath("bett")
	require.FileExists(t, bettSeed)

	// Run another restore; finishRestoreLocked reconciles before committing.
	_, err = p.Restore(context.Background(), RestoreRequest{
		Profile:    "gamm",
		Mnemonic:   "alpha beta gamma delta epsilon zeta",
		IndexerURL: "http://indexer",
		NoSync:     true,
		NewConnection: func(_, _ string) ConnectionFlow {
			return &stubConn{appKeyHex: validAppKeyHex(t)}
		},
	})
	require.NoError(t, err)

	// The unregistered directory is preserved (not destroyed).
	require.FileExists(t, orphanState, "unregistered profile state must not be deleted")
	require.FileExists(t, orphanDB, "unregistered profile cache must not be deleted")

	// The active profile's consumed seed is gone.
	_, err = os.Stat(alphSeed)
	require.True(t, os.IsNotExist(err), "consumed seed for an active profile must be removed")

	// The pending profile's seed and any restore artifacts are preserved.
	require.FileExists(t, bettSeed)
	require.Equal(t, pendB.Seed+"\n", string(mustReadFile(t, bettSeed)))
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return b
}
