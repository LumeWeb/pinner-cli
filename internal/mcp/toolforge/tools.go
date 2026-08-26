package toolforge

import (
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// Description targets for each transport-aware tool. Each target is a
// complete, self-contained description string keyed by the features the
// connected platform must have for the target to be eligible. The resolver
// (ResolveDescription) picks the most specific match — the target with the
// most required features that the platform satisfies.

// UploadFileTargets are the description variants for upload_file.
// The variants differ by which source modes the platform supports.
// Hosts that also support FeatFileHostInput get a description that
// emphasizes the file parameter; the generic variant does not.
var UploadFileTargets = []model.ToolTarget{
	// OpenAI/ChatGPT tunnel: file + url/data relay, no HTTP mux.
	Target(
		"Upload a file and pin it. The returned CID is already pinned: do NOT call pins_add afterward; the wait flag waits for this upload's own pin operation. MUST use `file` when the host already has the file (user-uploaded attachments AND assistant-generated files in the assistant's sandbox); the OpenAI runtime converts it to a temporary download_url + file_id this tool receives — do NOT base64-encode, create a data URI, or manually construct the download_url object. Website ZIPs: if you already have a site ZIP on the host (index.html + CSS/JS/images), call upload_file with file=<host file> and archive_mode=convert — the entire directory tree becomes one directory DAG whose CID you can publish directly to websites_create/update. Do NOT upload individual images/assets, and do NOT mint a presigned curl URL for a file your host already holds. Before uploading a site ZIP, verify that index.html is at the archive root (not inside a wrapper directory) — websites_create/update will reject a CID whose root lacks index.html. Fallback: source.mode=url (server-fetchable HTTPS URL) or source.mode=data (RFC 2397 data: URI) — the server fetches/decodes and uploads them. If the upload fails with 'context canceled', retry with the same parameters — this is a transient host-side cancellation, not a file rejection. Poll upload_status with the returned handle.",
		hostenv.FeatFileHostInput, hostenv.FeatSourceURL, hostenv.FeatSourceData,
	),

	// OpenAI/ChatGPT HTTP: file + mint presigned PUT.
	Target(
		"Upload a file and pin it. The returned CID is already pinned: do NOT call pins_add afterward; the wait flag waits for this upload's own pin operation. MUST use `file` when the host already has the file (user-uploaded attachments AND assistant-generated files in the assistant's sandbox); the OpenAI runtime converts it to a temporary download_url + file_id this tool receives — do NOT base64-encode, create a data URI, or manually construct the download_url object. Website ZIPs: if you already have a site ZIP on the host (index.html + CSS/JS/images), call upload_file with file=<host file> and archive_mode=convert — the entire directory tree becomes one directory DAG whose CID you can publish directly to websites_create/update. Do NOT upload individual images/assets, and do NOT mint a presigned curl URL for a file your host already holds. Before uploading a site ZIP, verify that index.html is at the archive root (not inside a wrapper directory) — websites_create/update will reject a CID whose root lacks index.html. Fallback: source.mode=mint returns a one-time presigned HTTP PUT endpoint; stream the bytes with curl, then poll upload_status with the returned upload_handle. archive_mode and wrap are honored on mint too: request archive_mode=convert to have the PUT bytes extracted into a directory DAG when they are an archive (or wrap=true to wrap a single file), exactly as on host-file/path/url/data sources.",
		hostenv.FeatFileHostInput, hostenv.FeatSourceMint,
	),

	// Stdio co-located with file host input (theoretical: Claude/Cursor
	// don't currently support file refs, but if a future host does,
	// this variant covers it).
	Target(
		"Upload a file and pin it. The returned CID is already pinned: do NOT call pins_add afterward; the wait flag waits for this upload's own pin operation. MUST use `file` when the host already has the file (user-uploaded attachments AND assistant-generated files in the assistant's sandbox); the OpenAI runtime converts it to a temporary download_url + file_id this tool receives — do NOT base64-encode, create a data URI, or manually construct the download_url object. Fallback: source.mode=path with a host-side file/directory/archive path. Website ZIPs: if you already have a site ZIP on the host (index.html + CSS/JS/images), call upload_file with file=<host file> and archive_mode=convert — the entire directory tree becomes one directory DAG whose CID you can publish directly to websites_create/update. Do NOT upload individual images/assets, and do NOT mint a presigned curl URL for a file your host already holds. Before uploading a site ZIP, verify that index.html is at the archive root (not inside a wrapper directory) — websites_create/update will reject a CID whose root lacks index.html. If the upload fails with 'context canceled', retry with the same parameters — this is a transient host-side cancellation, not a file rejection. Poll upload_status with the returned handle.",
		hostenv.FeatFileHostInput, hostenv.FeatSourcePath,
	),

	// Stdio generic: path only, no file host input.
	Target(
		"Upload a file and pin it. The returned CID is already pinned: do NOT call pins_add afterward; the wait flag waits for this upload's own pin operation. Use source.mode=path with a host-side file/directory/archive path. Website ZIPs: if you already have a site ZIP on the host (index.html + CSS/JS/images), call upload_file with source.mode=path and archive_mode=convert — the entire directory tree becomes one directory DAG whose CID you can publish directly to websites_create/update. Do NOT upload individual images/assets. Before uploading a site ZIP, verify that index.html is at the archive root (not inside a wrapper directory) — websites_create/update will reject a CID whose root lacks index.html. If the upload fails with 'context canceled', retry with the same parameters — this is a transient host-side cancellation, not a file rejection. Poll upload_status with the returned handle.",
		hostenv.FeatSourcePath,
	),

	// HTTP generic: mint only, no file host input.
	Target(
		"Upload a file and pin it. The returned CID is already pinned: do NOT call pins_add afterward; the wait flag waits for this upload's own pin operation. Use source.mode=mint to get a one-time presigned HTTP PUT endpoint; stream your file's bytes to it with curl, then poll upload_status with the returned upload_handle. archive_mode and wrap are honored on mint: request archive_mode=convert to have the PUT bytes extracted into a directory DAG when they are an archive (or wrap=true to wrap a single file). Website ZIPs: call upload_file with source.mode=mint and archive_mode=convert — the entire directory tree becomes one directory DAG whose CID you can publish directly to websites_create/update.",
		hostenv.FeatSourceMint,
	),

	// OpenAI tunnel without file host input (theoretical, no current
	// profile matches, but covers future hosts).
	Target(
		"Upload a file and pin it. The returned CID is already pinned: do NOT call pins_add afterward; the wait flag waits for this upload's own pin operation. Use source.mode=url (server-fetchable HTTPS URL) or source.mode=data (RFC 2397 data: URI) — the server fetches/decodes and uploads them. If the upload fails with 'context canceled', retry with the same parameters — this is a transient host-side cancellation, not a file rejection. Poll upload_status with the returned handle.",
		hostenv.FeatSourceURL, hostenv.FeatSourceData,
	),

	// Universal fallback: no transport-specific features known.
	Fallback(
		"Upload a file and pin it. The returned CID is already pinned: do NOT call pins_add afterward; the wait flag waits for this upload's own pin operation. If the host provides a file reference, pass it via the `file` parameter; otherwise use source with the mode appropriate for this transport. Website ZIPs: use archive_mode=convert to extract the archive into a directory DAG whose CID you can publish directly to websites_create/update. Verify that index.html is at the archive root before uploading. If the upload fails with 'context canceled', retry with the same parameters. Poll upload_status with the returned handle.",
	),
}

// VaultPutFileTargets are the description variants for vault_put_file.
var VaultPutFileTargets = []model.ToolTarget{
	// OpenAI/ChatGPT tunnel: file + url/data relay.
	Target(
		"Store a file in the encrypted Pinner vault. If your host provides a generated file directly, pass it in the file input (a temporary download_url + file_id) and Pinner fetches and stores its bytes at vault_path. Over this OpenAI-tunnel transport you may instead set source.mode=url (a server-fetchable HTTPS download URL) or source.mode=data (an RFC 2397 data: URI). vault_path may be any vault file path (e.g. vault:/docs/f.pdf).",
		hostenv.FeatFileHostInput, hostenv.FeatSourceURL, hostenv.FeatSourceData,
	),

	// OpenAI/ChatGPT HTTP: file + mint presigned PUT.
	Target(
		"Store a file in the encrypted Pinner vault. If your host provides a generated file directly, pass it in the file input (a temporary download_url + file_id) and Pinner fetches and stores its bytes at vault_path — no curl needed for a file the host already owns. Do NOT mint a presigned URL to curl a file when the host already holds it; pass the file reference instead. Otherwise over this HTTP/tunnel transport, set source.mode=mint to get a one-time presigned HTTP PUT endpoint bound to vault_path and stream your file's bytes to it with curl. vault_path may be any vault file path (e.g. vault:/docs/f.pdf).",
		hostenv.FeatFileHostInput, hostenv.FeatSourceMint,
	),

	// Stdio with file host input (theoretical).
	Target(
		"Store a file in the encrypted Pinner vault. If your host provides the file to you directly, pass it in the file input (a temporary download_url + file_id) and Pinner fetches and stores its bytes at vault_path. In this co-located stdio mode you may instead set source.mode=path and source.path to a host-side file/directory/archive path; the server reads it directly. vault_path may be any vault file path (e.g. vault:/docs/f.pdf).",
		hostenv.FeatFileHostInput, hostenv.FeatSourcePath,
	),

	// Stdio generic: path only, no file host input.
	Target(
		"Store a file in the encrypted Pinner vault. Set source.mode=path and source.path to a host-side file/directory/archive path; the server reads it directly. vault_path may be any vault file path (e.g. vault:/docs/f.pdf).",
		hostenv.FeatSourcePath,
	),

	// HTTP generic: mint only, no file host input.
	Target(
		"Store a file in the encrypted Pinner vault. Set source.mode=mint to get a one-time presigned HTTP PUT endpoint bound to vault_path and stream your file's bytes to it with curl. vault_path may be any vault file path (e.g. vault:/docs/f.pdf).",
		hostenv.FeatSourceMint,
	),

	// OpenAI tunnel without file host input (theoretical).
	Target(
		"Store a file in the encrypted Pinner vault. Set source.mode=url (a server-fetchable HTTPS download URL) or source.mode=data (an RFC 2397 data: URI). vault_path may be any vault file path (e.g. vault:/docs/f.pdf).",
		hostenv.FeatSourceURL, hostenv.FeatSourceData,
	),

	// Universal fallback: no transport-specific features known.
	Fallback(
		"Store a file in the encrypted Pinner vault. If the host provides a file reference, pass it via the `file` parameter; otherwise use source with the mode appropriate for this transport. vault_path may be any vault file path (e.g. vault:/docs/f.pdf).",
	),
}
