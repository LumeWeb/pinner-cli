// Package catalogops implements vault domain operations for the operation
// catalog. Each operation drives the core vault service directly and returns
// typed data.
//
// The vault CLI is largely IO-heavy, so only the data-returning operations
// live here. The interactive and streaming commands stay hand-written in
// pkg/cli: vault create and vault restore (browser approval, seed-file and
// stdin IO, progressive handoff), vault cp (binary streaming with atomic
// temp-file rename and force-overwrite guards), and vault cat (raw binary
// stdout streaming).
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
	// Provisioner builds the vault provisioning service (create/restore).
	// Only the setup operations (vault.create / vault.restore) need it; when
	// nil they fail with a clear error. It is a getter so tests can inject a
	// stub and the MCP layer can reuse the default provisioner.
	Provisioner func() *vault.Provisioner
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
// be represented as data-returning handlers driving core services.
func VaultOperations(d VaultDeps) []catalog.Operation {
	return []catalog.Operation{
		vaultStatus(d),
		vaultLs(d),
		vaultStat(d),
		vaultVerify(d),
		vaultVersionLs(d),
		vaultVersionGet(d),
		vaultVersionRestore(d),
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
		Name:        "vault_status",
		Title:       "Vault status",
		Summary:     "Show vault profile status",
		Description: "Summarize identity, local session, remote health, storage usage, and cache health for the selected vault profile. Remote health is probed live against the indexer; local cache stats come from the profile's index. Note: the vault is read-only over this catalog (ls/stat/verify/sync/share/rm). Writing new files is not supported through searchable tools: to add a file, use the host's file-upload path instead.",
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
			// StatusResult is rendered as fields (human) or raw JSON (machine);
			// profileName is a CLI-presentation nuance resolved by the renderer.
			return svc.Status(ctx)
		}),
	})
}

// ---------------------------------------------------------------------------
// vault ls
// ---------------------------------------------------------------------------

func vaultLs(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault_ls",
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
		Name:        "vault_stat",
		Title:       "Show vault file metadata",
		Summary:     "Show file or directory metadata",
		Description: "Show metadata for a single vault path: type, size, media type, content digest, and object ID. Returns metadata only and does NOT stream file content.",
		Category:    "vault",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path>",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "Vault path to stat", AgentHelp: "The vault:/ path to report on."},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			if vaultPath == "" {
				return nil, fmt.Errorf("vault_stat: missing required argument path")
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
		Name:        "vault_verify",
		Title:       "Verify vault file integrity",
		Summary:     "Verify content integrity of a vault file",
		Description: "Check a vault file's integrity: verifies its recorded SHA-256 digest matches and that the object exists on the Sia indexer. Returns an OK/FAIL result with digest and object facts. Does NOT stream or return file content.",
		Category:    "vault",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path>",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "Vault path to verify", AgentHelp: "The vault:/ path to verify."},
			{Name: "deep", Type: catalog.ArgTypeBool, Default: "false", Help: "Download the full object and recompute SHA-256 (true integrity check; transfers the whole file)"},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			if vaultPath == "" {
				return nil, fmt.Errorf("vault_verify: missing required argument path")
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
// vault version ls / get / restore
// ---------------------------------------------------------------------------

// VaultVersion is a single version in a vault_version_ls result.
type VaultVersion struct {
	VersionID     string `json:"version_id"`
	Seq           uint   `json:"seq"`
	ObjectKey     string `json:"object_key"`
	Size          int64  `json:"size,omitempty"`
	MediaType     string `json:"media_type,omitempty"`
	ContentDigest string `json:"content_digest,omitempty"`
	IsCurrent     bool   `json:"is_current"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

// VaultVersionListResult is the data returned by vault_version_ls: the
// requested path and its version history (newest first).
type VaultVersionListResult struct {
	Path     string         `json:"path"`
	Versions []VaultVersion `json:"versions"`
}

// VaultVersionGetResult is the data returned by vault_version_get: the
// requested version's record.
type VaultVersionGetResult struct {
	Path          string `json:"path"`
	VaultVersion  `json:",inline"`
}

// VaultVersionRestoreResult is the data returned by vault_version_restore:
// the new (restored) version that became the live current winner.
type VaultVersionRestoreResult struct {
	Path        string `json:"path"`
	RestoredTo  string `json:"restored_to"` // new version_id
	ObjectKey   string `json:"object_key"`
	ContentDigest string `json:"content_digest,omitempty"`
	Size        int64  `json:"size"`
}

func vaultVersionLs(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault_version_ls",
		Title:       "List vault file versions",
		Summary:     "List version history of a vault file",
		Description: "List every stored version of a vault file, newest first (seq descending). Each version carries its version_id, size, digest, and whether it is the current live winner. Overwrites preserve prior content as versions, so this surfaces the file's full history.\n\nTo retrieve an old version's content, pass its version_id to vault_version_get (metadata) or use vault cat with a version id. To restore an old version as the new current, use vault_version_restore.",
		Category:    "vault",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path>",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "Vault path whose versions to list", AgentHelp: "The vault:/ path whose version history to list."},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			if vaultPath == "" {
				return nil, fmt.Errorf("vault_version_ls: missing required argument path")
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
			versions, err := svc.VersionList(ctx, vaultPath)
			if err != nil {
				return nil, err
			}
			res := &VaultVersionListResult{Path: vaultPath, Versions: make([]VaultVersion, 0, len(versions))}
			for _, f := range versions {
				res.Versions = append(res.Versions, VaultVersion{
					VersionID:     f.VersionID,
					Seq:           f.Seq,
					ObjectKey:     f.ObjectKey,
					Size:          f.Size,
					MediaType:     f.MediaType,
					ContentDigest: f.ContentDigest,
					IsCurrent:     f.IsCurrent,
					CreatedAt:     f.CreatedAt.UTC().Format(time.RFC3339),
					UpdatedAt:     f.UpdatedAt.UTC().Format(time.RFC3339),
				})
			}
			return res, nil
		}),
	})
}

func vaultVersionGet(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault_version_get",
		Title:       "Get vault file version",
		Summary:     "Get metadata for one version of a vault file",
		Description: "Return the metadata record (size, digest, object id, created time) for a specific version of a vault file, addressed by its version_id (obtainable from vault_version_ls). Read-only; does not stream content.\n\nTo get the CONTENT of a historical version, use vault cat with the version id on the path.",
		Category:    "vault",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path> <version_id>",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "Vault path of the file", AgentHelp: "The vault:/ path of the file."},
			{Name: "version_id", Type: catalog.ArgTypeString, Required: true, Help: "Version id to inspect (from vault_version_ls)", AgentHelp: "The version_id to inspect (from vault_version_ls)."},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			versionID := catalog.StrArg(input, "version_id", "")
			if vaultPath == "" || versionID == "" {
				return nil, fmt.Errorf("vault_version_get: missing required argument (path, version_id)")
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
			f, err := svc.VersionGet(ctx, vaultPath, versionID)
			if err != nil {
				return nil, err
			}
			return &VaultVersionGetResult{
				Path: vaultPath,
				VaultVersion: VaultVersion{
					VersionID:     f.VersionID,
					Seq:           f.Seq,
					ObjectKey:     f.ObjectKey,
					Size:          f.Size,
					MediaType:     f.MediaType,
					ContentDigest: f.ContentDigest,
					IsCurrent:     f.IsCurrent,
					CreatedAt:     f.CreatedAt.UTC().Format(time.RFC3339),
					UpdatedAt:     f.UpdatedAt.UTC().Format(time.RFC3339),
				},
			}, nil
		}),
	})
}

func vaultVersionRestore(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault_version_restore",
		Title:       "Restore a vault file version",
		Summary:     "Restore an old version as the current file",
		Description: "Restore a specific historical version of a vault file as the new live current version. The old version's content is copied and re-uploaded as a NEW version (the current winner is replaced; all prior versions, including the one restored, remain in history). Requires confirm=true (destructive to the current live content).",
		Category:    "vault",
		Safety:      catalog.SafetyDestructive,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path> <version_id>",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "Vault path of the file", AgentHelp: "The vault:/ path of the file to restore into."},
			{Name: "version_id", Type: catalog.ArgTypeString, Required: true, Help: "Version id to restore (from vault_version_ls)", AgentHelp: "The version_id to restore (from vault_version_ls)."},
			{Name: "confirm", Type: catalog.ArgTypeBool, Default: "false", Required: true, Help: "Must be true to restore (destructive to current content)", AgentHelp: "Set to true to confirm the destructive restore."},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			versionID := catalog.StrArg(input, "version_id", "")
			if vaultPath == "" || versionID == "" {
				return nil, fmt.Errorf("vault_version_restore: missing required argument (path, version_id)")
			}
			if !catalog.BoolArg(input, "confirm", false) {
				return nil, fmt.Errorf("vault_version_restore: confirm=true is required (restore is destructive to current live content)")
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
			f, err := svc.VersionRestore(ctx, vaultPath, versionID)
			if err != nil {
				return nil, err
			}
			return &VaultVersionRestoreResult{
				Path:          vaultPath,
				RestoredTo:    f.VersionID,
				ObjectKey:     f.ObjectKey,
				ContentDigest: f.ContentDigest,
				Size:          f.Size,
			}, nil
		}),
	})
}

// ---------------------------------------------------------------------------
// vault rm (destructive)
// ---------------------------------------------------------------------------

// VaultRmResult is the data returned by a successful vault.rm: the deleted
// path.
type VaultRmResult struct {
	Deleted string `json:"deleted"`
}

func vaultRm(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault_rm",
		Title:       "Delete a vault file",
		Summary:     "Delete a file from the vault",
		Description: "Permanently delete a file from the vault: removes it from both the local vault database and the Sia indexer. DESTRUCTIVE and irreversible: requires confirm=true. Targets a single file path.",
		Category:    "vault",
		Safety:      catalog.SafetyDestructive,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path>",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "Vault path to delete", AgentHelp: "The vault:/ path of the file to delete."},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to the active profile)"},
			// confirm is required on the surface and enforced here so any
			// caller (CLI --force gate, programmatic, or MCP) must confirm
			// before a file is irreversibly removed.
			{Name: "confirm", Type: catalog.ArgTypeBool, Required: true, Help: "Confirm the destructive delete", AgentHelp: "Must be true to delete the file; this is destructive and cannot be undone."},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			if vaultPath == "" {
				return nil, fmt.Errorf("vault_rm: missing required argument path")
			}
			// The CLI wiring maps --force to confirm; enforcing here guards
			// programmatic/MCP callers who bypass the CLI gate from deleting a
			// file with no confirmation state effective.
			if !catalog.BoolArg(input, "confirm", false) {
				return nil, fmt.Errorf("vault_rm: confirmation is required to remove the file")
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
		Name:        "vault_sync",
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
// (never) into a valid-until time.Time.
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
		Name:        "vault_share",
		Title:       "Share a vault file",
		Summary:     "Generate a shareable link for a vault file",
		Description: "Generate a shareable download link for a vault file. Returns the share URL and its expiry time. Control the expiry with the expiry field (e.g. 7d, 30d, 1h, or 0 for never). Does NOT upload or modify the file itself.",
		Category:    "vault",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path>",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "Vault path to share", AgentHelp: "The vault:/ path to the file to share."},
			{Name: "expiry", Type: catalog.ArgTypeString, Default: "7d", Help: "Share link expiry (e.g. 7d, 30d, 1h, or 0 for never)"},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to the active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			if vaultPath == "" {
				return nil, fmt.Errorf("vault_share: missing required argument path")
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
		Name:        "vault_forget",
		Title:       "Forget a vault profile",
		Summary:     "Remove a vault profile and its local data",
		Description: "Permanently removes a vault profile from this machine: the registry entry and its local data (state, cache DB, and any pending recovery seed) are deleted. DESTRUCTIVE and irreversible: the on-disk credential for accessing the vault is gone. Remote vault data on Sia is not deleted. Requires an explicit profile (never auto-resolves) and confirm=true to proceed.",
		Category:    "vault",
		Safety:      catalog.SafetyDestructive,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Args: []catalog.OperationArg{
			{Name: "profile", Type: catalog.ArgTypeString, Required: true, Help: "Vault profile to forget; must not auto-resolve a default", AgentHelp: "The name of the vault profile to remove. Always required; this tool never auto-resolves a default profile."},
			{Name: "confirm", Type: catalog.ArgTypeBool, Required: true, Help: "Confirm the destructive operation", AgentHelp: "Must be true to forget the profile; this permanently deletes local vault data and cannot be undone."},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			if !catalog.BoolArg(input, "confirm", false) {
				return nil, fmt.Errorf("vault_forget: confirm is required to forget a vault profile")
			}
			profileName := catalog.StrArg(input, "profile", "")
			if profileName == "" {
				return nil, fmt.Errorf("vault_forget: profile is required to forget a vault profile")
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
		Name:        "vault_profile_use",
		Title:       "Set default vault profile",
		Summary:     "Set the default profile for vault commands",
		Description: "Sets the profile used by default when neither an explicit name argument nor the PINNER_PROFILE environment variable selects one. An explicit name argument or the PINNER_PROFILE environment variable (a host-side setting, not settable by an agent) still take precedence.",
		Category:    "vault",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<name>",
		Args: []catalog.OperationArg{
			{Name: "name", Type: catalog.ArgTypeString, Required: true, Help: "Profile name to set as default", AgentHelp: "The vault profile name to set as the default for subsequent vault operations."},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			name := catalog.StrArg(input, "name", "")
			if name == "" {
				return nil, fmt.Errorf("vault_profile_use: missing required argument name")
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
// the frontend can report "no cache to clear".
type VaultCacheResult struct {
	State           string `json:"state"`
	EventsProcessed int64  `json:"events_processed,omitempty"`
	Existed         bool   `json:"-"`
}

func vaultCacheRebuild(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault_cache_rebuild",
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
		Name:        "vault_cache_clear",
		Title:       "Clear vault cache",
		Summary:     "Clear the local cache (keeps profile credentials)",
		Description: "Deletes the SQLite cache file. The next vault operation recreates an empty cache; run vault_cache_rebuild to populate it from remote.",
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
