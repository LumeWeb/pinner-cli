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
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/samber/lo"

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
	// FlushMgr builds the per-profile flush manager for the running server.
	// When nil, vault_flush falls back to a detached single-use goroutine and
	// vault_flush_status / vault_send fail with a clear error.
	FlushMgr func() *vault.FlushManager
	// Profiles returns every provisioned/unlocked profile name (empty slice
	// when none). When nil it degrades to reading the registry.
	Profiles func() []string
}

// unlockedProfiles returns every provisioned profile name the server can see,
// for the vault_profiles tool and the multi-profile profile-required rule.
func (d VaultDeps) unlockedProfiles() []string {
	if d.Profiles != nil {
		return d.Profiles()
	}
	// Fallback: load the registry directly.
	if reg, err := vault.LoadRegistry(); err == nil {
		out := make([]string, 0, len(reg.Profiles))
		for n := range reg.Profiles {
			out = append(out, n)
		}
		return out
	}
	return nil
}

// VaultProfileRequiredResult is returned when a multi-profile server receives a
// vault op without an explicit profile. It is a structured error (not the
// silent/active-vault default, and not a misleading "vault object not found").
type VaultProfileRequiredResult struct {
	Code     string   `json:"code"` // "profile_required"
	Profiles []string `json:"profiles,omitempty"`
	Message  string   `json:"message"`
}

// profileRequired returns a *VaultProfileRequiredResult when the profile arg is
// absent AND more than one profile is unlocked; otherwise nil. It is the guard
// applied at the top of every multi-profile-sensitive vault op.
func (d VaultDeps) profileRequired(profileArg string) *VaultProfileRequiredResult {
	if profileArg != "" {
		return nil
	}
	profiles := d.unlockedProfiles()
	if len(profiles) > 1 {
		return &VaultProfileRequiredResult{
			Code:     "profile_required",
			Profiles: profiles,
			Message:  "more than one vault profile is unlocked; pass profile=<name>",
		}
	}
	return nil
}

// service builds the VaultService for the given profile, resolving the
// indexer URL from config.
func (d VaultDeps) service(profileName string) (vault.VaultService, error) {
	indexerURL, err := d.resolveIndexerURL()
	if err != nil {
		return nil, err
	}
	return d.Service(profileName, indexerURL)
}

// ServiceForProfile is the exported form of service: it builds a VaultService
// for the given profile (resolving the indexer URL from config). Public so the
// CLI wiring can drive a synchronous flush without depending on catalogops
// internals.
func (d VaultDeps) ServiceForProfile(profileName string) (vault.VaultService, error) {
	return d.service(profileName)
}

// resolveIndexerURL resolves the configured Sia indexer URL for an invocation.
func (d VaultDeps) resolveIndexerURL() (string, error) {
	if d.Service == nil {
		return "", fmt.Errorf("vault: no service factory wired")
	}
	if d.ResolveIndexerURL == nil {
		return "", fmt.Errorf("vault: no indexer URL resolver wired")
	}
	return d.ResolveIndexerURL(), nil
}

// withService resolves the active profile from input, builds a VaultService
// for it, and invokes fn with it, guaranteeing Close() on every exit path
// (including fn's error path). It collapses the ResolveProfile + d.service +
// defer svc.Close() preamble repeated by every vault handler.
func withService(ctx context.Context, d VaultDeps, input map[string]any, fn func(ctx context.Context, svc vault.VaultService) (any, error)) (any, error) {
	// A multi-profile server must never silently target the active vault for a
	// profile-scoped op that lacks an explicit profile (the same not-found bug
	// class as vault_status before the guard). withService backs the tag ops.
	if pr := d.profileRequired(catalog.StrArg(input, "profile", "")); pr != nil {
		return pr, nil
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
	return fn(ctx, svc)
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
		vaultSearch(d),
		vaultTagAdd(d),
		vaultTagRm(d),
		vaultTagSet(d),
		vaultTagLs(d),
		vaultRm(d),
		vaultSync(d),
		vaultFlush(d),
		vaultFlushStatus(d),
		vaultProfiles(d),
		vaultShare(d),
		vaultShareAccept(d),
		vaultSend(d),
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
		Description: "Summarize identity, local session, remote health, storage usage, and cache health for the selected vault profile. Remote health is probed live against the indexer; local cache stats come from the profile's index. Writing new files is done via vault_put_file (and the co-located/remote/mint source it accepts); tags/version operations are exposed by vault_tag_* and vault_version_*. The read-only operations on this catalog are ls/stat/verify/sync/share/rm.",
		Category:    "vault",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Args: []catalog.OperationArg{
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			if pr := d.profileRequired(catalog.StrArg(input, "profile", "")); pr != nil {
				return pr, nil
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
			{Name: "path", Type: catalog.ArgTypeString, Help: "Vault path to list (e.g. vault:/reports; defaults to the root)", AgentHelp: "The vault path to list. Append a trailing slash (vault:/a/b/) to list a subdirectory; without it, a non-root path is assumed to be a file path and lists the parent. If the directory is empty, the tool auto-retries as a directory."},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			if vaultPath == "" {
				vaultPath = vault.VaultRoot
			}
			if pr := d.profileRequired(catalog.StrArg(input, "profile", "")); pr != nil {
				return pr, nil
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
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("Show metadata for a single vault path: type, size, media type, content digest, object ID, and current status (staged | flushing | durable | failed). Returns metadata only, never the content. While a file is not yet durable (staged/flushing/failed) the result carries flush_attempts, flush_error and flush_started_at: a flushing file shows a flush_started_at and a rising flush_attempts with no error, a failed file shows flush_attempts plus a non-empty flush_error, and a staged file that has never started shows zero attempts/no error and an empty flush_started_at. Compare now against flush_started_at to tell a long-but-progressing host upload from a hung pin. Once durable, these fields are omitted."),
		),
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
			if pr := d.profileRequired(catalog.StrArg(input, "profile", "")); pr != nil {
				return pr, nil
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
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("Verify a vault file's integrity: checks that the object exists on the Sia indexer and compares the recorded SHA-256 digest. Returns digest_verified (verified/unverified/mismatch/not_applicable), digest_match, object_exists, and the recorded digest. An accepted share or vault_send has no digest until first decrypt/get/deep verify; in that state digest_verified is 'not_applicable' (a neutral no-verdict-yet — NOT a failure), so treat the pin as successful and resolve the digest on first get or a deep=true verify. Use deep=true to download the full content, recompute the hash, and backfill the digest if missing. Does NOT stream or return file content."),
		),
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
			if pr := d.profileRequired(catalog.StrArg(input, "profile", "")); pr != nil {
				return pr, nil
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
	Path         string `json:"path"`
	VaultVersion `json:",inline"`
}

// VaultVersionRestoreResult is the data returned by vault_version_restore:
// the new (restored) version that became the live current winner.
type VaultVersionRestoreResult struct {
	Path          string `json:"path"`
	RestoredTo    string `json:"restored_to"` // new version_id
	ObjectKey     string `json:"object_key"`
	ContentDigest string `json:"content_digest,omitempty"`
	Size          int64  `json:"size"`
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
		Args: append(catalog.ListArgs(),
			catalog.OperationArg{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "Vault path whose versions to list", AgentHelp: "The vault:/ path whose version history to list."},
			catalog.OperationArg{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		),
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			if vaultPath == "" {
				return nil, fmt.Errorf("vault_version_ls: missing required argument path")
			}
			if pr := d.profileRequired(catalog.StrArg(input, "profile", "")); pr != nil {
				return pr, nil
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
			items := make([]VaultVersion, 0, len(versions))
			for _, f := range versions {
				items = append(items, VaultVersion{
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
			totalVersions := len(items)
			page := catalog.ParseList(input)
			items = slicePage(items, page.Start, page.Limit)
			headers := []string{"Version ID", "Seq", "Current", "Size", "Updated"}
			rows := make([][]string, 0, len(items))
			for _, v := range items {
				cur := ""
				if v.IsCurrent {
					cur = "*"
				}
				rows = append(rows, []string{v.VersionID, fmt.Sprintf("%d", v.Seq), cur, fmt.Sprintf("%d", v.Size), v.UpdatedAt})
			}
			return NewListResult(items, ListResultMeta{
				Noun: "vault version(s)", Headers: headers, Rows: rows, Total: totalVersions,
			}), nil
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
			if pr := d.profileRequired(catalog.StrArg(input, "profile", "")); pr != nil {
				return pr, nil
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
			{Name: "confirm", Type: catalog.ArgTypeBool, Default: "false", Required: true, AgentConfirm: true, Help: "Must be true to restore (destructive to current content)", AgentHelp: "Set to true to confirm the destructive restore. An agent may self-confirm this restore headlessly with confirm=true."},
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
			if pr := d.profileRequired(catalog.StrArg(input, "profile", "")); pr != nil {
				return pr, nil
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
// vault tag add / rm / set / ls
// ---------------------------------------------------------------------------

// VaultTagResult is returned by vault_tag_add/rm/set: the path and the file's
// resulting full tag set.
type VaultTagResult struct {
	Path string   `json:"path"`
	Tags []string `json:"tags"`
}

// VaultTagListResult is returned by vault_tag_ls: every distinct tag in use
// across the vault, ordered most-recently-used first.
type VaultTagListResult struct {
	Tags []string `json:"tags"`
}

// VaultSearchResult is returned by vault_search: the matching files.
type VaultSearchResult struct {
	Query   string               `json:"query,omitempty"`
	Count   int                  `json:"count"`
	Results []string             `json:"results"` // full vault paths, newest-first
	Detail  map[string]vaultItem `json:"-"`
}

type vaultItem struct {
	Path   string   `json:"path"`
	Size   int64    `json:"size,omitempty"`
	Tags   []string `json:"tags,omitempty"`
	Source string   `json:"source,omitempty"`
	Host   string   `json:"host,omitempty"`
	Agent  string   `json:"agent,omitempty"`
}

func vaultSearch(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault_search",
		Title:       "Search vault files",
		Summary:     "Search vault files by name, tag, status, or write context",
		Description: "Search vault files by name and metadata.\n\nquery is a filename substring. It is not a query language. Filter with parameters: tag (repeat for AND), tag_any, status, not_status, source, host, agent, since, before, dir, and the structured `where` predicate list.\n\nwhere is an ANDed list of predicates; a field value that is a list is OR/IN on that field. Each object carries ONE field key (or not).\n\nExamples:\n  vault search report --tag finance --host claude-desktop\n  vault search \"q4 invoice\" --since 2024-01-01 --dir reports/\n  vault search --status failed --tag legal\n  vault search --where '[{\"tag\":[\"finance\",\"tax\"]},{\"host\":\"claude-desktop\"}]'",
		Category:    "vault",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Args: []catalog.OperationArg{
			{Name: "query", Type: catalog.ArgTypeString, Help: "Case-insensitive substring of the file name", AgentHelp: "A substring of the file name to match (case-insensitive)."},
			{Name: "tag", Type: catalog.ArgTypeStringSlice, Help: "Require ALL of these tags (repeatable; AND)", AgentHelp: "One or more tags; a file must have all of them. Repeat for multiple."},
			{Name: "tag_any", Type: catalog.ArgTypeStringSlice, Help: "Require ANY of these tags (list = OR/IN)", AgentHelp: "A file must have at least one of these tags."},
			{Name: "dir", Type: catalog.ArgTypeString, Help: "Restrict to files under this vault directory", AgentHelp: "A vault directory to restrict results to (inclusive)."},
			{Name: "status", Type: catalog.ArgTypeString, Enum: []string{"staged", "flushing", "durable", "failed", "pending", "ok", "uploaded", "lost"}, Help: "Only files with this status (staged|flushing|durable|failed; legacy ok/pending/lost accepted)"},
			{Name: "not_status", Type: catalog.ArgTypeString, Enum: []string{"staged", "flushing", "durable", "failed", "pending", "ok", "uploaded", "lost"}, Help: "Only files NOT with this status (staged|flushing|durable|failed; legacy ok/pending/lost accepted)"},
			{Name: "since", Type: catalog.ArgTypeString, Help: "Only files created at/after this time (RFC3339 or YYYY-MM-DD)"},
			{Name: "before", Type: catalog.ArgTypeString, Help: "Only files created before this time (RFC3339 or YYYY-MM-DD)"},
			{Name: "source", Type: catalog.ArgTypeString, Enum: []string{"mcp", "cli"}, Help: "Only files written by this frontend (mcp|cli)"},
			{Name: "source_any", Type: catalog.ArgTypeStringSlice, Help: "Any of these frontends (mcp|cli)"},
			{Name: "host", Type: catalog.ArgTypeString, Help: "Only files written from this host platform (e.g. claude-desktop)"},
			{Name: "host_any", Type: catalog.ArgTypeStringSlice, Help: "Any of these host platforms"},
			{Name: "agent", Type: catalog.ArgTypeString, Help: "Only files whose creator agent matches"},
			{Name: "agent_any", Type: catalog.ArgTypeStringSlice, Help: "Any of these creator agents"},
			{Name: "where", Type: catalog.ArgTypeRawJSON, RawSchema: vaultSearchWhereSchema, Help: "Structured ANDed predicate list (JSON; --where)", AgentHelp: "A list of predicates to filter by. Items are ANDed. Each object has exactly one field (or not): tag/status/source/host/agent/dir accept a string OR a list (a list means match ANY of them); since/before are scalar times (RFC3339 or YYYY-MM-DD)."},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to the active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			preds, err := vaultSearchPredicates(input)
			if err != nil {
				return nil, err
			}
			if pr := d.profileRequired(catalog.StrArg(input, "profile", "")); pr != nil {
				return pr, nil
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
			items, err := svc.Search(ctx, vault.SearchRequest{
				Query: catalog.StrArg(input, "query", ""),
				Where: preds,
			})
			if err != nil {
				return nil, err
			}
			paths := make([]string, 0, len(items))
			detail := make(map[string]vaultItem, len(items))
			for _, it := range items {
				paths = append(paths, it.Path)
				detail[it.Path] = vaultItem{Path: it.Path, Size: it.Size, Tags: it.Tags, Source: it.Source, Host: it.Host, Agent: it.Agent}
			}
			return &VaultSearchResult{Query: catalog.StrArg(input, "query", ""), Count: len(items), Results: paths, Detail: detail}, nil
		}),
	})
}

// searchPredicateFields metadata for the `where` arg's structured JSON Schema.
var vaultSearchWhereSchema = buildWhereSchema()

// buildWhereSchema returns the JSON Schema for the vault_search `where` arg: an
// array of predicate objects. Each object carries exactly one field key (or a
// `not` wrapper); the list-field values accept a string or a string array.
func buildWhereSchema() json.RawMessage {
	anyField := func(desc string) map[string]any {
		return map[string]any{
			"type":        []string{"string", "array"},
			"items":       map[string]any{"type": "string"},
			"description": desc,
		}
	}
	item := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tag":    anyField("Tag name(s): a single string or a list (list = match ANY of these tags)"),
			"status": anyField("File status: a single string or a list (list = any of staged|flushing|durable|failed; legacy ok/pending/lost also accepted)"),
			"source": anyField("Frontend that wrote the file (mcp|cli): a single string or a list"),
			"host":   anyField("Host platform (e.g. claude-desktop, codex): a single string or a list"),
			"agent":  anyField("Creator agent: a single string or a list"),
			"dir":    anyField("Vault directory prefix (exact or prefix, e.g. contracts/): a single string or a list"),
			"since":  map[string]any{"type": "string", "description": "Created at/after this time (RFC3339 or YYYY-MM-DD)"},
			"before": map[string]any{"type": "string", "description": "Created before this time (RFC3339 or YYYY-MM-DD)"},
			"not":    map[string]any{"type": "object", "description": "Negate a single predicate"},
		},
		"additionalProperties": false,
	}
	arr := map[string]any{
		"type":  "array",
		"items": item,
		"description": "A list of predicates. Items are ANDed. A field value that is a list is OR/IN on that field. " +
			"Each object carries exactly one field key (or a not wrapper): tag/status/source/host/agent/dir accept a string or a list of strings; since/before are scalar times.",
	}
	raw, _ := json.Marshal(arr)
	return raw
}

// vaultSearchPredicates compiles the CLI flags and the structured `where`
// parameter into a single ANDed []vault.Predicate list. Flag predicates and
// where predicates are ANDed together (no precedence). This is the single
// compiler shared by the CLI flags, --where, and the MCP `where` parameter.
func vaultSearchPredicates(input map[string]any) ([]vault.Predicate, error) {
	var preds []vault.Predicate
	// --tag: each value becomes its own scalar predicate -> AND across repeats.
	for _, t := range catalog.StrSliceArg(input, "tag") {
		if t != "" {
			preds = append(preds, vault.Predicate{Tag: []string{t}})
		}
	}
	// --tag-any: one predicate with a list -> OR/IN on the tag field.
	if any := catalog.StrSliceArg(input, "tag_any"); len(any) > 0 {
		preds = append(preds, vault.Predicate{Tag: any})
	}
	// --host / --host-any. host/source/agent are scalar ArgTypeString args, so
	// they must be read with StrArg (StrSliceArg returns nil for a scalar,
	// which silently dropped these filters) and wrapped in a one-element slice.
	if h := catalog.StrArg(input, "host", ""); h != "" {
		preds = append(preds, vault.Predicate{Host: []string{h}})
	}
	if any := catalog.StrSliceArg(input, "host_any"); len(any) > 0 {
		preds = append(preds, vault.Predicate{Host: any})
	}
	// --source / --source-any. Source is lowercased to match the stored
	// write-context values ("mcp"/"cli"): the enum gate accepts any case via
	// EqualFold, but columnFilter matches case-sensitively, so an uppercase
	// --source MCP would otherwise pass validation yet match no rows.
	if s := strings.ToLower(catalog.StrArg(input, "source", "")); s != "" {
		preds = append(preds, vault.Predicate{Source: []string{s}})
	}
	if any := catalog.StrSliceArg(input, "source_any"); len(any) > 0 {
		preds = append(preds, vault.Predicate{Source: lo.Map(any, func(v string, _ int) string { return strings.ToLower(v) })})
	}
	// --agent / --agent-any.
	if a := catalog.StrArg(input, "agent", ""); a != "" {
		preds = append(preds, vault.Predicate{Agent: []string{a}})
	}
	if any := catalog.StrSliceArg(input, "agent_any"); len(any) > 0 {
		preds = append(preds, vault.Predicate{Agent: any})
	}
	// --dir.
	if d := catalog.StrArg(input, "dir", ""); d != "" {
		preds = append(preds, vault.Predicate{Dir: []string{d}})
	}
	// --status (lowercased to match the catalog enum gate).
	if st := strings.ToLower(catalog.StrArg(input, "status", "")); st != "" {
		preds = append(preds, vault.Predicate{Status: []string{st}})
	}
	// --not-status.
	if ns := strings.ToLower(catalog.StrArg(input, "not_status", "")); ns != "" {
		preds = append(preds, vault.Predicate{Not: &vault.Predicate{Status: []string{ns}}})
	}
	// --since / --before (validated at compile in Search).
	if since := catalog.StrArg(input, "since", ""); since != "" {
		preds = append(preds, vault.Predicate{Since: since})
	}
	if before := catalog.StrArg(input, "before", ""); before != "" {
		preds = append(preds, vault.Predicate{Before: before})
	}
	// --where / MCP where: ANDed with the flag predicates.
	if w, ok := input["where"]; ok && w != nil {
		parsed, err := vault.ParseWhere(w)
		if err != nil {
			return nil, err
		}
		preds = append(preds, parsed...)
	}
	return preds, nil
}

func vaultTagAdd(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault_tag_add",
		Title:       "Add tags to a vault file",
		Summary:     "Add tags to a vault file (durable)",
		Description: "Add one or more tags to a vault file. Durable: tags are written to the Sia object's sealed metadata (in-place re-pin at the same content address) AND the local tag index, so they sync to every device without creating a new version. Repeat the --tag flag for multiple tags. Tags are normalized (lowercased, deduped).",
		Category:    "vault",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path>",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "Vault path to tag", AgentHelp: "The vault:/ path of the file to tag."},
			{Name: "tags", Type: catalog.ArgTypeStringSlice, Required: true, Help: "Tag(s) to add (repeatable)", AgentHelp: "One or more tags to add to the file."},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			tags := catalog.StrSliceArg(input, "tags")
			if vaultPath == "" || len(tags) == 0 {
				return nil, fmt.Errorf("vault_tag_add: missing required argument (path, tags)")
			}
			return withService(ctx, d, input, func(ctx context.Context, svc vault.VaultService) (any, error) {
				f, err := svc.AddTags(ctx, vaultPath, tags)
				if err != nil {
					return nil, err
				}
				return &VaultTagResult{Path: vaultPath, Tags: f.Tags}, nil
			})
		}),
	})
}

func vaultTagRm(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault_tag_rm",
		Title:       "Remove tags from a vault file",
		Summary:     "Remove tags from a vault file (durable)",
		Description: "Remove one or more tags from a vault file. Durable (same re-pin-and-write path as vault_tag_add). Tags that become unused by any file are pruned from the tag index. Repeat --tag for multiple tags.",
		Category:    "vault",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path>",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "Vault path", AgentHelp: "The vault:/ path of the file."},
			{Name: "tags", Type: catalog.ArgTypeStringSlice, Required: true, Help: "Tag(s) to remove (repeatable)", AgentHelp: "One or more tags to remove from the file."},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			tags := catalog.StrSliceArg(input, "tags")
			if vaultPath == "" || len(tags) == 0 {
				return nil, fmt.Errorf("vault_tag_rm: missing required argument (path, tags)")
			}
			return withService(ctx, d, input, func(ctx context.Context, svc vault.VaultService) (any, error) {
				f, err := svc.RemoveTags(ctx, vaultPath, tags)
				if err != nil {
					return nil, err
				}
				return &VaultTagResult{Path: vaultPath, Tags: f.Tags}, nil
			})
		}),
	})
}

func vaultTagSet(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault_tag_set",
		Title:       "Set tags on a vault file",
		Summary:     "Replace a vault file's full tag set (durable)",
		Description: "Replace a vault file's tag set with exactly the given tags (remove-all-then-add). Durable (same re-pin-and-write path as vault_tag_add). Pass an empty set to clear all tags. Repeat --tag for multiple tags.",
		Category:    "vault",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path>",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "Vault path", AgentHelp: "The vault:/ path of the file."},
			{Name: "tags", Type: catalog.ArgTypeStringSlice, Help: "Full tag set (repeatable; empty clears all)", AgentHelp: "The exact tag set; omit to clear all tags."},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			vaultPath := catalog.StrArg(input, "path", "")
			if vaultPath == "" {
				return nil, fmt.Errorf("vault_tag_set: missing required argument path")
			}
			tags := catalog.StrSliceArg(input, "tags")
			return withService(ctx, d, input, func(ctx context.Context, svc vault.VaultService) (any, error) {
				f, err := svc.SetTags(ctx, vaultPath, tags)
				if err != nil {
					return nil, err
				}
				return &VaultTagResult{Path: vaultPath, Tags: f.Tags}, nil
			})
		}),
	})
}

func vaultTagLs(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault_tag_ls",
		Title:       "List vault tags",
		Summary:     "List every distinct tag in use",
		Description: "List every distinct tag currently in use across the vault, ordered most-recently-used first. Read-only. Use with vault_search --tag to find 'everything tagged X'.",
		Category:    "vault",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Args: append(catalog.ListArgs(),
			catalog.OperationArg{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		),
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			return withService(ctx, d, input, func(ctx context.Context, svc vault.VaultService) (any, error) {
				tags, err := svc.TagList(ctx)
				if err != nil {
					return nil, err
				}
				page := catalog.ParseList(input)
				items := slicePage(tags, page.Start, page.Limit)
				headers := []string{"Tag"}
				rows := make([][]string, 0, len(items))
				for _, t := range items {
					rows = append(rows, []string{t})
				}
				return NewListResult(items, ListResultMeta{
					Noun: "vault tag(s)", Headers: headers, Rows: rows,
				}), nil
			})
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
			if pr := d.profileRequired(catalog.StrArg(input, "profile", "")); pr != nil {
				return pr, nil
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
			if pr := d.profileRequired(catalog.StrArg(input, "profile", "")); pr != nil {
				return pr, nil
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
// vault flush
// ---------------------------------------------------------------------------

// vaultFlushTimeout bounds how long a non-blocking vault_flush background
// goroutine is allowed to run a (possibly large) packed upload from staged
// files to finish. The flush is detached from the invocation: it runs on a
// context derived from Background so a slow full host-set upload is not killed
// by the MCP transport deadline. vault_flush itself returns immediately with a
// job-accepted result; the agent polls vault_stat until status is ok.
const vaultFlushTimeout = 10 * time.Minute

// VaultFlushResult is returned by vault_flush. vault_flush is non-blocking: it
// accepts a flush job on the per-profile FlushManager (or, as a fallback when no
// FlushMgr is wired, launches a detached goroutine) and returns immediately.
// Status is "idle" when there is nothing to do (file already durable or failed)
// and "accepted" when a flush job was accepted. When a FlushMgr is wired, the
// result carries the job_id to poll via vault_flush_status.
type VaultFlushResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	// JobID is the flush job to poll via vault_flush_status (only when a
	// FlushMgr is wired; empty on the detached-goroutine fallback and idle path).
	JobID string `json:"job_id,omitempty"`
	// Profile is the profile the job was accepted for.
	Profile string `json:"profile,omitempty"`
	// Path is the restricted flush path, when a single file was targeted.
	Path string `json:"path,omitempty"`
	// Flushed is the number of staged files made durable by a completed flush.
	// It is unset (0) on the "accepted" (non-blocking) path; the CLI sets it on
	// its blocking flush, which reports the real count before the process exits.
	Flushed int `json:"flushed,omitempty"`
}

func vaultFlush(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault_flush",
		Title:       "Flush staged vault files to durable storage",
		Summary:     "Upload and pin pending vault files",
		Description: "Upload and pin staged vault files so they become durable on Sia. vault_put_file stages bytes locally and returns immediately; this packs the staged bytes into shared slabs, uploads and pins them, and marks the rows durable. Flush all staged files, or a single path via the path argument. The flush runs on a per-profile flush worker: this tool returns immediately with an accepted job { job_id, profile, path? } — poll vault_flush_status(job_id) or vault_stat until status is durable before sharing a freshly written file (a share link requires a durable object).",
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("Make staged vault files durable on Sia. vault_put_file returns before bytes are on Sia (status: staged); call this to kick off upload + pin. Flush all staged files by default, or pass path to flush a single file. This tool is non-blocking and runs on a per-profile flush worker (one worker goroutine per profile, never a shared global queue). It returns a job { job_id, profile, path? } with status accepted — poll vault_flush_status(job_id) or vault_stat until status is durable, then vault_share. A full host-set upload takes time, so the immediate accepted response is not completion. If a file stays non-durable across polls, vault_stat's flush_started_at, flush_attempts and flush_error describe its progress: a flushing file shows a flush_started_at and a rising flush_attempts with no error, a failed file shows flush_attempts plus a non-empty flush_error, and a staged file that never started shows zero attempts/no error and an empty flush_started_at. Comparing now against flush_started_at distinguishes a long-but-progressing host upload from a hung pin."),
		),
		Category:    "vault",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "[<path>]",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Help: "Vault path to flush (flush only this file if set; otherwise flush every staged file)", AgentHelp: "An optional vault:/ path restricting the flush to a single file; when omitted, every staged file is flushed."},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile name (defaults to active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			if pr := d.profileRequired(catalog.StrArg(input, "profile", "")); pr != nil {
				return pr, nil
			}
			profileName, err := vault.ResolveProfile(catalog.StrArg(input, "profile", ""))
			if err != nil {
				return nil, err
			}
			path := catalog.StrArg(input, "path", "")

			// Per-profile flush manager path (default when wired): Enqueue is the
			// ONLY launch — do NOT also spawn a detached goroutine (that would
			// double-flush). The manager worker lazily builds and owns its own
			// VaultService per profile.
			if d.FlushMgr != nil {
				// Single-path case: short-circuit synchronously when there is
				// nothing to flush. FlushPath silently no-ops for an
				// already-durable or failed file, so resolve it first (on a
				// throwaway service) and report idle instead of launching a
				// useless job. st.Status is already canonical (normalized by Stat).
				if path != "" {
					if svc, serr := d.service(profileName); serr == nil {
						st, serr2 := svc.Stat(ctx, path)
						_ = svc.Close()
						if serr2 == nil && (st.Status == vault.FileStatusDurable || st.Status == vault.FileStatusFailed) {
							return &VaultFlushResult{
								Status:  "idle",
								Message: "file is already " + st.Status + "; nothing to flush.",
							}, nil
						}
					}
				}
				mgr := d.FlushMgr()
				job, err := mgr.Enqueue(ctx, profileName, path)
				if err != nil {
					return nil, err
				}
				return &VaultFlushResult{
					JobID:   job.JobID,
					Profile: profileName,
					Path:    path,
					Status:  "accepted",
					Message: "flush job " + job.JobID + " accepted for profile " + profileName + "; poll vault_flush_status(job_id) or vault_stat until status is durable.",
				}, nil
			}

			// Fallback (no FlushMgr wired): detached single-use goroutine. Build
			// ONE service and use it for BOTH the single-path idle short-circuit
			// and the detached flush, so Close runs exactly once.
			svc, err := d.service(profileName)
			if err != nil {
				return nil, err
			}
			if path != "" {
				if st, serr := svc.Stat(ctx, path); serr == nil && (st.Status == vault.FileStatusDurable || st.Status == vault.FileStatusFailed) {
					svc.Close()
					return &VaultFlushResult{
						Status:  "idle",
						Message: "file is already " + st.Status + "; nothing to flush.",
					}, nil
				}
			}
			flushCtx, cancel := context.WithTimeout(context.Background(), vaultFlushTimeout)
			go func() {
				defer cancel()
				defer svc.Close()
				if path != "" {
					_ = svc.FlushPath(flushCtx, path)
				} else {
					_, _ = svc.Flush(flushCtx)
				}
			}()

			scope := "all staged files"
			if path != "" {
				scope = "file " + path
			}
			return &VaultFlushResult{
				Status:  "accepted",
				Message: "flush initiated for " + scope + "; poll vault_stat until status: durable before sharing.",
			}, nil
		}),
	})
}

// ---------------------------------------------------------------------------
// vault flush status
// ---------------------------------------------------------------------------

// VaultFlushJobStatus is the current snapshot of an accepted flush job, read
// via vault_flush_status(job_id). Status is one of queued|running|done|failed.
type VaultFlushJobStatus struct {
	JobID   string `json:"job_id"`
	Profile string `json:"profile"`
	Path    string `json:"path,omitempty"`
	Status  string `json:"status"` // queued|running|done|failed
	Flushed int    `json:"flushed,omitempty"`
	Error   string `json:"error,omitempty"`
	// StartedAt is the RFC3339 time the worker began the job ("" when still
	// queued), so a swarm can detect a hung pin by elapsed time.
	StartedAt string `json:"started_at,omitempty"`
}

func vaultFlushStatus(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault_flush_status",
		Title:       "Vault flush job status",
		Summary:     "Check the status of a vault_flush job",
		Description: "Return the current status of a flush job previously accepted by vault_flush, addressed by its job_id. Status is queued (accepted, not started), running (the per-profile worker is flushing), done (all targeted staged files became durable; flushed reports the count), or failed (the flush errored; error explains why). A running job carries started_at (RFC3339) so a swarm can detect a hung pin by elapsed time; read vault_stat's flush_started_at / flush_attempts / flush_error on the file for the underlying state. Read-only.",
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("Check the status of a flush job accepted by vault_flush, given its job_id. Returns { job_id, profile, path?, status (queued|running|done|failed), flushed?, error?, started_at? }. A done job means the targeted staged file(s) are now durable; a failed job carries an error whose cause is visible in vault_stat's flush_error, after which vault_flush can be re-run. started_at is the RFC3339 time the worker began the job, so a job stuck in 'running' longer than your hang threshold (compare against vault_stat's flush_started_at on the file) is a hung pin, not just a slow upload. Requires a wired flush manager; without one this errors with code no_flush_manager."),
		),
		Category:    "vault",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Args: []catalog.OperationArg{
			{Name: "job_id", Type: catalog.ArgTypeString, Required: true, Help: "The job_id returned by vault_flush", AgentHelp: "The job_id returned by vault_flush."},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			jobID := catalog.StrArg(input, "job_id", "")
			if jobID == "" {
				return nil, fmt.Errorf("vault_flush_status: missing required argument job_id")
			}
			if d.FlushMgr == nil {
				return map[string]any{
					"code":    "no_flush_manager",
					"message": "no flush manager is wired; vault_flush_status requires a per-profile flush manager",
				}, nil
			}
			job, ok := d.FlushMgr().Job(jobID)
			if !ok {
				return map[string]any{
					"code":    "job_not_found",
					"job_id":  jobID,
					"message": "no flush job with id " + jobID + " is known",
				}, nil
			}
			return &VaultFlushJobStatus{
				JobID:     job.JobID,
				Profile:   job.Profile,
				Path:      job.Path,
				Status:    job.Status,
				Flushed:   job.Flushed,
				Error:     job.Error,
				StartedAt: job.StartedAt,
			}, nil
		}),
	})
}

// ---------------------------------------------------------------------------
// vault profiles
// ---------------------------------------------------------------------------

// VaultProfilesResult is returned by vault_profiles: every provisioned/unlocked
// profile name the server can see.
type VaultProfilesResult struct {
	Profiles []string `json:"profiles"`
}

func vaultProfiles(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault_profiles",
		Title:       "List vault profiles",
		Summary:     "List every unlocked vault profile",
		Description: "List every provisioned/unlocked vault profile name the running server can access. On a multi-profile server, vault ops that require a profile (stat/verify/search/flush/share/send/accept) return code profile_required unless you pass profile=<name>; use this tool to enumerate the names. Read-only.",
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("List every provisioned/unlocked vault profile name the server can access: { profiles: [<names>] }. On a multi-profile server (more than one unlocked profile), vault ops without an explicit profile= argument return code profile_required instead of silently hitting the active vault; pass the name from this list. Read-only."),
		),
		Category:    "vault",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Args:        []catalog.OperationArg{},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			return &VaultProfilesResult{Profiles: d.unlockedProfiles()}, nil
		}),
	})
}

// ---------------------------------------------------------------------------
// vault share
// ---------------------------------------------------------------------------

// VaultShareResult is the data returned by a successful vault share: the
// share URL and its expiry timestamp. A share link is only produced for a
// durable file; a non-durable target returns a VaultNotDurableResult instead.
type VaultShareResult struct {
	ShareURL string `json:"share_url,omitempty"`
	Expires  string `json:"expires,omitempty"`
	Status   string `json:"status,omitempty"`
	Message  string `json:"message,omitempty"`
}

// VaultNotDurableResult is returned (as data, not a Go error) when a share or
// send targets a file that is not yet durable on Sia. It distinguishes "still
// uploading" (staged/flushing — tell the caller to flush) from "failed" (a
// terminal state — sharing will never work without re-uploading).
type VaultNotDurableResult struct {
	Code    string `json:"code"` // "not_durable"
	Path    string `json:"path"`
	Status  string `json:"status"`
	Message string `json:"message"`
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
		Description: "Generate a shareable download link for a vault file. Returns a pre-signed URL and its expiry time. The URL is time-limited and grants read access to a single object. Control the expiry with the expiry field (e.g. 7d, 30d, 1h, or 0 for never). Does NOT upload or modify the file itself. Only durable files can be shared: a file that is staged/flushing/failed returns a structured {code:\"not_durable\",...} result, never a misleading success.",
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("Generate a share link for a durable vault file. Returns a time-limited pre-signed https:// URL whose fragment carries the object's encryption key (#encryption_key=…). Pass this URL unchanged to vault_share_accept on another profile to pin slab references — a metadata-only operation that transfers no content. Donor sealed metadata is NOT put into the share URL; the fragment still only carries the encryption key. A link works only for a durable file: if the file is staged/flushing/failed this returns a structured {code:\"not_durable\", path, status, message} result (failed means the durability flush failed and sharing will not work until the file is re-uploaded; a staged/flushing file needs its flush completed via vault_flush, then vault_flush_status or vault_stat until durable, or vault_send). Bound how long the link works with the expiry field (e.g. 7d, 30d, 1h, or 0 for never)."),
		),
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
			if pr := d.profileRequired(catalog.StrArg(input, "profile", "")); pr != nil {
				return pr, nil
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
			// A share link grants read access to a Sia-stored object, so the
			// file must be durable before a link can be issued. We deliberately
			// do NOT force a synchronous flush here: a full host-set packed
			// upload can exceed the invocation deadline. Instead we return a
			// structured not_durable result pointing the agent at vault_flush.
			st, serr := svc.Stat(ctx, vaultPath)
			if serr != nil {
				return nil, serr
			}
			if st.Status != vault.FileStatusDurable {
				// A failed file has no durable object to share and is not going
				// to become shareable via a flush — surface the real reason
				// instead of suggesting vault_flush.
				if st.Status == vault.FileStatusFailed {
					reason := st.LostReason
					msg := "file is not durable (status: failed); it cannot be shared."
					if reason != "" {
						msg = "file is not durable (status: failed: " + reason + "); it cannot be shared."
					}
					return &VaultNotDurableResult{
						Code:    "not_durable",
						Path:    vaultPath,
						Status:  st.Status,
						Message: msg,
					}, nil
				}
				// staged/flushing: staged locally, not yet fully pinned. Point the
				// agent at vault_flush rather than blocking the share under the
				// request deadline.
				return &VaultNotDurableResult{
					Code:    "not_durable",
					Path:    vaultPath,
					Status:  st.Status,
					Message: "file is not yet durable on Sia (status: " + st.Status + "). Run vault_flush and poll vault_flush_status(job_id) until done, or use vault_send, then share again.",
				}, nil
			}
			shareURL, err := svc.Share(ctx, vaultPath, validUntil)
			if err != nil {
				return nil, err
			}
			return &VaultShareResult{ShareURL: shareURL, Expires: validUntil.Format(time.RFC3339), Status: "durable"}, nil
		}),
	})
}

// VaultShareAcceptResult is returned by vault_share_accept: the newly-pinned
// copy in the accepting profile. Accept creates an independent pin of the same
// object key (a metadata-only slab-reference copy), not a rewritten object, so
// AcceptState is "pinned" and DigestVerified is "not_applicable" until the
// acceptor first gets/decrypts or deep-verifies the content.
type VaultShareAcceptResult struct {
	Path           string `json:"path"`
	ObjectKey      string `json:"object_key"`
	Size           int64  `json:"size"`
	AcceptState    string `json:"accept_state"`              // "pinned"
	DigestVerified string `json:"digest_verified,omitempty"` // "not_applicable"
}

func vaultShareAccept(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault_share_accept",
		Title:       "Accept a vault share",
		Summary:     "Accept a share URL and pin the shared content",
		Description: "Accept a time-limited share link for a vault file and pin the referenced content into this profile's vault at the given path. Only the slab references are recorded — the content is not re-downloaded, so it is fast regardless of file size. Returns the newly-pinned file.",
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("Accept a share URL issued by another agent/profile and pin its slab references into this profile's vault. Metadata-only — no content is downloaded from Sia hosts, so it completes quickly regardless of file size. Accept creates an independent pin of the same object key (a metadata-only slab-reference copy), NOT a rewritten object, so accept_state is 'pinned' and digest_verified is 'not_applicable' until the acceptor first gets/decrypts or deep-verifies the content. The accepting profile owns an independent object referencing the same sectors, so the content survives even if the sharer deletes theirs. The share URL's scheme and host are rewritten to this profile's indexer origin before any request is made, so the agent never needs to validate or transform the URL. Accepting the same share at different paths creates multiple local rows referencing the same indexer object (like hard-links); PinObject is idempotent, so duplicate accepts of the same slabs are a no-op on the indexer. path is the vault:/ destination for the pinned copy."),
		),
		Category:    "vault",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path>",
		Args: []catalog.OperationArg{
			{Name: "share_url", Type: catalog.ArgTypeString, Required: true, Help: "The share URL to accept", AgentHelp: "The https:// share URL you received from vault_share. It is time-limited and carries the encryption key in its fragment (#encryption_key=…). Pass it through unchanged."},
			{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "Where to store the accepted copy", AgentHelp: "The vault:/ destination path where the accepted copy should be pinned."},
			{Name: "tags", Type: catalog.ArgTypeStringSlice, Help: "Tags to apply at write time (repeatable; durable)", AgentHelp: "Tags applied atomically at write time — durable on the sealed object and local tag index. Eliminates the need for a separate vault_tag_add call."},
			{Name: "target_principal", Type: catalog.ArgTypeString, Help: "Optional principal/source identity recorded in the share ledger"},
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile to accept into (defaults to the active profile)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			shareURL := catalog.StrArg(input, "share_url", "")
			vaultPath := catalog.StrArg(input, "path", "")
			if shareURL == "" || vaultPath == "" {
				return nil, fmt.Errorf("vault_share_accept: missing required argument (share_url, path)")
			}
			if pr := d.profileRequired(catalog.StrArg(input, "profile", "")); pr != nil {
				return pr, nil
			}
			targetPrincipal := catalog.StrArg(input, "target_principal", "")
			tags := catalog.StrSliceArg(input, "tags")
			var metadata map[string]any
			if len(tags) > 0 {
				metadata = map[string]any{"tags": tags}
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
			// Detach from the MCP request context so a token expiry or
			// client disconnect does not cancel an in-flight download+pin
			// that has already received the share body.
			taskCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
			defer cancel()
			f, err := svc.ShareAccept(taskCtx, vaultPath, shareURL, targetPrincipal, metadata)
			if err != nil {
				return nil, err
			}
			return &VaultShareAcceptResult{
				Path:           vaultPath,
				ObjectKey:      f.ObjectKey,
				Size:           f.Size,
				AcceptState:    "pinned",
				DigestVerified: "not_applicable",
			}, nil
		}),
	})
}

// ---------------------------------------------------------------------------
// vault send (swarm handoff primitive)
// ---------------------------------------------------------------------------

// VaultSendResult is returned by a successful vault_send: the destination row
// was pinned into to_profile referencing the same object key (accept_state
// "pinned"), and the destination's profile names.
type VaultSendResult struct {
	FromProfile string `json:"from_profile"`
	ToProfile   string `json:"to_profile"`
	DestPath    string `json:"dest_path"`
	ObjectKey   string `json:"object_key"`
	Size        int64  `json:"size"`
	AcceptState string `json:"accept_state"` // "pinned"
}

func vaultSend(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "vault_send",
		Title:       "Send a vault file to another profile",
		Summary:     "Hand off a durable vault file from one profile to another",
		Description: "Hand a durable vault file from one vault profile to another within the same process (the swarm handoff primitive). The server flushes-if-needed (it does NOT block on a long Sia upload in the request — if the source is not yet durable it returns a structured not_durable result telling you to vault_flush and poll vault_flush_status), then mints a 24h share from the source and accepts it into the destination profile. Accept is metadata-only (a pin of the same object key, not a full decrypt), so it returns quickly with accept_state pinned once the destination row exists. Requires two distinct profiles (from_profile, to_profile) and a vault:/ destination (dest_path).",
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("Send a durable vault file from one profile to another in the same process (swarm handoff). Requires path (vault:/ source), from_profile, to_profile (two distinct unlocked profiles), and dest_path (vault:/ destination); tags is an optional durable tag list. The server flushes-if-needed — it does NOT block on a long Sia upload in the request: if the source is not yet durable it returns {code:'not_durable', path, status, message}, and the source is made durable by running vault_flush, then polling vault_flush_status(job_id) or vault_stat until durable, before calling vault_send again. Once durable it mints a 24h share from the source, accepts it into the destination profile (metadata-only pin of the same object key — no full decrypt, no long client-side sleep), and returns once the destination row exists: {from_profile, to_profile, dest_path, object_key, size, accept_state:'pinned'}. Accept-state pinned — NOT a digest failure — is the success signal; the destination will deep-verify on first get/decrypt."),
		),
		Category:    "vault",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<path> <dest_path>",
		Args: []catalog.OperationArg{
			{Name: "path", Type: catalog.ArgTypeString, Required: true, Help: "vault:/ source path to send", AgentHelp: "The vault:/ source path whose durable object to hand off."},
			{Name: "dest_path", Type: catalog.ArgTypeString, Required: true, Help: "vault:/ destination path in to_profile", AgentHelp: "The vault:/ destination path where the source object should be pinned in to_profile."},
			{Name: "from_profile", Type: catalog.ArgTypeString, Required: true, Help: "Source profile name", AgentHelp: "The unlocked profile that owns the source object (required)."},
			{Name: "to_profile", Type: catalog.ArgTypeString, Required: true, Help: "Destination profile name", AgentHelp: "The unlocked profile that will own the pinned destination copy (required)."},
			{Name: "tags", Type: catalog.ArgTypeStringSlice, Help: "Tags to apply at accept time (repeatable; durable)", AgentHelp: "Optional durable tags applied at accept time on the destination row."},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			path := catalog.StrArg(input, "path", "")
			destPath := catalog.StrArg(input, "dest_path", "")
			fromProfile := catalog.StrArg(input, "from_profile", "")
			toProfile := catalog.StrArg(input, "to_profile", "")
			if path == "" || destPath == "" {
				return nil, fmt.Errorf("vault_send: missing required argument (path, dest_path)")
			}
			if fromProfile == "" || toProfile == "" {
				return map[string]any{
					"code":    "invalid_profiles",
					"message": "vault_send requires both from_profile and to_profile (the unlocked source and destination profiles)",
				}, nil
			}
			if fromProfile == toProfile {
				return map[string]any{
					"code":    "invalid_profiles",
					"message": "vault_send requires two distinct profiles (from_profile != to_profile); use vault_share + vault_share_accept within one profile instead",
				}, nil
			}

			fromSvc, err := d.service(fromProfile)
			if err != nil {
				return nil, err
			}
			defer fromSvc.Close()

			// Flush-if-needed but NEVER block on a long Sia upload in the
			// request: if the source is not yet durable, return a structured
			// not_durable result so the agent flushes and retries.
			st, err := fromSvc.Stat(ctx, path)
			if err != nil {
				return nil, err
			}
			if st.Status != vault.FileStatusDurable {
				return &VaultNotDurableResult{
					Code:    "not_durable",
					Path:    path,
					Status:  st.Status,
					Message: "source not durable; run vault_flush and poll vault_flush_status, then vault_send again",
				}, nil
			}

			toSvc, err := d.service(toProfile)
			if err != nil {
				return nil, err
			}
			defer toSvc.Close()

			// Mint a 24h share from the source, then accept it into dest as a
			// metadata-only pin of the same object key.
			validUntil := time.Now().Add(24 * time.Hour)
			shareURL, err := fromSvc.Share(ctx, path, validUntil)
			if err != nil {
				return nil, err
			}
			var metadata map[string]any
			if tags := catalog.StrSliceArg(input, "tags"); len(tags) > 0 {
				metadata = map[string]any{"tags": tags}
			}
			f, err := toSvc.ShareAccept(ctx, destPath, shareURL, "", metadata)
			if err != nil {
				return nil, err
			}
			return &VaultSendResult{
				FromProfile: fromProfile,
				ToProfile:   toProfile,
				DestPath:    destPath,
				ObjectKey:   f.ObjectKey,
				Size:        f.Size,
				AcceptState: "pinned",
			}, nil
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
			if pr := d.profileRequired(catalog.StrArg(input, "profile", "")); pr != nil {
				return pr, nil
			}
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
			if pr := d.profileRequired(catalog.StrArg(input, "profile", "")); pr != nil {
				return pr, nil
			}
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
