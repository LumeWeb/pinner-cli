// Package schematext is the dependency-neutral canonical source of shared
// JSON-schema property descriptions that several repeat-building surfaces
// (tool input schemas, launcher input schemas, and app-only helper schemas)
// must copy verbatim: the presigned-endpoint TTL, the vault write profile,
// the download `sink` property, and the canonical sink=drop benefit prose
// (toolforge descriptions, capabilities report, agent guide).
// It imports nothing from the mcp parent package, so the vault, upload, and
// core subpackages can all compose THE SAME fragments; each surface keeps its
// own lead-in shape but the behavior text has exactly ONE source and can never
// drift. The TTL wording is tied to the upload coordinator's
// DefaultHTTPUploadTTL so a single constant change re-renders every surface,
// and the default is normalized to one format ("default 5m") everywhere.
//
// Go struct tags cannot embed constants, so a tag field copies its description
// literally; tests in the owning packages pin every such tag to the composed
// constant below, making drift a build failure instead of silent divergence.
package schematext

import (
	"strconv"
	"time"

	"go.lumeweb.com/mcpplane/transfer"
)

// DefaultPresignTTL is the presigned-endpoint lifetime every TTL schema
// description documents. It echoes the upload coordinator's
// DefaultHTTPUploadTTL so the schema prose and the actually applied default
// change together.
const DefaultPresignTTL = transfer.DefaultHTTPUploadTTL

// ttlPretty renders d as compact schema prose: whole minutes collapse to "<n>m"
// (never Duration.String()'s "5m0s") so the text matches the duration-string
// format agents are told to pass.
func ttlPretty(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	if d%time.Minute == 0 {
		return strconv.FormatInt(int64(d/time.Minute), 10) + "m"
	}
	return d.String()
}

var (
	// TTLExample is the "e.g. 5m" example clause derived from DefaultPresignTTL.
	TTLExample = "e.g. " + ttlPretty(DefaultPresignTTL)

	// TTLDefault is the "default 5m" default clause derived from
	// DefaultPresignTTL — the ONE normalized default format across surfaces.
	TTLDefault = "default " + ttlPretty(DefaultPresignTTL)

	// TTLLauncherFresh is open_upload_manager's TTL description: the TTL only
	// applies when the launcher mints a fresh operation (a live handle
	// continues its existing endpoint).
	TTLLauncherFresh = "Optional presigned endpoint lifetime, " + TTLExample + " (" + TTLDefault + "). Only used when a fresh operation is prepared."

	// TTLOptional is the optional-TTL description shared by the
	// open_vault_manager launcher and the vault_upload_submit app helper.
	TTLOptional = "Optional presigned endpoint lifetime, " + TTLExample + " (" + TTLDefault + ")."

	// TTLLifetime is the ipfs_upload_submit (Upload to IPFS app helper) TTL
	// description.
	TTLLifetime = "Presigned endpoint lifetime (" + TTLExample + "; " + TTLDefault + ")."

	// TTLDurationString is the raw-schema TTL variant naming the value as a
	// duration string explicitly.
	TTLDurationString = "Presigned endpoint lifetime as a duration string (" + TTLExample + "; " + TTLDefault + ")."

	// TTLMint is vault_put_file's TTL description (mint PUT endpoints only).
	TTLMint = "Presigned endpoint lifetime (" + TTLExample + "; " + TTLDefault + "). Only used with source mode mint."

	// TTLPut is upload_file's TTL description (the original presigned PUT
	// mint surface; same DefaultPresignTTL default).
	TTLPut = "Presigned endpoint lifetime (" + TTLExample + "; " + TTLDefault + "). Only used on transports that use presigned PUT endpoints."

	// TTLDownloadGetExample / TTLDownloadGetDefault derive from the download
	// coordinator's DefaultHTTPDownloadTTL (aligned with the upload TTL), so
	// the filedrop-GET wording is just as single-source as the PUT wording.
	TTLDownloadGetExample = "e.g. " + ttlPretty(transfer.DefaultHTTPDownloadTTL)

	TTLDownloadGetDefault = "default " + ttlPretty(transfer.DefaultHTTPDownloadTTL)

	// TTLDownloadGet is the filedrop GET lifetime description shared by
	// download_file and vault_get_file (sink=drop only).
	TTLDownloadGet = "Filedrop GET endpoint lifetime for sink=drop (" + TTLDownloadGetExample + "; " + TTLDownloadGetDefault + ")."
)

// SinkDescription is the canonical `sink` property description shared by the
// download_file and vault_get_file input schemas: local is the host-side
// write (available on every transport); drop mints the one-time HTTP GET
// filedrop link. Both input types carry it as a literal struct tag (Go struct
// tags cannot embed constants), so SinkPropertyTag is the canonical literal
// and the owning packages' parity tests pin both fields against it — drift is
// a test failure instead of silent divergence.
const SinkDescription = "Where the downloaded bytes land: local writes to a host-side output_path on the MCP server's disk (available on every transport); drop mints a one-time HTTP GET filedrop link to pull from out of band."

// SinkPropertyTag is the full byte-for-byte jsonschema tag for the shared
// `sink` property: the enum set AND the canonical SinkDescription.
const SinkPropertyTag = "enum=local,enum=drop,description=" + SinkDescription

// The canonical sink=drop benefit fragments: the filedrop mechanism clause and
// the optional curl/browser pull guidance, separately composable. Consumers
// name the mechanism everywhere; only surfaces with room for concrete plumbing
// append the pull guidance. Composed by the toolforge download/vault-get
// descriptions, the capabilities sink report, and the agent guide's download
// flows, so the drop prose has exactly ONE source and can never drift.
const (
	// DropLinkHTTP is the mechanism clause: what sink=drop produces. Worded so
	// it composes into both "to get a <DropLinkHTTP>" and "it returns <...>".
	DropLinkHTTP = "a one-time HTTP GET filedrop link to pull from out of band"

	// DropLinkPull is the optional concrete pull plumbing.
	DropLinkPull = "curl -o <url> or a browser link"

	// DropLinkBenefit composes the mechanism clause with the pull guidance in
	// parentheses — the full prose variant.
	DropLinkBenefit = DropLinkHTTP + " (" + DropLinkPull + ")"
)

// ProfileWriteBase is the shared vault WRITE-profile schema contract: the
// multi-profile requirement and the single-profile default. Every surface that
// mints a vault PUT (vault_put_file, the open_vault_manager launcher, and the
// vault_upload_submit app helper) composes one of these two constants.
const ProfileWriteBase = "Vault profile name to write into. Required when more than one profile is unlocked (omitting it returns profile_required and mints nothing); on a single-profile server it defaults to the active profile."

// ProfileWriteExtended is ProfileWriteBase plus the cross-vault targeting
// sentence, so all vault-write surfaces carry the same contract (including the
// app helper, whose shorter copy historically omitted it).
const ProfileWriteExtended = ProfileWriteBase + " Specify a different profile to store in another vault without changing the default."

// The shared wrap (single-file directory-root) contract: the HTML auto-name
// and explicit-name sentences carried by EVERY wrap property (upload_file's
// live BoolProperty schema, upload_data's pinned struct tag) and composed into
// the agent guide's htmlRootClause. uploadFileSchema composes WrapDesc
// directly; the upload_dataWrapTag literal and the guide htmlRootClause are
// pinned by the owning packages' tests.
const (
	// WrapHTMLAutoName is the auto-name behavior sentence.
	WrapHTMLAutoName = "When wrap=true and no name is given, HTML content is auto-named index.html so the site resolves at its root."

	// WrapExplicitName is the explicit-name behavior sentence: an explicit
	// name moves a wrapped HTML page under /name (the site resolves at
	// /name, not /).
	WrapExplicitName = "An explicit name such as 'starter-site' is honored as-is and the page is then only reachable at /starter-site, not /."

	// WrapDesc is upload_file's `wrap` property description — the canonical
	// website→directory-root contract. uploadFileSchema composes THIS
	// constant, so the meaning (a website must resolve to a directory root;
	// single-file uploads only) has exactly ONE live copy.
	WrapDesc = "Wrap a single file in a directory root so the CID is a directory (required when the upload is a website). " + WrapHTMLAutoName + " " + WrapExplicitName + " Only affects single-file uploads; directories and archive-converted uploads are already a directory root."

	// WrapDataDesc is upload_data's `wrap` property description: the
	// data-relay variant lead/tail (upload_data has no archive_mode field, so
	// every data-URI upload is single-file) with the SHARED auto-name and
	// explicit-name sentences.
	WrapDataDesc = "Wrap the single file in a directory root so the resulting CID is a directory. Required when the upload is a website (a website resolves to a directory, not a bare file). " + WrapHTMLAutoName + " " + WrapExplicitName + " True only affects single-file uploads; directory uploads are already a directory root."
)

// The shared website-archive root contract: the pre-upload layout check an
// agent performs on a site ZIP, and the publisher-side rejection clause. The
// toolforge upload_file description, the agent guide's siteBundleUpload
// fragment, and the guide's CID-structure rule all compose these instead of
// hand-copying the layout/rejection prose.
const (
	// ArchiveRootCheck is the layout check: index.html at the archive root,
	// never inside a wrapper directory.
	ArchiveRootCheck = "Before uploading a site ZIP, verify that index.html is at the archive root (not inside a wrapper directory)."

	// RootRejectClause is the rejection contract: websites_create/update
	// refuse a CID whose root lacks index.html or carries a wrapper directory.
	RootRejectClause = "websites_create/update will reject a CID whose root lacks index.html or is wrapped in a wrapper directory."
)

// The shared static-site ZIP bundle definition: what a static-site bundle ZIP
// contains (the asset list) and its single-directory-DAG upload behavior,
// composed by every surface that teaches the rule — the agent guide's
// upload-flow detail and summary, the guide's siteBundleUpload fragment, the
// toolforge upload_file site-ZIP clauses, and the archive_mode schema copy —
// so the asset enumeration and the one-DAG behavior have exactly ONE source
// and can never drift between the guide and the tool schemas.
const (
	// SiteZIPAssets is the canonical asset enumeration of a static-site
	// bundle ZIP.
	SiteZIPAssets = "index.html, CSS, JS, images, nested pages"

	// SiteZIPBundle composes the bundle description used inside parentheses
	// ("For a static site bundle (<SiteZIPBundle>)").
	SiteZIPBundle = "ZIP containing " + SiteZIPAssets

	// SiteZIPSingleDAG is the canonical single-directory-DAG behavior clause
	// ("a ZIP containing <assets> is a single directory DAG").
	SiteZIPSingleDAG = "a " + SiteZIPBundle + " is a single directory DAG"
)

// HostHeldNoMint is the canonical host-held-file no-mint behavior: when the
// host runtime can hand Pinner a file it already holds (a user attachment or
// an assistant-generated file), no presigned curl URL is minted for it — the
// agent passes the file reference instead. Composed by upload_file's
// host-file site-ZIP clause, vault_put_file's host-file clause, and the agent
// guide's siteBundleUpload fragment, normalizing their previously divergent
// wordings to ONE behavior sentence (a surface keeps its own lead-in, the
// behavior text has exactly ONE source).
const HostHeldNoMint = "Do NOT mint a presigned curl URL for a file the host already holds — pass the file reference instead."

// The shared mint-only relay/vault contract: the separate upload_url /
// upload_data relay tools are IPFS-only and never write the vault, so every
// vault surface that meets a public URL or inline bytes on a mint-only host
// must steer the agent to materialize them to an agent-local file first and
// mint + PUT them via vault_put_file's mint source. Composed by upload_file's
// vaultSourceModeDesc (feature-gated: FeatSourceURL/FeatSourceData) and the
// agent guide's vault upload flow / vault byte-route (tool- and host-gated),
// so the relay-not-vault guidance has exactly ONE source.
const (
	// RelayToolsNotVaultWrite is the relay-not-vault fact (lowercase opener,
	// no punctuation — a surface supplies its own casing and lead-in/period).
	RelayToolsNotVaultWrite = "the upload_url / upload_data relay tools pin to IPFS and do NOT write the vault"

	// RelayVaultMaterialize is the materialize-then-mint action (lowercase
	// opener, no punctuation — composed after a colon or em-dash lead-in).
	RelayVaultMaterialize = "write them to an agent-local file first, then vault_put_file source.mode=mint and PUT it to the returned url"
)
