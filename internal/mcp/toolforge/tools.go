// Package toolforge shim: pinner-domain tool descriptions and target sets.
// This is content, not machinery — the DSL (Static/When*/List) is provided by
// go.lumeweb.com/mcpforge, and gating that no single Feature expresses uses
// the hostenv predicate constructors via mcpforge's WhenPred.
package toolforge

import (
	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/mintcontract"
	"go.lumeweb.com/pinner-cli/internal/mcp/schematext"
)

// uploadFileDesc composes the upload_file tool description from a static
// preamble plus feature-gated segments, replacing the previous 7-way
// duplication of pre-built complete strings. At resolution time only
// segments whose required features are satisfied by the profile are
// concatenated.
const (
	// sinkDropUnavailable is the shared FeatSinkDrop-absent clause for the
	// download tools' DSL branches: ONE copy (download_file and
	// vault_get_file), never a per-description hand duplicate.
	sinkDropUnavailable = "The filedrop GET sink is unavailable on this transport."
)

var uploadFileDesc = Static(
	"Upload a file and pin it. "+mintcontract.FirstUpper(mintcontract.UploadPinnedCIDCompletion)+"; the wait flag waits for this upload's own pin operation.",
).
	When(hostenv.FeatFileHostInput,
		"Use `file` when the host already has the file (user-uploaded attachments AND assistant-generated files in the assistant's sandbox); the OpenAI runtime converts it to a temporary download_url + file_id this tool receives — the file is passed as-is, without base64 encoding, a data URI, or manually constructing the download_url object.",
	).
	When(hostenv.FeatSourceMint,
		// The mint-led clauses compose the shared dependency-neutral canon
		// (internal/mcp/mintcontract): the one-time PUT endpoint, the
		// not-stored-bytes fact, the PUT action, the upload_status poll with
		// the returned upload_handle, and the already-pinned-CID contract each
		// have one source.
		"Use source.mode=mint to get "+mintcontract.UploadMintEndpoint+". Mint "+mintcontract.UploadMintNoBytes+": "+mintcontract.UploadMintPutAction+", then "+mintcontract.UploadMintPoll+" — "+mintcontract.UploadMintNoPinsAdd+". For a website ZIP, mint holds the bytes as a raw archive unless you pass archive_mode=convert, so always pass archive_mode=convert for a site ZIP (or wrap=true for a single HTML page).",
	).
	When(hostenv.FeatSourcePath,
		"Use source.mode=path with a host-side file/directory/archive path.",
	).
	WhenPred(hostenv.TransportIs(hostenv.TransportOpenAI),
		"Use source.mode=url (server-fetchable HTTPS URL) or source.mode=data (RFC 2397 data: URI) — the server fetches/decodes and uploads them.",
	).
	// Both site-ZIP clauses compose the shared schematext
	// SiteZIPAssets bundle enumeration (the "(index.html + CSS/JS/images)"
	// shorthand was already-diverged wording normalized to the ONE asset list
	// the guide surfaces compose).
	When(hostenv.FeatFileHostInput,
		"Website ZIPs: if you already have a site ZIP on the host ("+schematext.SiteZIPAssets+"), call upload_file with file=<host file> and archive_mode=convert — the entire directory tree becomes one directory DAG whose CID you can publish directly to websites_create/update. Individual images/assets are not uploaded separately. "+schematext.HostHeldNoMint+" "+schematext.ArchiveRootCheck+" "+schematext.RootRejectClause,
	).
	When(hostenv.FeatSourcePath,
		"Website ZIPs: if you already have a site ZIP on the host ("+schematext.SiteZIPAssets+"), call upload_file with source.mode=path and archive_mode=convert — the entire directory tree becomes one directory DAG whose CID you can publish directly to websites_create/update. Individual images/assets are not uploaded separately. "+schematext.ArchiveRootCheck+" "+schematext.RootRejectClause,
	).
	When(hostenv.FeatSourceMint,
		"Website ZIPs: call upload_file with source.mode=mint and archive_mode=convert — the entire directory tree becomes one directory DAG whose CID you can publish directly to websites_create/update.",
	).
	WhenPred(hostenv.TransportIs(hostenv.TransportOpenAI),
		"If the upload fails with 'context canceled', retry with the same parameters — this is a transient host-side cancellation, not a file rejection. Poll upload_status with the returned handle.",
	)

// vaultPutFileDesc composes the vault_put_file tool description from a static
// preamble plus feature-gated segments. The mint branch states the full
// mint + PUT + poll contract (never "no curl needed") because it fires on
// every FeatSourceMint host — including Grok, which has no `file` parameter
// and CANNOT skip the curl. The "no curl needed" fragment from an earlier
// draft was removed: it only ever belonged on file-host-input hosts, and its
// presence here told Grok the mint was already done. The mint lead composes
// the ONE mintcontract.VaultMintLead clause; the PUT action composes
// mintcontract.UploadMintPutAction — no copy restates either by hand.
var vaultPutFileDesc = Static(
	"Store a file in the encrypted Pinner vault.",
).
	When(hostenv.FeatFileHostInput,
		// The no-mint tail composes the canonical schematext.HostHeldNoMint
		// behavior fragment (the same prose upload_file's host-file site-ZIP
		// clause and the agent guide's siteBundleUpload fragment compose);
		// the diverged "a presigned URL is not minted to curl..." passive
		// restatement was normalized to the shared wording.
		"If your host provides a generated file directly, pass it in the file input (a temporary download_url + file_id) and Pinner fetches and stores its bytes at vault_path; "+schematext.HostHeldNoMint,
	).
	When(hostenv.FeatSourceMint,
		// The mint-durability tail composes the shared dependency-neutral canon
		// (internal/mcp/mintcontract) — staging, flush job shape, polling, and
		// the no-upload_status fact each have one source.
		"Use source.mode=mint plus vault_path: mint returns "+mintcontract.VaultMintLead+". "+mintcontract.UploadMintPutAction+". "+mintcontract.FirstUpper(mintcontract.DurabilityCanon)+" "+mintcontract.UploadStatusContrast+".",
	).
	When(hostenv.FeatSourcePath,
		"In this co-located stdio mode you may instead set source.mode=path and source.path to a host-side file/directory/archive path; the server reads it directly.",
	).
	WhenPred(hostenv.TransportIs(hostenv.TransportOpenAI),
		"Over this transport you may instead set source.mode=url (a server-fetchable HTTPS download URL) or source.mode=data (an RFC 2397 data: URI).",
	).
	Static("vault_path may be any vault file path (e.g. vault:/docs/f.pdf).")

// UploadFileTargets are the per-profile description targets for upload_file.
// A single Fallback target with a DescFunc resolves the description
// dynamically against the platform profile, eliminating the need for
// pre-built complete-string variants.
var UploadFileTargets = []model.ToolTarget{{
	Visible:  true,
	DescFunc: DescResolver(uploadFileDesc.Resolve),
}}

// VaultPutFileTargets are the per-profile description targets for vault_put_file.
var VaultPutFileTargets = []model.ToolTarget{{
	Visible:  true,
	DescFunc: DescResolver(vaultPutFileDesc.Resolve),
}}

// downloadFileDesc composes the download_file description: sink=local is
// available on every transport, while the drop filedrop sink is only
// advertised when the resolved profile has a reachable HTTP mux
// (FeatSinkDrop). The two clauses mirror downloadFileDescription's previous
// if/else so the startup and per-request surfaces cannot diverge.
var downloadFileDesc = Static(
	"Download IPFS content (CID or CID/path) as a file. Set sink=local to write the bytes to a host-side output_path on the MCP server's own disk (available on every transport)",
).
	WhenSep(SepSpace, hostenv.FeatSinkDrop,
		// The drop clause composes the canonical schematext drop-link
		// fragments (mechanism + pull guidance) — the same prose the
		// capabilities report and the agent guide's download flow compose.
		"or sink=drop to get "+schematext.DropLinkBenefit+".",
	).
	UnlessSep(SepSentence, hostenv.FeatSinkDrop,
		sinkDropUnavailable,
	)

// DownloadFileTargets are the per-profile description targets for download_file.
var DownloadFileTargets = []model.ToolTarget{{
	Visible:  true,
	DescFunc: DescResolver(downloadFileDesc.Resolve),
}}

// vaultGetFileDesc mirrors downloadFileDesc for vault_get_file, keeping the
// encrypted-vault preamble fixed and gating the drop sink clause on
// FeatSinkDrop.
var vaultGetFileDesc = Static(
	"Download a file from your encrypted Pinner vault by vault_path (e.g. vault:/docs/f.pdf). Set sink=local to write the decrypted bytes to a host-side output_path on the MCP server's own disk (available on every transport)",
).
	// The vault-get drop clause composes the mechanism-only canonical
	// fragment (no concrete curl/browser plumbing in the vault description) —
	// the same schematext.DropLinkHTTP the other drop surfaces compose.
	WhenSep(SepSpace, hostenv.FeatSinkDrop,
		"or sink=drop to get "+schematext.DropLinkHTTP+".",
	).
	UnlessSep(SepSentence, hostenv.FeatSinkDrop,
		sinkDropUnavailable,
	)

// VaultGetFileTargets are the per-profile description targets for vault_get_file.
var VaultGetFileTargets = []model.ToolTarget{{
	Visible:  true,
	DescFunc: DescResolver(vaultGetFileDesc.Resolve),
}}
