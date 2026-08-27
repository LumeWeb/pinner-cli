package toolforge

import (
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// uploadFileDesc composes the upload_file tool description from a static
// preamble plus feature-gated segments, replacing the previous 7-way
// duplication of pre-built complete strings. At resolution time only
// segments whose required features are satisfied by the profile are
// concatenated.
var uploadFileDesc = Static(
	"Upload a file and pin it. The returned CID is already pinned: do NOT call pins_add afterward; the wait flag waits for this upload's own pin operation.",
).
	When(hostenv.FeatFileHostInput,
		"MUST use `file` when the host already has the file (user-uploaded attachments AND assistant-generated files in the assistant's sandbox); the OpenAI runtime converts it to a temporary download_url + file_id this tool receives — do NOT base64-encode, create a data URI, or manually construct the download_url object.",
	).
	When(hostenv.FeatSourceMint,
		"Use source.mode=mint to get a one-time presigned HTTP PUT endpoint. Mint does NOT store bytes: PUT your agent-local file to the returned url (curl -sS -T <file> \"<url>\"), then poll upload_status with the returned upload_handle until it reports completed — the completed CID is already pinned; do not call pins_add. For a website ZIP, mint holds the bytes as a raw archive unless you pass archive_mode=convert, so always pass archive_mode=convert for a site ZIP (or wrap=true for a single HTML page).",
	).
	When(hostenv.FeatSourcePath,
		"Use source.mode=path with a host-side file/directory/archive path.",
	).
	WhenTransport(hostenv.TransportOpenAI,
		"Use source.mode=url (server-fetchable HTTPS URL) or source.mode=data (RFC 2397 data: URI) — the server fetches/decodes and uploads them.",
	).
	When(hostenv.FeatFileHostInput,
		"Website ZIPs: if you already have a site ZIP on the host (index.html + CSS/JS/images), call upload_file with file=<host file> and archive_mode=convert — the entire directory tree becomes one directory DAG whose CID you can publish directly to websites_create/update. Do NOT upload individual images/assets, and do NOT mint a presigned curl URL for a file your host already holds. Before uploading a site ZIP, verify that index.html is at the archive root (not inside a wrapper directory) — websites_create/update will reject a CID whose root lacks index.html.",
	).
	When(hostenv.FeatSourcePath,
		"Website ZIPs: if you already have a site ZIP on the host (index.html + CSS/JS/images), call upload_file with source.mode=path and archive_mode=convert — the entire directory tree becomes one directory DAG whose CID you can publish directly to websites_create/update. Do NOT upload individual images/assets. Before uploading a site ZIP, verify that index.html is at the archive root (not inside a wrapper directory) — websites_create/update will reject a CID whose root lacks index.html.",
	).
	When(hostenv.FeatSourceMint,
		"Website ZIPs: call upload_file with source.mode=mint and archive_mode=convert — the entire directory tree becomes one directory DAG whose CID you can publish directly to websites_create/update.",
	).
	WhenTransport(hostenv.TransportOpenAI,
		"If the upload fails with 'context canceled', retry with the same parameters — this is a transient host-side cancellation, not a file rejection. Poll upload_status with the returned handle.",
	).
	When(hostenv.FeatSourcePath,
		"If the upload fails with 'context canceled', retry with the same parameters — this is a transient host-side cancellation, not a file rejection. Poll upload_status with the returned handle.",
	)

// vaultPutFileDesc composes the vault_put_file tool description from a static
// preamble plus feature-gated segments. The mint branch states the full
// mint + PUT + poll contract (never "no curl needed") because it fires on
// every FeatSourceMint host — including Grok, which has no `file` parameter
// and CANNOT skip the curl. The "no curl needed" fragment from an earlier
// draft was removed: it only ever belonged on file-host-input hosts, and its
// presence here told Grok the mint was already done.
var vaultPutFileDesc = Static(
	"Store a file in the encrypted Pinner vault.",
).
	When(hostenv.FeatFileHostInput,
		"If your host provides a generated file directly, pass it in the file input (a temporary download_url + file_id) and Pinner fetches and stores its bytes at vault_path. Do NOT mint a presigned URL to curl a file when the host already holds it; pass the file reference instead.",
	).
	When(hostenv.FeatSourceMint,
		"Use source.mode=mint plus vault_path: mint returns a one-time presigned PUT url bound to vault_path (it has NOT stored bytes yet). PUT the agent-local file to the returned url (curl -sS -T <file> \"<url>\"); the vault write is synchronous, so the PUT response carries the completed vault write directly — there is no upload_status to poll.",
	).
	When(hostenv.FeatSourcePath,
		"In this co-located stdio mode you may instead set source.mode=path and source.path to a host-side file/directory/archive path; the server reads it directly.",
	).
	WhenTransport(hostenv.TransportOpenAI,
		"Over this transport you may instead set source.mode=url (a server-fetchable HTTPS download URL) or source.mode=data (an RFC 2397 data: URI).",
	).
	Static("vault_path may be any vault file path (e.g. vault:/docs/f.pdf).")

// UploadFileTargets are the per-profile description targets for upload_file.
// A single Fallback target with a DescFunc resolves the description
// dynamically against the platform profile, eliminating the need for
// pre-built complete-string variants.
var UploadFileTargets = []model.ToolTarget{{
	Visible:  true,
	DescFunc: uploadFileDesc.Resolve,
}}

// VaultPutFileTargets are the per-profile description targets for vault_put_file.
var VaultPutFileTargets = []model.ToolTarget{{
	Visible:  true,
	DescFunc: vaultPutFileDesc.Resolve,
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
		"or sink=drop to get a one-time HTTP GET filedrop link to pull from out of band (curl -o <url> or a browser link).",
	).
	UnlessSep(SepSentence, hostenv.FeatSinkDrop,
		"The filedrop GET sink is unavailable on this transport.",
	)

// DownloadFileTargets are the per-profile description targets for download_file.
var DownloadFileTargets = []model.ToolTarget{{
	Visible:  true,
	DescFunc: downloadFileDesc.Resolve,
}}

// vaultGetFileDesc mirrors downloadFileDesc for vault_get_file, keeping the
// encrypted-vault preamble fixed and gating the drop sink clause on
// FeatSinkDrop.
var vaultGetFileDesc = Static(
	"Download a file from your encrypted Pinner vault by vault_path (e.g. vault:/docs/f.pdf). Set sink=local to write the decrypted bytes to a host-side output_path on the MCP server's own disk (available on every transport)",
).
	WhenSep(SepSpace, hostenv.FeatSinkDrop,
		"or sink=drop to get a one-time HTTP GET filedrop link to pull from out of band.",
	).
	UnlessSep(SepSentence, hostenv.FeatSinkDrop,
		"The filedrop GET sink is unavailable on this tunnel transport.",
	)

// VaultGetFileTargets are the per-profile description targets for vault_get_file.
var VaultGetFileTargets = []model.ToolTarget{{
	Visible:  true,
	DescFunc: vaultGetFileDesc.Resolve,
}}
