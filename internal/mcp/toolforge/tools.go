package toolforge

import (
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// sourceURLData returns the two features that co-occur for the OpenAI tunnel
// relay source (url + data).
func sourceURLData() []hostenv.Feature {
	return []hostenv.Feature{hostenv.FeatSourceURL, hostenv.FeatSourceData}
}

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
		"Use source.mode=mint to get a one-time presigned HTTP PUT endpoint; stream your file's bytes to it with curl, then poll upload_status with the returned upload_handle. archive_mode and wrap are honored on mint too: request archive_mode=convert to have the PUT bytes extracted into a directory DAG when they are an archive (or wrap=true to wrap a single file), exactly as on host-file/path/url/data sources.",
	).
	When(hostenv.FeatSourcePath,
		"Use source.mode=path with a host-side file/directory/archive path.",
	).
	WhenAny(sourceURLData(),
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
	WhenAny(sourceURLData(),
		"If the upload fails with 'context canceled', retry with the same parameters — this is a transient host-side cancellation, not a file rejection. Poll upload_status with the returned handle.",
	).
	When(hostenv.FeatSourcePath,
		"If the upload fails with 'context canceled', retry with the same parameters — this is a transient host-side cancellation, not a file rejection. Poll upload_status with the returned handle.",
	)

// vaultPutFileDesc composes the vault_put_file tool description from a static
// preamble plus feature-gated segments.
var vaultPutFileDesc = Static(
	"Store a file in the encrypted Pinner vault.",
).
	When(hostenv.FeatFileHostInput,
		"If your host provides a generated file directly, pass it in the file input (a temporary download_url + file_id) and Pinner fetches and stores its bytes at vault_path. Do NOT mint a presigned URL to curl a file when the host already holds it; pass the file reference instead.",
	).
	When(hostenv.FeatSourceMint,
		"Otherwise set source.mode=mint to get a one-time presigned HTTP PUT endpoint bound to vault_path and stream your file's bytes to it with curl.",
	).
	When(hostenv.FeatSourcePath,
		"In this co-located stdio mode you may instead set source.mode=path and source.path to a host-side file/directory/archive path; the server reads it directly.",
	).
	WhenAny(sourceURLData(),
		"Over this transport you may instead set source.mode=url (a server-fetchable HTTPS download URL) or source.mode=data (an RFC 2397 data: URI).",
	).
	WhenDash(hostenv.FeatSourceMint,
		"no curl needed for a file the host already owns.",
	).
	WhenDash(hostenv.FeatSourcePath,
		"no curl needed for a file the host already owns.",
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
