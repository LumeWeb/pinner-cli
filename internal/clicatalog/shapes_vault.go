package clicatalog

// shapes_vault.go declares the declarative command shape for the vault catalog
// domain, consumed by CompileCommandTree.
//
// Canonical op names (all-underscore, emitted by catalogops.VaultOperations in
// a stable declaration order):
//
//	vault_status/ls/stat/verify/search/rm/sync/flush/profiles/share/send/forget
//	                          -> {"vault",<leaf>}             (flat leaves)
//	vault_version_ls/get/restore -> {"vault","version",<leaf>}
//	vault_tag_add/rm/set/ls    -> {"vault","tag",<leaf>}
//	vault_flush_status         -> {"vault","flush","status"}
//	vault_share_accept         -> {"vault","share","accept"}
//	vault_profile_use          -> {"vault","profile","use"}
//	vault_cache_rebuild/clear  -> {"vault","cache",<leaf>}
//
// Leaf display names are the canonical last path token (single, already
// hyphenated) with no rename/alias — the historical CLI surface is preserved
// exactly.
//
// Two flat leaves DOUBLE as a nested parent's top-level Action via FoldFlat,
// reproducing the historical fold where a flat op shares a parent with a
// two-level sibling: vault_flush is folded into the "flush" parent (the
// parent keeps the blocking sync Action so `pinner vault flush <path>` still
// runs, while `vault flush status` nests under it), and vault_share is folded
// into the "share" parent (`vault share <path>` issues while `vault share
// accept` nests).
//
// Order is left at 0 everywhere so tied siblings keep the
// registry-declaration (== VaultOperations emission) order, which reproduces
// the historical root child order exactly (status, ls, stat, verify,
// version, search, tag, rm, sync, flush, profiles, share, send, forget,
// profile, cache) and each parent's leaf order, per the model's deterministic
// (Order, declaration-order) sort. The interactive/IO commands (create,
// restore, cp, cat) are NOT catalog ops and are appended by the CLI mount
// after the compiled tree is returned.

// VaultShapes is the ShapeRegistry for the vault domain, keyed by the
// all-underscore canonical op Name.
var VaultShapes = ShapeRegistry{
	// Flat leaves under the vault root.
	"vault_status":   {Path: []string{"vault", "status"}},
	"vault_ls":       {Path: []string{"vault", "ls"}},
	"vault_stat":     {Path: []string{"vault", "stat"}},
	"vault_verify":   {Path: []string{"vault", "verify"}},
	"vault_search":   {Path: []string{"vault", "search"}},
	"vault_rm":       {Path: []string{"vault", "rm"}},
	"vault_sync":     {Path: []string{"vault", "sync"}},
	"vault_profiles": {Path: []string{"vault", "profiles"}},
	"vault_send":     {Path: []string{"vault", "send"}},
	"vault_forget":   {Path: []string{"vault", "forget"}},

	// Folded flat leaves: also the top-level Action of the flush/share parents.
	"vault_flush": {Path: []string{"vault", "flush"}, FoldFlat: true},
	"vault_share": {Path: []string{"vault", "share"}, FoldFlat: true},

	// Version nesting.
	"vault_version_ls": {
		Path:     []string{"vault", "version", "ls"},
		Segments: map[int]Segment{1: vaultVersionSegment},
	},
	"vault_version_get": {
		Path:     []string{"vault", "version", "get"},
		Segments: map[int]Segment{1: vaultVersionSegment},
	},
	"vault_version_restore": {
		Path:     []string{"vault", "version", "restore"},
		Segments: map[int]Segment{1: vaultVersionSegment},
	},

	// Tag nesting.
	"vault_tag_add": {
		Path:     []string{"vault", "tag", "add"},
		Segments: map[int]Segment{1: vaultTagSegment},
	},
	"vault_tag_rm": {
		Path:     []string{"vault", "tag", "rm"},
		Segments: map[int]Segment{1: vaultTagSegment},
	},
	"vault_tag_set": {
		Path:     []string{"vault", "tag", "set"},
		Segments: map[int]Segment{1: vaultTagSegment},
	},
	"vault_tag_ls": {
		Path:     []string{"vault", "tag", "ls"},
		Segments: map[int]Segment{1: vaultTagSegment},
	},

	// Flush nesting (folded flat action + status child).
	"vault_flush_status": {
		Path:     []string{"vault", "flush", "status"},
		Segments: map[int]Segment{1: vaultFlushSegment},
	},

	// Share nesting (folded flat action + accept child).
	"vault_share_accept": {
		Path:     []string{"vault", "share", "accept"},
		Segments: map[int]Segment{1: vaultShareSegment},
	},

	// Profile nesting.
	"vault_profile_use": {
		Path:     []string{"vault", "profile", "use"},
		Segments: map[int]Segment{1: vaultProfileSegment},
	},

	// Cache nesting.
	"vault_cache_rebuild": {
		Path:     []string{"vault", "cache", "rebuild"},
		Segments: map[int]Segment{1: vaultCacheSegment},
	},
	"vault_cache_clear": {
		Path:     []string{"vault", "cache", "clear"},
		Segments: map[int]Segment{1: vaultCacheSegment},
	},
}

// vaultVersionSegment is the `version` sibling parent under the vault root.
var vaultVersionSegment = Segment{
	Name:     "version",
	Category: "Vault",
	Usage:    "Manage vault version",
}

// vaultTagSegment is the `tag` sibling parent under the vault root.
var vaultTagSegment = Segment{
	Name:     "tag",
	Category: "Vault",
	Usage:    "Manage vault tag",
}

// vaultFlushSegment is the `flush` sibling parent under the vault root; its
// top-level Action is the folded vault_flush leaf.
var vaultFlushSegment = Segment{
	Name:     "flush",
	Category: "Vault",
	Usage:    "Manage vault flush",
}

// vaultShareSegment is the `share` sibling parent under the vault root; its
// top-level Action is the folded vault_share leaf.
var vaultShareSegment = Segment{
	Name:     "share",
	Category: "Vault",
	Usage:    "Manage vault share",
}

// vaultProfileSegment is the `profile` sibling parent under the vault root,
// reproducing the historical Usage "Manage vault profiles".
var vaultProfileSegment = Segment{
	Name:     "profile",
	Category: "Vault",
	Usage:    "Manage vault profiles",
}

// vaultCacheSegment is the `cache` sibling parent under the vault root,
// reproducing the historical Usage "Manage the vault SQLite cache".
var vaultCacheSegment = Segment{
	Name:     "cache",
	Category: "Vault",
	Usage:    "Manage the vault SQLite cache",
}

// VaultDomainRoot is the DomainRoot declaration for the vault domain. The root
// command itself is constructed by the consuming CLI mount (newVaultCommand);
// the transform returns its ordered children (the flat leaves + the
// version/tag/flush/share/profile/cache parents).
var VaultDomainRoot = DomainRoot{
	Name:     "vault",
	Category: "Vault",
	Usage:    "Private encrypted file storage via Sia",
	Desc:     "Manage private encrypted files in a Sia-backed vault. These subcommands are compiled from the canonical operation catalog (internal/catalogops); the interactive create/restore/cp/cat commands are appended by the CLI mount.",
}
