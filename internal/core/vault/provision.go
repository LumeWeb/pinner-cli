package vault

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

// This file holds the vault provisioning lifecycle: turning a named profile
// into a provisioned, usable vault. Provisioning (create a fresh vault, or
// restore one from a recovery seed + browser approval) is UI-agnostic and must
// live here, in the core service layer, not in internal/cli. Both the CLI
// commands and the catalog/MCP layer drive this one service, so provisioning
// is never tied to a presentation surface and the CLI never needs to become
// MCP-aware (and vice versa).
//
// The Provisioner returns typed data. It never writes to an Output, never
// reads global --agent flags, and never parses another surface's stdout. The
// plaintext recovery mnemonic is handled carefully: it is persisted to a 0600
// seed file (the global safe-handoff convention) and returned in the pending
// result so an out-of-band coordinator can present it to a human in a one-time
// browser page. Consumers must never place the mnemonic on any machine text
// channel (MCP/CLI stdout/logs).

// PendingCreate is the result of provisioning a "pending" vault: a fresh
// recovery seed has been generated and a pending (empty VaultID) profile
// registered, but the vault is not yet active (browser approval is deferred to
// restore). Seed holds the plaintext mnemonic for an out-of-band coordinator
// to present; it is host-side and must never cross a machine text channel.
type PendingCreate struct {
	// Profile is the provisioned profile name.
	Profile string
	// SeedPath is the path of the 0600 recovery-seed file (the durable,
	// terminal-safe copy the human can read).
	SeedPath string
	// Seed is the plaintext recovery mnemonic. Only for out-of-band
	// presentation (e.g. a one-time browser seed_url). Never place on the
	// MCP/CLI channel.
	Seed string
}

// CreateRequest describes a vault create.
type CreateRequest struct {
	Profile    string
	DeviceName string
	// IndexerURL is the Sia indexer to register the new device against.
	IndexerURL string
	// NewConnection, when set, builds the Sia approval connection flow (a test
	// seam; defaults to NewConnectionFlow). Mirrors RestoreRequest.
	NewConnection func(indexerURL, mnemonic string) ConnectionFlow
	// OnApprovalURL, when set, is invoked with the browser approval URL after
	// the connection request is issued but before the service blocks waiting
	// for approval. Mirrors RestoreRequest.
	OnApprovalURL func(url string)
}

// CreateResult is the typed result of a successful create: the freshly
// generated, now-active profile and the plaintext recovery mnemonic. Seed is
// host-side presentation only (delivered over a one-time seed_url) and is
// never placed on the MCP/CLI channel.
type CreateResult struct {
	Profile  string
	VaultID  string
	SeedPath string
	Seed     string
}

// RestoreRequest describes a full restore completion given an already-known
// recovery mnemonic. The mnemonic may come from --seed-stdin, an interactive
// prompt, or the out-of-band browser restore form; the caller resolves it and
// passes it in. It never comes from this service to the caller.
type RestoreRequest struct {
	Profile    string
	Mnemonic   string
	IndexerURL string
	DeviceName string
	NoSync     bool
	// NewService, when set, builds the VaultService used for the post-restore
	// cache rebuild (a test seam; defaults to NewVaultServiceForProfile).
	NewService func(profileName, indexerURL string) (VaultService, error)
	// NewConnection, when set, builds the Sia approval connection flow (a test
	// seam; defaults to NewConnectionFlow). This lets tests stub the
	// network-bound approval request/registration while still exercising the
	// restore state transitions (dedup, seed consumption, profile
	// registration, cache).
	NewConnection func(indexerURL, mnemonic string) ConnectionFlow
	// OnApprovalURL, when set, is invoked with the browser approval URL after
	// the connection request is issued but before the service blocks waiting
	// for approval. The CLI uses it to print the URL; an out-of-band handler
	// may leave it nil. The service itself never writes to a presentation
	// surface.
	OnApprovalURL func(url string)
	// KeepSeed, when true, leaves the on-disk recovery seed file in place after
	// a successful completion. Restore defaults to false (it consumes the seed
	// the human supplied). Create sets it true so the freshly generated seed
	// stays as the durable backup the human is about to retrieve via seeddrop.
	KeepSeed bool
}

// ConnectionFlow is the subset of Connection needed to complete a restore:
// request a browser approval and then wait-for-approval + register on the same
// builder. It is an interface so tests can stub the network side while still
// driving the restore state transitions through the Provisioner.
type ConnectionFlow interface {
	Request(ctx context.Context) (string, error)
	WaitAndRegister(ctx context.Context) (string, error)
}

// RestoreResult is the typed completion of a restore.
type RestoreResult struct {
	Profile  string
	VaultID  string
	DeviceID string
	Device   string
	Cache    string // "ready" | "skipped" | "error"
}

// Provisioner drives the vault provisioning lifecycle (create / restore).
type Provisioner struct{}

// NewProvisioner returns a Provisioner wired to the default core primitives.
func NewProvisioner() *Provisioner { return &Provisioner{} }

// NewConnectionFlow builds a real Sia approval Connection (the default
// ConnectionFlow used by Restore). It is separated so tests can substitute a
// stub via RestoreRequest.NewConnection.
func NewConnectionFlow(indexerURL, mnemonic string) ConnectionFlow {
	return NewConnection(indexerURL, mnemonic)
}

// CreatePending provisions a named profile with a fresh recovery seed and a
// pending (empty VaultID) registry entry, with the same semantics as the
// `vault create --agent` command: generate a new mnemonic, persist it to a
// 0600 seed file before any other write (so a later failure cannot orphan the
// vault unrecoverably), then register the pending profile. It does not issue a
// connection/browser approval (that is deferred to restore, which owns the
// single approval).
//
// If a pending seed already exists for the profile, the operation fails rather
// than overwrite the human's only path back into that vault. Returns the
// pending result carrying the seed path and (host-side) mnemonic.
func (p *Provisioner) CreatePending(req CreateRequest) (*PendingCreate, error) {
	if err := ValidateProfileName(req.Profile); err != nil {
		return nil, err
	}

	// Serialize the guard, seed write, registry write, and rollback under the
	// cross-process registry lock so two concurrent creates for the same
	// profile cannot both pass the guard, write different seeds to the same
	// file, and have one clobber or roll back the other. Provisioner instances
	// are created fresh at every call site (CLI, OOB runner, catalog), so a
	// per-instance mutex would not serialize across them; the registry flock is
	// shared by every writer.
	unlock, err := lockRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to lock registry: %w", err)
	}
	defer unlock()

	reg, err := LoadRegistry() // freshest snapshot under the lock
	if err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}

	seedPath := SeedPath(req.Profile)
	if _, err := os.Stat(seedPath); err == nil {
		// A pending seed file is the human's only path into a pending vault;
		// never overwrite it. Report that it exists for the caller to resolve.
		return nil, fmt.Errorf("a pending recovery seed already exists for profile %q; restore it to complete the vault, or remove %s to start over", req.Profile, seedPath)
	}

	// A completed (non-pending) profile must not be silently overwritten by a
	// fresh create: its seed was already consumed, so the stat guard above
	// will not fire, and the upsert below would replace its VaultID/credentials.
	if prof, ok := reg.Profiles[req.Profile]; ok && prof.VaultID != "" {
		return nil, fmt.Errorf("profile %q already exists as an active vault; use a different profile name", req.Profile)
	}

	mnemonic := NewSeedPhrase()

	// Persist the fresh seed to a 0600 file immediately, before the registry
	// write, so a mid-flow failure cannot orphan the vault.
	seedDir := ProfileDir(req.Profile)
	if err := os.MkdirAll(seedDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create seed directory: %w", err)
	}
	seedData := []byte(mnemonic + "\n")
	if err := os.WriteFile(seedPath, seedData, 0600); err != nil {
		return nil, fmt.Errorf("failed to save recovery seed: %w", err)
	}

	// Record the profile as pending in the registry (already under the lock)
	// so a repeat create hits the existence guard instead of silently
	// overwriting the seed.
	reg.Profiles[req.Profile] = ProfileConfig{
		VaultID:    "",
		CachePath:  ProfileDBPath(req.Profile),
		AppKeyRef:  ProfileStatePath(req.Profile),
		DeviceName: req.DeviceName,
	}
	if reg.Default == "" {
		reg.Default = req.Profile
	}
	if err := SaveRegistry(reg); err != nil {
		// Roll the seed back so a failed registry write does not leave an
		// orphaned seed that blocks a retry (the existence guard above treats
		// a residual seed as an existing pending profile). Only remove the
		// file if its content still matches what this invocation wrote, so we
		// never delete a seed written by another create.
		if cur, statErr := os.ReadFile(seedPath); statErr == nil && string(cur) == string(seedData) {
			_ = os.Remove(seedPath)
		}
		return nil, fmt.Errorf("failed to save registry: %w", err)
	}

	return &PendingCreate{Profile: req.Profile, SeedPath: seedPath, Seed: mnemonic}, nil
}

// Restore completes a vault restore from an already-known recovery mnemonic:
// derive the vault identity, drive the single browser approval + registration,
// register a new device credential, rebuild the local cache, and consume the
// one-time seed file. This is the shared completion path used by both the CLI
// action and the out-of-band restore handler, so the two cannot drift.
//
// The approval connection (Request, WaitForApproval, then Register) runs here,
// on the same shared builder. Returns the typed result on success.
func (p *Provisioner) Restore(ctx context.Context, req RestoreRequest) (*RestoreResult, error) {
	if err := ValidateProfileName(req.Profile); err != nil {
		return nil, err
	}
	mnemonic := strings.TrimSpace(req.Mnemonic)
	if mnemonic == "" {
		return nil, fmt.Errorf("mnemonic is required")
	}
	if req.IndexerURL == "" {
		return nil, fmt.Errorf("indexer URL is required")
	}

	// Reject restoring over an already-active profile before issuing any
	// browser approval (fail-fast: avoid spending an approval on a known-active
	// profile). A pending profile (empty VaultID) is the one case restore is
	// meant to complete, so it is allowed. The authoritative guard is re-applied
	// under the registry lock in finishRestoreLocked before any local state is
	// mutated, so a profile completed concurrently after this read is not
	// overwritten.
	reg, err := LoadRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}
	if prof, ok := reg.Profiles[req.Profile]; ok && prof.VaultID != "" {
		return nil, fmt.Errorf("profile %q already exists as an active vault; restore cannot overwrite it", req.Profile)
	}

	connBuilder := req.NewConnection
	if connBuilder == nil {
		connBuilder = NewConnectionFlow
	}

	appKeyHex, vaultID, err := p.driveApproval(ctx, req, connBuilder, mnemonic)
	if err != nil {
		return nil, err
	}

	// The vault ID derives from the app key, which is only known after
	// WaitAndRegister, so a cross-profile dedup cannot run before the approval:
	// the target vault ID is indeterminate until registration returns it. The
	// authoritative cross-profile dedup therefore happens under the registry
	// lock in finishRestoreLocked, and no approval is wasted beyond the
	// unavoidable one required to derive the identity. (The same-profile guard
	// above is the one check that can legitimately fail fast pre-approval,
	// since it needs no derived ID.)

	// Commit the pending->active transition atomically. finishRestoreLocked
	// holds the registry lock across the re-check, profile-state write, DB
	// creation, and registry upsert, so a profile that became active
	// concurrently since the pre-approval guard is rejected before any of it
	// mutates local state (the loser cannot clobber the winner's credentials).
	return p.finishRestoreLocked(ctx, req, appKeyHex, vaultID)
}

// driveApproval runs the shared Sia browser-approval step used by both create
// and restore: request a connection on a single shared builder, surface the
// approval URL, then wait-for-approval + register, returning the hex app key
// and the derived vault ID. The SDK requires Request and WaitAndRegister on the
// same builder or the pending request is lost, so this owns the whole sequence.
func (p *Provisioner) driveApproval(ctx context.Context, req RestoreRequest, connBuilder func(indexerURL, mnemonic string) ConnectionFlow, mnemonic string) (appKeyHex, vaultID string, err error) {
	conn := connBuilder(req.IndexerURL, mnemonic)
	approvalURL, err := conn.Request(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to request connection: %w", err)
	}
	if req.OnApprovalURL != nil {
		req.OnApprovalURL(approvalURL)
	}
	appKeyHex, err = conn.WaitAndRegister(ctx)
	if err != nil {
		return "", "", fmt.Errorf("approval/registration failed: %w", err)
	}
	return appKeyHex, VaultID(appKeyHex), nil
}

// Create provisions and activates a new vault in one flow, symmetric with
// Restore. The only difference is seed origin: create GENERATES a fresh
// recovery mnemonic (seed OUT), restore consumes a user-supplied one (seed IN).
// Both then drive the identical Sia browser approval -> device registration ->
// atomic activation path (driveApproval + finishRestoreLocked).
//
// Create writes the fresh seed to a 0600 file before the approval (a later
// failure cannot orphan the vault unrecoverably), keeps it in place on success
// (KeepSeed), and returns it host-side so the caller can hand it to the human
// over a one-time seed_url. The plaintext mnemonic is never placed on the
// MCP/CLI channel.
func (p *Provisioner) Create(ctx context.Context, req CreateRequest) (*CreateResult, error) {
	if err := ValidateProfileName(req.Profile); err != nil {
		return nil, err
	}
	if req.IndexerURL == "" {
		return nil, fmt.Errorf("indexer URL is required")
	}

	// Reject creating over an already-active profile before issuing any
	// browser approval (fail-fast: avoid spending an approval on a known-active
	// profile). The authoritative guard is re-applied under the registry lock
	// in finishRestoreLocked before any local state is mutated.
	reg, err := LoadRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}
	if prof, ok := reg.Profiles[req.Profile]; ok && prof.VaultID != "" {
		return nil, fmt.Errorf("profile %q already exists as an active vault; use a different profile name", req.Profile)
	}
	// A pending seed file is the human's only path into a pending vault; never
	// overwrite it.
	if _, err := os.Stat(SeedPath(req.Profile)); err == nil {
		return nil, fmt.Errorf("a pending recovery seed already exists for profile %q; restore it to complete the vault, or remove %s to start over", req.Profile, SeedPath(req.Profile))
	}

	// Generate the fresh seed and persist it to a 0600 file before the remote
	// approval, so a failure mid-flow cannot orphan the vault.
	mnemonic := NewSeedPhrase()
	seedPath := SeedPath(req.Profile)
	seedDir := ProfileDir(req.Profile)
	if err := os.MkdirAll(seedDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create seed directory: %w", err)
	}
	seedData := []byte(mnemonic + "\n")
	if err := os.WriteFile(seedPath, seedData, 0600); err != nil {
		return nil, fmt.Errorf("failed to save recovery seed: %w", err)
	}

	// Drive the same approval + registration as restore.
	appKeyHex, vaultID, err := p.driveApproval(ctx, RestoreRequest{
		Profile:       req.Profile,
		IndexerURL:    req.IndexerURL,
		DeviceName:    req.DeviceName,
		NewConnection: req.NewConnection,
		OnApprovalURL: req.OnApprovalURL,
		NoSync:        true, // fresh vault has nothing to sync
		KeepSeed:      true, // leave the durable backup in place
	}, func(ixURL, mnem string) ConnectionFlow {
		if req.NewConnection != nil {
			return req.NewConnection(ixURL, mnem)
		}
		return NewConnectionFlow(ixURL, mnem)
	}, mnemonic)
	if err != nil {
		// The seed was persisted before approval; on a failure the caller can
		// surface it, but we must not leave an orphaned pending seed blocking a
		// retry unless it is the human's path in (it is a fresh, never-used
		// key, so removing it is safe and matches CreatePending's rollback).
		_ = os.Remove(SeedPath(req.Profile))
		return nil, err
	}

	// Commit the active transition atomically, reusing the restore completion.
	if _, err := p.finishRestoreLocked(ctx, RestoreRequest{
		Profile:    req.Profile,
		IndexerURL: req.IndexerURL,
		DeviceName: req.DeviceName,
		NoSync:     true,
		KeepSeed:   true,
	}, appKeyHex, vaultID); err != nil {
		// Activation failed after approval/registration. The vault was never
		// made active, so the pending-seed guard would otherwise block retries
		// forever and leave a plaintext mnemonic for a never-activated vault on
		// disk. Mirror the driveApproval cleanup above.
		_ = os.Remove(SeedPath(req.Profile))
		return nil, err
	}

	return &CreateResult{
		Profile:  req.Profile,
		VaultID:  vaultID,
		SeedPath: seedPath,
		Seed:     mnemonic,
	}, nil
}

// MarkSeedRetrieved clears a profile's KeepSeed flag and removes the kept
// create-backup seed once the human has claimed the one-time seed retrieval
// (the SeedDrop GET that displays it). The plaintext recovery mnemonic must not
// persist at rest indefinitely: it is a one-time retrieval credential, not a
// permanent backup. After retrieval it is removed immediately (same post-use
// semantics as restore), and the profile is un-marked so reconcileLocked treats
// any straggler as ordinary consumed residue. Unknown or already-un-kept
// profiles are no-ops.
func (p *Provisioner) MarkSeedRetrieved(profile string) error {
	unlock, err := lockRegistry()
	if err != nil {
		return fmt.Errorf("failed to lock registry: %w", err)
	}
	defer unlock()

	reg, err := LoadRegistry()
	if err != nil {
		return err
	}
	prof, ok := reg.Profiles[profile]
	if !ok {
		return nil // unknown profile: nothing to flip
	}
	if prof.KeepSeed {
		prof.KeepSeed = false
		reg.Profiles[profile] = prof
		if err := SaveRegistry(reg); err != nil {
			return fmt.Errorf("failed to clear keep-seed marker: %w", err)
		}
	}
	// The seed was displayed exactly once; remove the at-rest copy now rather
	// than relying on a later reconcile that may never run.
	_ = os.Remove(SeedPath(profile))
	return nil
}

// finishRestoreLocked performs the local commit that turns a pending profile
// active, under the registry lock. The browser approval and registration have
// already happened (they need no lock); everything that durably records the
// profile identity is done here so the pending->active transition is a single
// critical section shared by every writer. If a concurrent completion already
// made the profile active, the re-check below rejects before any local file is
// touched, so the winner's VaultID/device credentials are never overwritten.
func (p *Provisioner) finishRestoreLocked(ctx context.Context, req RestoreRequest, appKeyHex, vaultID string) (*RestoreResult, error) {
	unlock, err := lockRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to lock registry: %w", err)
	}
	released := false
	defer func() {
		if !released {
			unlock()
		}
	}()

	reg, err := LoadRegistry() // fresh snapshot under the lock
	if err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}
	// Self-heal any partial state left by an interrupted prior completion before
	// committing this one, so this restore never coexists with orphaned restore
	// artifacts or a consumed master seed that a crashed run failed to remove.
	reconcileLocked(reg)
	if prof, ok := reg.Profiles[req.Profile]; ok && prof.VaultID != "" {
		return nil, fmt.Errorf("profile %q already exists as an active vault; restore cannot overwrite it", req.Profile)
	}

	// Authoritative cross-profile dedup: the pre-approval loop in Restore ran
	// against a snapshot taken before the browser approval, so a concurrent
	// restore of another profile that derives the same vault ID during that
	// window is missed here. Re-run the dedup against this fresh, lock-protected
	// snapshot so the vault is never registered under two profile names.
	for name, prof := range reg.Profiles {
		if name == req.Profile {
			continue
		}
		existingID := prof.VaultID
		if derivedID, ok := ProfileVaultID(name); ok {
			existingID = derivedID
		}
		if existingID == vaultID {
			return nil, fmt.Errorf("this vault is already configured locally as profile %q", name)
		}
	}

	// Device identity.
	deviceID := uuid.NewString()
	deviceName := req.DeviceName
	if deviceName == "" {
		deviceName, _ = os.Hostname()
	}

	// Profile state.
	if err := SaveProfileState(req.Profile, &ProfileState{
		AppKey:    appKeyHex,
		DeviceID:  deviceID,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return nil, fmt.Errorf("failed to save profile state: %w", err)
	}

	// Fresh SQLite DB.
	dbPath := ProfileDBPath(req.Profile)
	db, err := OpenDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize vault database: %w", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}

	// Upsert the now-active profile under the same lock.
	reg.Profiles[req.Profile] = ProfileConfig{
		VaultID:    vaultID,
		CachePath:  dbPath,
		AppKeyRef:  ProfileStatePath(req.Profile),
		DeviceName: deviceName,
		KeepSeed:   req.KeepSeed,
	}
	if reg.Default == "" {
		reg.Default = req.Profile
	}
	if err := SaveRegistry(reg); err != nil {
		return nil, fmt.Errorf("failed to save registry: %w", err)
	}

	// The pending->active transition is committed. Release the registry lock
	// now: the cache rebuild and seed removal below operate only on profile-local
	// state (the sync writes the profile's cache.db, never the registry), so
	// holding the flock across a network-bound svc.Sync would needlessly block
	// every other registry-locked operation (CreatePending, concurrent restores,
	// profile ops) behind a potentially long remote round-trip.
	released = true
	unlock()

	// Full cache rebuild (unless NoSync).
	cacheState := "skipped"
	if !req.NoSync {
		newSvc := req.NewService
		if newSvc == nil {
			newSvc = NewVaultServiceForProfile
		}
		svc, err := newSvc(req.Profile, req.IndexerURL)
		if err != nil {
			cacheState = "error"
		} else {
			if _, _, err := svc.Sync(ctx); err != nil {
				cacheState = "error"
			} else {
				cacheState = "ready"
			}
			svc.Close()
		}
	}

	// Consume the one-time recovery seed on any successful restore. The
	// plaintext master mnemonic must not persist on disk after use. Create
	// (KeepSeed) intentionally leaves it as the durable backup the human is
	// about to retrieve.
	if !req.KeepSeed {
		_ = os.Remove(SeedPath(req.Profile))
	}

	return &RestoreResult{
		Profile:  req.Profile,
		VaultID:  vaultID,
		DeviceID: deviceID,
		Device:   deviceName,
		Cache:    cacheState,
	}, nil
}

// reconcileLocked repairs the one piece of residue an interrupted restore
// completion can leave behind that is safe to remove: a consumed recovery seed
// for an already-active profile. The registry entry with a non-empty VaultID is
// the singular commit point and source of truth for "active". Callers must hold
// the registry lock.
//
// It is deliberately conservative:
//   - Directories with no entry in reg.Profiles are never touched. A profile
//     directory is only recognized by its registry entry; scanning the data dir
//     and treating arbitrary directory names as profiles could destroy files
//     owned by another tool or a future feature, so reconciliation only ever
//     acts on registry-known profiles.
//   - For an active (VaultID != "") profile, a leftover recovery.seed holds the
//     consumed one-time mnemonic for a completed vault; the plaintext master
//     key must not linger on disk and is removed — unless the profile was
//     created with KeepSeed, in which case the seed is an intentional durable
//     backup and is preserved across activations.
//   - Pending (VaultID == "") and unregistered profiles are left entirely
//     alone, including any state.json/cache.db a restore may have partially
//     written before the registry commit. That residue is not confirmed
//     orphaned (a retry could otherwise resume from it), and the seed is the
//     human's only recovery path; the eventual restore overwrites the artifacts
//     fresh rather than risking a destructive cleanup here.
func reconcileLocked(reg *VaultRegistry) {
	dirs, err := os.ReadDir(pinnerDataDir())
	if err != nil {
		return // no vault dir yet
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		name := d.Name()
		prof, known := reg.Profiles[name]
		if !known {
			continue // never touch a directory the registry does not own
		}
		if prof.VaultID == "" {
			continue // pending: leave the seed and any partial restore state alone
		}
		// Completed profile: remove a leftover *consumed* mnemonic — except an
		// intentional create backup (KeepSeed), which is the durable recovery
		// copy that must survive subsequent activations until explicitly removed.
		if prof.KeepSeed {
			continue
		}
		_ = os.Remove(SeedPath(name))
	}
}
