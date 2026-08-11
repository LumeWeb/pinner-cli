// Package catalogops wires the canonical operation catalog (internal/catalog)
// to the extracted core service domains. This file adds the vault domain:
// catalog operations that drive internal/core/vault (the VaultService and the
// profile/registry helpers). Every Handler returns typed core DATA and never
// renders — see the package doc on pins.go for the overall do/render split.
//
// Import rule (architectural invariant): this package may import
// internal/catalog and internal/core/* but NEVER pkg/cli.
//
// SPLIT DECISIONS (faithfulness vs. the data-returning contract)
//
// The vault CLI is a large, IO-heavy domain. The commands below are the ones
// that CAN be faithfully represented as pure data-returning operations driving
// core services (they read state, mutate the profile registry, or drive the
// VaultService and return typed results). Commands that are fundamentally
// interactive/IO-coupled are NOT ported here and remain hand-written in
// pkg/cli (see the per-domain note at the bottom):
//
//   - vault create   — interactive browser-approval flow + writing the
//     recovery seed to a 0600 file + progressive JSON handoff. The seed file
//     IO and the browser handoff are inherent CLI presentation concerns that
//     cannot be expressed as a data-returning handler without losing the
//     seed-on-disk safety property. Kept hand-written.
//   - vault restore  — same interactive browser approval + stdin seed read +
//     progressive handoff. Kept hand-written.
//   - vault cp       — binary streaming between the local filesystem and
//     vault(s) with atomic temp-file rename, progress, and force-overwrite
//     guards. Fundamentally IO-coupled. Kept hand-written.
//   - vault cat      — raw binary stdout streaming (and agent-mode base64
//     buffering). Fundamentally IO-coupled. Kept hand-written.
package catalogops

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// VaultDeps are the dependencies the vault operations need at construction
// time. They are getters/closures so service construction always uses fresh
// live values, never a package-init snapshot (the lazy-deps pattern).
type VaultDeps struct {
	// Service builds a VaultService for a resolved profile and indexer URL.
	// It is a getter closure so it can honor a test/global override of the
	// underlying factory at invocation time. When nil the operations that
	// need a service fail with a clear error.
	Service func(profileName, indexerURL string) (vault.VaultService, error)
	// ResolveIndexerURL returns the Sia indexer URL from config for the
	// current invocation. When nil and a service is needed, operations fail
	// with a clear error.
	ResolveIndexerURL func() string
}

// service builds the VaultService for the given profile, resolving the
// indexer URL from config.
func (d VaultDeps) service(profileName string) (vault.VaultService, error) {
	if d.Service == nil {
		return nil, fmt.Errorf("vault: no service factory wired")
	}
	if d.ResolveIndexerURL == nil {
		return nil, fmt.Errorf("vault: no indexer URL resolver wired")
	}
	indexerURL := d.ResolveIndexerURL()
	return d.Service(profileName, indexerURL)
}

// VaultOperations returns the catalog operations for the vault domain that can
// be faithfully expressed as data-returning handlers driving core services.
func VaultOperations(d VaultDeps) []catalog.Operation {
	return []catalog.Operation{
		vaultStatus(d),
		vaultLs(d),
		vaultStat(d),
		vaultVerify(d),
		vaultRm(d),
		vaultSync(d),
		vaultShare(d),
		vaultForget(d),
		vaultProfileUse(d),
		vaultCacheRebuild(d),
		vaultCacheClear(d),
	}
}

// ---------------------------------------------------------------------------
// vault status
// ---------------------------------------------------------------------------

func vaultStatus(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault.status",
		Title:       "Vault status",
		Summary:     "Show vault profile status",
		Description: "Summarize identity, local session, remote health, storage usage, and cache health for the selected vault profile. Remote health is probed live against the indexer; local cache stats come from the profile's index.",
		Category:    "vault",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Args: []catalog.OperationArg{
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			profileName, err := vault.ResolveProfile(catalog.StrArg(input, "profile", ""))
			if err != nil {
				return nil, err
			}
			svc, err := d.service(profileName)
			if err != nil {
				return nil, err
			}
			defer svc.Close()
			// *vault.StatusResult — the CLI renders it as fields (human) or
			// the raw JSON (machine). profileName is a CLI-presentation nuance
			// and is resolved by the renderer.
			return svc.Status(ctx)
		}),
	})
}

// ---------------------------------------------------------------------------
// vault ls
// ---------------------------------------------------------------------------

func vaultLs(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault.ls",
		Title:       "List vault files",
		Summary:     "List files and directories in the vault",
		Description: "List files and directories at the given vault path (name, type, size, and created time). If no path is provided, lists the root directory. Lists one level only (no recursion).",
		Category:    "vault",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path>",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Help: "Vault path to list (e.g. vault:/reports; defaults to the root)"},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			if vaultPath == "" {
				vaultPath = vault.VaultRoot
			}
			profileName, err := vault.ResolveProfile(catalog.StrArg(input, "profile", ""))
			if err != nil {
				return nil, err
			}
			svc, err := d.service(profileName)
			if err != nil {
				return nil, err
			}
			defer svc.Close()
			// []vault.ListItem
			return svc.List(ctx, vaultPath)
		}),
	})
}

// ---------------------------------------------------------------------------
// vault stat
// ---------------------------------------------------------------------------

func vaultStat(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault.stat",
		Title:       "Show vault file metadata",
		Summary:     "Show file or directory metadata",
		Description: "Show metadata for a single vault path: type, size, media type, content digest, and object ID. Returns metadata only and does NOT stream file content.",
		Category:    "vault",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path>",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "Vault path to stat (positional)"},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			if vaultPath == "" {
				return nil, fmt.Errorf("vault.stat: missing required argument path")
			}
			profileName, err := vault.ResolveProfile(catalog.StrArg(input, "profile", ""))
			if err != nil {
				return nil, err
			}
			svc, err := d.service(profileName)
			if err != nil {
				return nil, err
			}
			defer svc.Close()
			// *vault.StatResult
			return svc.Stat(ctx, vaultPath)
		}),
	})
}

// ---------------------------------------------------------------------------
// vault verify
// ---------------------------------------------------------------------------

func vaultVerify(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault.verify",
		Title:       "Verify vault file integrity",
		Summary:     "Verify content integrity of a vault file",
		Description: "Check a vault file's integrity: verifies its recorded SHA-256 digest matches and that the object exists on the Sia indexer. Returns an OK/FAIL result with digest and object facts. Does NOT stream or return file content.",
		Category:    "vault",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path>",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "Vault path to verify (positional)"},
			{Name: "deep", Type: catalog.ArgTypeBool, Default: "false", Help: "Download the full object and recompute SHA-256 (true integrity check; transfers the whole file)"},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			if vaultPath == "" {
				return nil, fmt.Errorf("vault.verify: missing required argument path")
			}
			profileName, err := vault.ResolveProfile(catalog.StrArg(input, "profile", ""))
			if err != nil {
				return nil, err
			}
			svc, err := d.service(profileName)
			if err != nil {
				return nil, err
			}
			defer svc.Close()
			if catalog.BoolArg(input, "deep", false) {
				// *vault.VerifyResult (full-content deep check)
				return svc.VerifyDeep(ctx, vaultPath)
			}
			// *vault.VerifyResult (cheap metadata-declared digest check)
			return svc.Verify(ctx, vaultPath)
		}),
	})
}

// ---------------------------------------------------------------------------
// vault rm (destructive)
// ---------------------------------------------------------------------------

// VaultRmResult is the data returned by a successful vault.rm: the deleted
// path. Pure data — the CLI renders it, the MCP client consumes it.
type VaultRmResult struct {
	Deleted string `json:"deleted"`
}

func vaultRm(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault.rm",
		Title:       "Delete a vault file",
		Summary:     "Delete a file from the vault",
		Description: "Permanently delete a file from the vault: removes it from both the local vault database and the Sia indexer. DESTRUCTIVE and irreversible: requires --force (agent mode always requires --force). Targets a single file path.",
		Category:    "vault",
		Safety:      catalog.SafetyDestructive,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path>",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "Vault path to delete (positional)"},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
			// confirm is threaded so programmatic callers can pass it
			// explicitly; the CLI wiring enforces --force via the destructive
			// gate.
			{Name: "confirm", Type: catalog.ArgTypeBool, Default: "false", Help: "Confirm the destructive operation (CLI maps --force here)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			if vaultPath == "" {
				return nil, fmt.Errorf("vault.rm: missing required argument path")
			}
			// The CLI wiring maps --force to confirm; enforcing here guards
			// programmatic/MCP callers who bypass the CLI gate from deleting a
			// file with no confirmation state effective.
			if !catalog.BoolArg(input, "confirm", false) {
				return nil, fmt.Errorf("vault.rm requires confirmation (pass --force/confirm)")
			}
			profileName, err := vault.ResolveProfile(catalog.StrArg(input, "profile", ""))
			if err != nil {
				return nil, err
			}
			svc, err := d.service(profileName)
			if err != nil {
				return nil, err
			}
			defer svc.Close()
			if err := svc.Remove(ctx, vaultPath); err != nil {
				return nil, err
			}
			return &VaultRmResult{Deleted: vaultPath}, nil
		}),
	})
}

// ---------------------------------------------------------------------------
// vault sync
// ---------------------------------------------------------------------------

// VaultSyncResult is the data returned by a successful vault sync: the number
// of events processed (the loop drains full batches so the count is the total
// applied across all drained batches).
type VaultSyncResult struct {
	EventsProcessed int `json:"events_processed"`
}

func vaultSync(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault.sync",
		Title:       "Sync vault cache from indexer",
		Summary:     "Sync local vault cache from indexer",
		Description: "Pull incremental changes from the Sia indexer into the local vault cache using an event cursor. Loops while a fetched batch is full so the cache converges even when >100 changes accumulate. Returns the number of events processed. Does NOT upload or delete any files.",
		Category:    "vault",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Args: []catalog.OperationArg{
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			profileName, err := vault.ResolveProfile(catalog.StrArg(input, "profile", ""))
			if err != nil {
				return nil, err
			}
			svc, err := d.service(profileName)
			if err != nil {
				return nil, err
			}
			defer svc.Close()
			// Sync fetches one batch of 100 per call and reports whether the
			// batch was full. Loop while full so the cache converges; the
			// cursor advances even across all-skip batches.
			count, full, err := svc.Sync(ctx)
			for err == nil && full {
				var n int
				n, full, err = svc.Sync(ctx)
				count += n
			}
			if err != nil {
				return nil, err
			}
			return &VaultSyncResult{EventsProcessed: count}, nil
		}),
	})
}

// ---------------------------------------------------------------------------
// vault share
// ---------------------------------------------------------------------------

// VaultShareResult is the data returned by a successful vault share: the
// share URL and its expiry timestamp.
type VaultShareResult struct {
	ShareURL string `json:"share_url"`
	Expires  string `json:"expires"`
}

// parseVaultExpiry parses a duration string like "7d", "30d", "1h", "0"
// (never) into a valid-until time.Time. Faithfully ported from the legacy CLI
// helper so the operation is self-contained and presentation-neutral.
func parseVaultExpiry(s string) (time.Time, error) {
	if s == "0" || s == "never" {
		return time.Now().AddDate(100, 0, 0), nil
	}
	if len(s) > 0 && s[len(s)-1] == 'd' {
		days, err := strconv.Atoi(s[:len(s)-1])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid expiry: %s", s)
		}
		if days <= 0 {
			return time.Time{}, fmt.Errorf("expiry days must be positive: %s", s)
		}
		return time.Now().AddDate(0, 0, days), nil
	}
	du, err := time.ParseDuration(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid expiry format: %w (use e.g. 7d, 30d, 1h, 0 for never)", err)
	}
	if du <= 0 {
		return time.Time{}, fmt.Errorf("expiry must be in the future: %s", s)
	}
	return time.Now().Add(du), nil
}

func vaultShare(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault.share",
		Title:       "Share a vault file",
		Summary:     "Generate a shareable link for a vault file",
		Description: "Generate a shareable download link for a vault file. Returns the share URL and its expiry time. Control the expiry with --expiry (e.g. 7d, 30d, 1h, or 0 for never). Does NOT upload or modify the file itself.",
		Category:    "vault",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path>",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "Vault path to share (positional)"},
			{Name: "expiry", Type: catalog.ArgTypeString, Default: "7d", Help: "Share link expiry (e.g. 7d, 30d, 1h, or 0 for never)"},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			if vaultPath == "" {
				return nil, fmt.Errorf("vault.share: missing required argument path")
			}
			validUntil, err := parseVaultExpiry(catalog.StrArg(input, "expiry", "7d"))
			if err != nil {
				return nil, err
			}
			profileName, err := vault.ResolveProfile(catalog.StrArg(input, "profile", ""))
			if err != nil {
				return nil, err
			}
			svc, err := d.service(profileName)
			if err != nil {
				return nil, err
			}
			defer svc.Close()
			shareURL, err := svc.Share(ctx, vaultPath, validUntil)
			if err != nil {
				return nil, err
			}
			return &VaultShareResult{ShareURL: shareURL, Expires: validUntil.Format(time.RFC3339)}, nil
		}),
	})
}

// ---------------------------------------------------------------------------
// vault forget (destructive)
// ---------------------------------------------------------------------------

// VaultForgetResult is the data returned by a successful vault forget: the
// profile name that was removed.
type VaultForgetResult struct {
	Profile string `json:"profile"`
	State   string `json:"state"`
}

func vaultForget(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault.forget",
		Title:       "Forget a vault profile",
		Summary:     "Remove a vault profile and its local data",
		Description: "Permanently removes a vault profile from this machine: the registry entry and its local data (state, cache DB, and any pending recovery seed) are deleted. DESTRUCTIVE and irreversible: the on-disk credential for accessing the vault is gone. Remote vault data on Sia is not deleted. Requires an explicit --profile (never auto-resolves) and --force to confirm.",
		Category:    "vault",
		Safety:      catalog.SafetyDestructive,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Args: []catalog.OperationArg{
			{Name: "profile", Type: catalog.ArgTypeString, Required: true, Help: "Vault profile to forget (required; must not auto-resolve a default)"},
			{Name: "confirm", Type: catalog.ArgTypeBool, Default: "false", Help: "Confirm the destructive operation (CLI maps --force here)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			if !catalog.BoolArg(input, "confirm", false) {
				return nil, fmt.Errorf("vault.forget requires confirmation (pass --force/confirm)")
			}
			profileName := catalog.StrArg(input, "profile", "")
			if profileName == "" {
				return nil, fmt.Errorf("vault.forget: --profile <name> is required to forget a vault profile")
			}
			if err := vault.RemoveProfile(profileName); err != nil {
				return nil, err
			}
			return &VaultForgetResult{Profile: profileName, State: "forgotten"}, nil
		}),
	})
}

// ---------------------------------------------------------------------------
// vault profile use
// ---------------------------------------------------------------------------

// VaultProfileUseResult is the data returned by a successful profile use:
// the name that was set as default.
type VaultProfileUseResult struct {
	Profile string `json:"profile"`
}

func vaultProfileUse(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault.profile.use",
		Title:       "Set default vault profile",
		Summary:     "Set the default profile for vault commands",
		Description: "Sets the profile used by default when neither --profile nor the PINNER_PROFILE env var selects one. An explicit --profile or PINNER_PROFILE still take precedence.",
		Category:    "vault",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<name>",
		Args: []catalog.OperationArg{
			{Name: "name", Type: catalog.ArgTypeString, Required: true, Help: "Profile name to set as default (positional)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			name := catalog.StrArg(input, "name", "")
			if name == "" {
				return nil, fmt.Errorf("vault.profile.use: missing required argument name")
			}
			if err := vault.ValidateProfileName(name); err != nil {
				return nil, err
			}
			if err := vault.SetDefaultProfile(name); err != nil {
				return nil, err
			}
			return &VaultProfileUseResult{Profile: name}, nil
		}),
	})
}

// ---------------------------------------------------------------------------
// vault cache rebuild / clear
// ---------------------------------------------------------------------------

// VaultCacheResult is the data returned by a vault cache operation: the
// outcome state ("rebuilt", "cleared") and, for rebuild, the number of changes
// synced. Existed records whether a cache DB was present before a clear, so
// the frontend can report "no cache to clear" faithfully (JSON keeps just
// state, matching the legacy wire shape).
type VaultCacheResult struct {
	State           string `json:"state"`
	EventsProcessed int64  `json:"events_processed,omitempty"`
	Existed         bool   `json:"-"`
}

func vaultCacheRebuild(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault.cache.rebuild",
		Title:       "Rebuild vault cache",
		Summary:     "Rebuild the cache from remote state",
		Description: "Discards the local SQLite index and re-syncs all metadata from the Sia indexer. File content is not re-downloaded; only the index is rederived. The prior cache is set aside (not deleted) and restored if the rebuild fails. Use to repair a corrupted or stale local cache.",
		Category:    "vault",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Args: []catalog.OperationArg{
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			profileName, err := vault.ResolveProfile(catalog.StrArg(input, "profile", ""))
			if err != nil {
				return nil, err
			}
			reg, err := vault.LoadRegistry()
			if err != nil {
				return nil, err
			}
			if _, exists := reg.Profiles[profileName]; !exists {
				return nil, fmt.Errorf("profile %q not found", profileName)
			}

			// Move the existing index aside (don't delete) so the cursor
			// resets and the rebuild re-syncs the ENTIRE object. Rename is
			// reversible: restore it if the rebuild cannot complete.
			dbPath := vault.ProfileDBPath(profileName)
			oldPath := dbPath + ".old"
			moved := false
			if err := os.Rename(dbPath, oldPath); err == nil {
				moved = true
			} else if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to set aside old cache: %w", err)
			}

			// Rebuild creates a brand-new empty cache; migrate it explicitly
			// before the service opens it, or the tables sync writes into
			// won't exist.
			restore := func() {
				if moved {
					_ = os.Remove(dbPath)
					_ = os.Rename(oldPath, dbPath)
				}
			}
			if db, err := vault.OpenDB(dbPath); err != nil {
				restore()
				return nil, fmt.Errorf("failed to initialize rebuild cache: %w", err)
			} else if sqlDB, err := db.DB(); err != nil {
				// OpenDB succeeded but we couldn't get the underlying *sql.DB.
				// The gorm.DB handle has no Close method; the pool is reclaimed
				// on GC. The important part is restore(): drop the partial new
				// cache and put the old one back, so we never proceed on a
				// half-built index.
				restore()
				return nil, fmt.Errorf("failed to initialize rebuild cache handle: %w", err)
			} else {
				_ = sqlDB.Close()
			}

			svc, err := d.service(profileName)
			if err != nil {
				restore()
				return nil, fmt.Errorf("failed to recreate cache: %w", err)
			}

			var count int
			var full bool
			count, full, err = svc.Sync(ctx)
			for err == nil && full {
				var n int
				n, full, err = svc.Sync(ctx)
				count += n
			}
			// Close the fresh service handle BEFORE restoring the old cache
			// (Windows cannot rename a file with an open handle).
			_ = svc.Close()
			if err != nil {
				restore()
				return nil, fmt.Errorf("sync during rebuild failed: %w", err)
			}
			// Rebuild succeeded; discard the rolled-aside old cache.
			if moved {
				_ = os.Remove(oldPath)
			}
			return &VaultCacheResult{State: "rebuilt", EventsProcessed: int64(count)}, nil
		}),
	})
}

func vaultCacheClear(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault.cache.clear",
		Title:       "Clear vault cache",
		Summary:     "Clear the local cache (keeps profile credentials)",
		Description: "Deletes the SQLite cache file. The next vault operation recreates an empty cache; run 'pinner vault cache rebuild' to populate it from remote.",
		Category:    "vault",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Args: []catalog.OperationArg{
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			profileName, err := vault.ResolveProfile(catalog.StrArg(input, "profile", ""))
			if err != nil {
				return nil, err
			}
			reg, err := vault.LoadRegistry()
			if err != nil {
				return nil, err
			}
			if _, exists := reg.Profiles[profileName]; !exists {
				return nil, fmt.Errorf("profile %q not found", profileName)
			}
			dbPath := vault.ProfileDBPath(profileName)
			existed := false
			if err := os.Remove(dbPath); err == nil {
				existed = true
			} else if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to clear cache: %w", err)
			}
			return &VaultCacheResult{State: "cleared", Existed: existed}, nil
		}),
	})
}
