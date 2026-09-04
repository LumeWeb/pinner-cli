package mcp

import (
	"context"
	"strings"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
	"go.lumeweb.com/pinner-cli/internal/mcp/wizard"
)

// The guide wire-model types (AgentGuide, GuideFlow, GuideDecision,
// GuideBranch) live in toolforge — the platform-DSL package — so the guide is
// composed by the same DSL that builds schemas and descriptions. These aliases
// let call-sites and tests keep referencing the mcp-package names without
// maintaining a parallel definition.
type (
	AgentGuide    = toolforge.AgentGuide
	GuideFlow     = toolforge.GuideFlow
	GuideDecision = toolforge.GuideDecision
	GuideBranch   = toolforge.GuideBranch
)

// profileFromRequest safely extracts the PlatformProfile from a tool request.
// If the request has no Caps or no Profile (e.g. tests invoking handlers
// directly), it returns a default stdio generic profile.
func profileFromRequest(request model.ToolRequest) *hostenv.PlatformProfile {
	if request.Caps != nil && request.Caps.Profile != nil {
		return request.Caps.Profile
	}
	p := hostenv.ProfileStdioGeneric
	return &p
}

// sourceModePrefix prefixes a transfer.FileSourceMode value into the
// "source.mode=X" label the guide surfaces. The mode names themselves come
// from the shared transfer enum so this guide can never drift from what
// capabilities() and the upload_file schema advertise.
const sourceModePrefix = "source.mode="

// guideSourceModes returns the transport-scoped source modes the resolved
// profile actually supports. It derives from the transport (matching
// capabilities().source_modes and the upload_file schema enum), never from the
// profile's feature flags: the separate upload_data / upload_url TOOLS are
// gated on FeatSourceData/FeatSourceURL, but upload_file's `source` enum is a
// pure function of the transport (mint on HTTP, path on stdio, url/data on the
// OpenAI tunnel). Deriving from features here would advertise a source mode
// the upload_file schema on that transport cannot accept.
func guideSourceModes(profile *hostenv.PlatformProfile) []string {
	switch profile.Transport {
	case hostenv.TransportStdio:
		return []string{sourceModePrefix + string(transfer.SourcePath)}
	case hostenv.TransportOpenAI:
		// The tunnel's url + data pair is a single relay label in the guide.
		return []string{sourceModePrefix + string(transfer.SourceURL) + "/" + string(transfer.SourceData)}
	default: // TransportHTTP
		return []string{sourceModePrefix + string(transfer.SourceMint)}
	}
}

// sourceModesText joins guideSourceModes with "or" for inline use in
// description segments.
func sourceModesText(profile *hostenv.PlatformProfile) string {
	return strings.Join(guideSourceModes(profile), " or ")
}

// uploadDetailDesc composes the upload flow detail string from feature-gated
// segments, replacing the previous string concatenation. The returned CID is
// already pinned, so it must never steer an agent to pins_add.
var uploadDetailDesc = toolforge.Static(
	"Check capabilities to pick the byte source THIS client is told to use.",
).
	When(hostenv.FeatFileHostInput,
		"If capabilities' file_input_policy is host_file_first (only when your client can hand Pinner a {download_url, file_id} file object), pass a host-provided file reference directly. Otherwise use a transport-scoped source: {{SOURCES}}.",
	).
	Unless(hostenv.FeatFileHostInput,
		"Use a transport-scoped source: {{SOURCES}}.",
	).
	Static("The returned CID is already pinned — use it directly in websites_create/update; do NOT call pins_add after an upload").
	WhenSentence(hostenv.FeatSourceMint,
		"Mint (source.mode=mint) has NOT stored bytes when upload_file returns — it only mints url + upload_handle.",
	).
	WhenSentence(hostenv.FeatSourceMint,
		"1) PUT your agent-local file to the returned url (curl -sS -T <file> \"<url>\")",
	).
	WhenSentence(hostenv.FeatSourceMint,
		"2) poll upload_status with the returned upload_handle until it reports completed",
	).
	WhenSentence(hostenv.FeatSourceMint,
		"3) the completed CID is already pinned — use it directly; do NOT call pins_add. Treat the mint response as the START of the upload, not the end.",
	).
	ListWhenAny([]hostenv.Feature{hostenv.FeatSourceURL, hostenv.FeatSourceData},
		toolforge.List(toolforge.ListNumbered).
			Intro("Pick the byte route in this order:").
			ItemWhen(hostenv.FeatSourceMint, "a file you can read locally → upload_file mint + host PUT + upload_status").
			ItemWhen(hostenv.FeatSourceURL, "bytes already at a public HTTPS URL → upload_url (server fetch; do not download then re-upload)").
			ItemWhen(hostenv.FeatSourceData, "only raw bytes, no file, no URL → upload_data (RFC 2397 data: URI) — last resort; never base64-encode a real file"),
	).
	StaticSentence("Static site bundle rule: a ZIP containing index.html, CSS, JS, images, or nested pages is a single directory DAG — call upload_file").
	When(hostenv.FeatFileHostInput,
		"with the host file argument (or a convert source) and archive_mode=convert",
	).
	Unless(hostenv.FeatFileHostInput,
		"with a convert source ({{SOURCES}}) and archive_mode=convert",
	).
	StaticList("not individual assets.")

// vaultUploadDetailDesc composes the vault upload flow detail string.
var vaultUploadDetailDesc = toolforge.Static(
	"Check capabilities to pick the byte source THIS client is told to use.",
).
	When(hostenv.FeatFileHostInput,
		"If capabilities' file_input_policy is host_file_first (only when your client can hand Pinner a {download_url, file_id} file object), pass a host-provided file reference directly. Otherwise use a transport-scoped source ({{SOURCES}}) plus the destination vault_path.",
	).
	Unless(hostenv.FeatFileHostInput,
		"Use a transport-scoped source ({{SOURCES}}) plus the destination vault_path.",
	).
	When(hostenv.FeatSourceMint,
		"When using source.mode=mint + vault_path, mint returns a one-time presigned PUT url bound to vault_path (it has NOT stored bytes yet). PUT the agent-local file to the returned url; the PUT returns quickly after staging the bytes locally (status: staged). The file is immediately readable from this instance; durability on Sia happens in the background or via the vault_flush tool — vault_flush is non-blocking and returns an accepted job { job_id, profile, path? }, so poll vault_flush_status(job_id) or vault_stat until status: durable when durability is needed before sharing; there is no upload_status to poll.",
	).
	WhenSentence(hostenv.FeatSourceMint,
		"On this mint-only transport there is no direct vault path for a public URL or inline bytes: materialize them to an agent-local file first, then vault_put_file(source.mode=mint) + PUT.",
	).
	When(hostenv.FeatSourceURL,
		"The separate upload_url tool is IPFS-only, not a vault write — do not invent a 'vault a CID' step.",
	).
	When(hostenv.FeatSourceData,
		"The separate upload_data tool is IPFS-only, not a vault write — do not invent a 'vault a CID' step.",
	).
	WhenTransportSep(toolforge.SepSentence, hostenv.TransportOpenAI,
		"The separate upload_url / upload_data tools pin to IPFS and are NOT vault writes: over this tunnel transport vault_put_file takes public-URL or raw-inline bytes via its own url/data source plus the destination vault_path. Do not invent a 'vault a CID' step.",
	)

// downloadDetailDesc composes the download flow detail string. sink=local is
// always available but writes to the MCP server's own disk; for a remote agent
// (not co-located) that path is invisible, so drop is the preferred sink.
var downloadDetailDesc = toolforge.Static(
	"Read capabilities' download_sink_modes; call download_file with ipfs_path (CID or CID/path) using a supported sink.",
).
	WhenSentence(hostenv.FeatSinkDrop,
		"Prefer sink=drop: it returns a one-time HTTP GET filedrop link to pull into your sandbox with curl -o or a browser.",
	).
	UnlessSep(toolforge.SepSentence, hostenv.FeatCoLocated,
		"sink=local writes to a path on the MCP server's own disk and is NOT visible to a remote agent like this one — do not look for the downloaded file in your sandbox.",
	).
	UnlessSep(toolforge.SepSentence, hostenv.FeatSinkDrop,
		"On this transport, sink=local is the only sink offered.",
	)

// vaultDownloadDetailDesc composes the vault download flow detail string.
var vaultDownloadDetailDesc = toolforge.Static(
	"Read capabilities' download_sink_modes and ensure the vault is unlocked; call vault_get_file with vault_path using a supported sink.",
).
	WhenSentence(hostenv.FeatSinkDrop,
		"Prefer sink=drop: it returns a one-time HTTP GET filedrop link to pull into your sandbox.",
	).
	UnlessSep(toolforge.SepSentence, hostenv.FeatCoLocated,
		"sink=local writes the decrypted bytes to the MCP server's own disk and is NOT visible to a remote agent like this one.",
	).
	UnlessSep(toolforge.SepSentence, hostenv.FeatSinkDrop,
		"On this transport, sink=local is the only sink offered.",
	)

// guideSummary is the guide's opening orientation: start here, check state,
// follow flows, treat a static website ZIP as a single directory DAG, and take
// the byte path capabilities actually reports for THIS host. The byte-path and
// wizard-orientation tails are feature-gated so a host without a `file`
// parameter (e.g. Grok) never sees a "prefer the file parameter" clause, and a
// host without elicitation is not steered into a wizard. The {{SOURCES}} token
// is substituted per profile.
var guideSummary = toolforge.Static(
	"Start here. Drive Pinner through these primary flows; each step is a tool. Check the current state first, then follow the matching flow. A static website ZIP (index.html, CSS, JS, images, nested pages) is always a single directory DAG: call upload_file",
).
	When(hostenv.FeatFileHostInput,
		"with a host file argument IF capabilities' file_input_policy is host_file_first (your client can hand Pinner a {download_url, file_id} object), otherwise a convert-capable transport source",
	).
	Unless(hostenv.FeatFileHostInput,
		"with a convert-capable transport source ({{SOURCES}})",
	).
	StaticList("then publish the resulting directory CID.").
	When(hostenv.FeatFileHostInput,
		"Follow the byte path capabilities reports: when file_input_policy is host_file_first, prefer the `file` parameter (user attachments AND assistant-generated sandbox files) over a transport source; otherwise use a transport-scoped source ({{SOURCES}}). Do NOT invent an OpenAI download_url/file_id or base64-encode a file as a data URI.",
	).
	Unless(hostenv.FeatFileHostInput,
		"This host has no `file` parameter it can fill: use a transport-scoped source ({{SOURCES}}). Do NOT invent a file_id or OpenAI download_url, and do NOT base64-encode a file as upload_data.",
	).
	When(hostenv.FeatSourceMint,
		"For source.mode=mint, completion differs by tool: upload_file is asynchronous — PUT the agent-local file to the returned url, then poll upload_status; vault_put_file is non-blocking — PUT the file and it returns after staging locally (status: staged), with durability on Sia happening in the background or via the vault_flush tool (which is itself non-blocking and returns an accepted job { job_id, profile, path? }), so poll vault_flush_status(job_id) or vault_stat until status: durable when durability is needed before sharing; there is no upload_status poll (see the upload and vault_upload flows).",
	).
	When(hostenv.FeatSourcePath,
		"For source.mode=path, point the source at the host-side file/directory/archive path — the server reads it directly, so there is no PUT.",
	).
	WhenAll([]hostenv.Feature{hostenv.FeatSourceMint, hostenv.FeatSourceURL, hostenv.FeatSourceData},
		"Byte route order is in the upload flow: a local file → mint + PUT, a public HTTPS URL → upload_url, raw bytes → upload_data.",
	).
	StaticSentence("For autonomous website publishing after an upload, run the publish_website flow directly. For explicitly requested guided website onboarding (human-in-the-loop, step-by-step DNS setup), use the website-onboarding prompt and the websites_wizard tools (websites_wizard_start → websites_wizard_step) instead. Once a wizard session is active, stay in it: always call the returned next_step_schema via the wizard step tool — do not abandon the wizard to rediscover low-level tools.").
	When(hostenv.FeatMCPApps,
		"This host renders MCP Apps: interactive app views are available via open_app for human-facing interactions (vault_browser, sso_signin, pin_creator, upload_manager, pin_list, account, vault_create, vault_restore, account_password, account_email). Prefer headless primitives for autonomous workflows; call open_app only when a human-facing screen is needed.")

// guideArchiveInvariant and guideCIDStructure are the two operational website
// rules every agent must honor. Kept as named fragments so branch guidance can
// cite the same wrapper rule without duplicating the prose.
var (
	guideArchiveInvariant = "Website archive invariant: before publishing any generated static-site archive, verify that index.html is at the archive root. Never publish an archive where the entire site is wrapped in a single parent directory (e.g. site.zip/mysite/index.html). The correct layout is site.zip/index.html. If the first path component wraps the entire site, rebuild the archive from the directory's contents, not the directory itself."
	guideCIDStructure     = "Website CID structure: a website CID must be a directory whose root contains index.html. Gateways serve /index.html at the directory path. Uploading an archive with archive_mode=convert produces a directory CID whose structure mirrors the archive — if the archive has a wrapper directory, the CID will too, and the site will not resolve at /. The tool will reject a CID that has no root index.html or is wrapped in a single parent directory."
)

// byteRouteDecision composes the "where are the bytes?" chooser as a guide
// Decision so the flow's steps (not just its detail) can produce a CID from any
// source the host actually registers. Each branch is feature-gated and ends with
// real upload tools — every step resolves to a genuine tool, so the guide's
// "steps are real tools" invariant holds.
//
// The branches are intentionally the union of every profile's route; each host
// resolves to only the branches its features enable, so uploaded bytes always
// have a matching, non-invented chain. next, when non-nil, is attached to every
// branch as a nested decision (used by publish_website to chain the byte route
// to the domain/websites_create choice).
func byteRouteDecision(next *toolforge.GuideDecisionBuilder) *toolforge.GuideDecisionBuilder {
	return toolforge.Decision("Where are the bytes?",
		toolforge.Branch("a file on the host — a user attachment, OR a file the host runtime itself created (assistant-generated sandbox file)").
			WhenFeature(hostenv.FeatFileHostInput).
			Steps("upload_file").
			Detail(toolforge.Static("Pass the host file reference via the file argument; the host runtime fetches and uploads it. Do not base64-encode, mint a presigned URL, or build a download_url/file_id yourself.")).
			Next(next),
		toolforge.Branch("a local file/directory path on a co-located host").
			WhenFeature(hostenv.FeatSourcePath).
			Steps("upload_file").
			Detail(toolforge.Static("Use source.mode=path with the host-side file/directory/archive path; the server reads it directly.")).
			Next(next),
		toolforge.Branch("agent-local bytes the host runtime cannot provide through `file` (not a host/user/assistant-generated file)").
			WhenFeature(hostenv.FeatSourceMint).
			Steps("upload_file").
			StepWhen(hostenv.FeatSourceMint, "<host PUT>", "upload_status").
			Detail(toolforge.Static("Mint has NOT stored bytes when upload_file returns: PUT the agent-local file to the returned url (curl -sS -T <file> \"<url>\"), then poll upload_status until completed — the completed CID is already pinned; do not call pins_add.")).
			Next(next),
		toolforge.Branch("bytes already at a public HTTPS URL (user handed a URL)").
			WhenFeature(hostenv.FeatSourceURL).
			Steps("upload_url").
			Detail(toolforge.Static("upload_url server-fetches the public HTTPS URL and pins it; do not download then re-upload.")).
			Next(next),
		toolforge.Branch("only raw inline bytes, with no file and no URL").
			WhenFeature(hostenv.FeatSourceData).
			Steps("upload_data").
			Detail(toolforge.Static("upload_data is a last resort (RFC 2397 data: URI); never base64-encode a real or host-provided file into it.")).
			Next(next),
	)
}

// vaultByteRouteDecision is the upload-chooser twin for the vault flow. Every
// branch still ends at vault_put_file — the ONLY vault write (there is no
// "vault a CID" tool) — but the decision makes the steps represent the byte
// source (host file, local path, mint, public URL, raw bytes) so a steps-first
// model does not assume vault storage must be mint, nor invent a path through
// the IPFS-only upload_url / upload_data tools.
func vaultByteRouteDecision() *toolforge.GuideDecisionBuilder {
	return toolforge.Decision("Where are the bytes for the vault?",
		toolforge.Branch("a file on the host — a user attachment, OR a file the host runtime itself created (assistant-generated sandbox file)").
			WhenFeature(hostenv.FeatFileHostInput).
			Steps("vault_put_file").
			Detail(toolforge.Static("Pass the host file reference via the file argument; the vault stores its bytes at vault_path.")),
		toolforge.Branch("a local file/directory path on a co-located host").
			WhenFeature(hostenv.FeatSourcePath).
			Steps("vault_put_file").
			Detail(toolforge.Static("Use source.mode=path with the host-side path and the destination vault_path; the server reads it directly.")),
		toolforge.Branch("agent-local bytes the host runtime cannot provide through `file` (not a host/user/assistant-generated file)").
			WhenFeature(hostenv.FeatSourceMint).
			Steps("vault_put_file").
			StepWhen(hostenv.FeatSourceMint, "<host PUT>").
			Detail(toolforge.Static("vault_put_file with source.mode=mint + vault_path mints a one-time presigned PUT url bound to vault_path; it has not stored bytes yet.").
				StaticSentence("PUT the agent-local file to the returned url.").
				StaticSentence("The vault write is non-blocking: the PUT returns after staging the bytes locally (status: staged); the file is immediately readable from this instance, and durability on Sia happens in the background or via the vault_flush tool (non-blocking, returns an accepted job { job_id, profile, path? }) — poll vault_flush_status(job_id) or vault_stat until status: durable when durability is needed before sharing. There is no upload_status to poll.")),
		toolforge.Branch("bytes already at a public HTTPS URL").
			// vault_put_file's url source exists ONLY on the OpenAI tunnel
			// transport. Gate on the transport, not FeatSourceURL: Grok declares
			// FeatSourceURL to register upload_url, but its vault_put_file is
			// mint-only — there is no "vault a URL" branch on Grok.
			WhenTransport(hostenv.TransportOpenAI).
			Steps("vault_put_file").
			Detail(toolforge.Static("vault_put_file takes the URL via its own url source on the tunnel transport; the separate upload_url tool is IPFS-only, not a vault write.")),
		toolforge.Branch("only raw inline bytes, no file and no URL").
			WhenTransport(hostenv.TransportOpenAI).
			Steps("vault_put_file").
			Detail(toolforge.Static("vault_put_file takes raw inline bytes via its own data source as a last resort; never base64-encode a real or host-provided file.")),
	)
}

// publishDomainDecision is the publish_website choice of how to deploy the
// already-uploaded site: a generic platform subdomain, an explicit label, or a
// custom domain. It is nested under the byte-route decision (byteRouteDecision)
// so a model first obtains a CID via real upload tools, then chooses the
// deployment shape. Every step here is a real tool.
func publishDomainDecision() *toolforge.GuideDecisionBuilder {
	return toolforge.Decision("Does the user have a domain or subdomain label preference?",
		toolforge.Branch("No — generic request (e.g. \"create me a website\", \"publish this\", \"host this\")").
			Steps("websites_create", "websites_validate").
			Detail(publishCidLead.Then(htmlRootClause).
				Static("Call websites_create with only {\"cid\": \"<cid>\"} — no domain, no label, no platform. The platform auto-generates a subdomain and manages DNS. Do NOT invent a label or call websites_platform_domain_availability. Do not infer a desire for custom naming from a generic request to create or publish a website.").
				Then(validateAfterCreateClause).
				Then(cdnDeployNoticeClause).
				Then(reconcileNoSleep).
				Then(siteBundleUpload())),
		toolforge.Branch("Yes — user explicitly supplied or requested a specific label (e.g. \"call it acme\", \"use myapp\")").
			Steps("websites_platform_domains_list", "websites_platform_domain_availability", "websites_create", "websites_validate").
			Detail(publishCidLead.Then(htmlRootClause).
				Static("List platform roots with websites_platform_domains_list, then check the label is claimable with websites_platform_domain_availability <label>, then call websites_create with {\"cid\": \"<cid>\", \"platform\": true, \"label\": \"<label>\"}. Only use this branch when the user explicitly named a label — never invent one to perform the availability step.").
				Then(validateAfterCreateClause).
				Then(cdnDeployNoticeClause).
				Then(reconcilePlain)),
		toolforge.Branch("Yes — user owns a custom domain (e.g. example.com)").
			Steps("websites_create", "websites_validate").
			Detail(publishCidLead.Then(htmlRootClause).
				Static("Call websites_create with {\"cid\": \"<cid>\", \"website\": \"<domain>\"}. The domain is used directly as a custom domain (not a platform subdomain). Read pinner://websites/<domain>/dns-requirements for DNS records to publish. If dns_hosting=true (managed), DNS is reconciled asynchronously — validation may report the old CID right after the update; that is reconciliation lag, not failure, so re-call websites_validate without starting a new flow. If self-managed, publish the _dnslink TXT and validation TXT before calling websites_validate.").
				Then(cdnDeployNoticeClause).
				Then(hnsNamespaceClause)),
	)
}

// buildAgentGuide constructs the AgentGuide declaratively with the platform
// DSL, then resolves it against the detected platform profile. Every flow,
// step, branch and sentence is feature-gated and per-host resolved through the
// same toolforge DSL the tool schemas use, so the guide can never advertise a
// tool or source mode the resolved surface rejects (e.g. upload_status only
// appears on mint transports).
func buildAgentGuide(profile *hostenv.PlatformProfile) AgentGuide {
	p := *profile
	// The server surface and deployment mode are construction-time properties
	// (recorded by buildCatalog); overlay them so the guide reflects the actual
	// registered surface and whether this is a hosted assembly, neither of which
	// the request profile carries as a wire signal.
	p.Surface = activeSurface()
	p.Hosted = activeHosted()
	substitute := func(s string) string {
		return strings.ReplaceAll(s, "{{SOURCES}}", sourceModesText(&p))
	}

	spec := toolforge.Guide().
		Substitute(substitute).
		Summary(guideSummary).
		Rule(guideArchiveInvariant).
		Rule(guideCIDStructure).
		RuleWhen(hostenv.FeatMCPApps,
			"MCP Apps rule: this host renders interactive app views. When a user explicitly requests a visual interface, call open_app with the app name (vault_browser, sso_signin, pin_creator, upload_manager, pin_list, account, vault_create, vault_restore, account_password, account_email). open_app returns a ui:// view the host renders as an iframe. Prefer headless primitives (vault_status, vault_put_file, pins_list, auth_sso, ...) for autonomous workflows — call open_app only when a human-facing screen is needed.").
		// Claude Web (host "claude") on a self-hosted (non-hosted) deployment
		// cannot exercise the transport-derived mint/sink endpoints, so the
		// only working upload is the base64 upload_data relay and downloads
		// cannot be delivered to the user. Scoped to the Web host AND
		// non-hosted deployment only (RuleWhenPred) — Claude Desktop (a
		// different HostType) is co-located with full local file access and
		// must NOT get this notice, and a hosted (Portal-embedded) deployment
		// lets Claude Web use the mint/drop endpoints like any other HTTP
		// host, so it is not treated as special.
		RuleWhenPred(hostenv.And(hostenv.HostIs(hostenv.HostClaude), hostenv.Not(hostenv.HostedIs(true))),
			"Host capability notice (Claude Web): this agent has no network egress (no curl) and no file references, so the ONLY working upload is upload_data (RFC 2397 base64 data: URI passed in the tool args). upload_file's source.mode=mint and the sink=drop download link both require the agent to curl or fetch out of band, which this host cannot do, and sink=local writes to the MCP server's own unreachable disk — so warn the user before offering a download that the content cannot be delivered to them.").
		// Hosted (Portal-embedded) deployments establish the caller's identity
		// via Portal OAuth before the request reaches the MCP server. State that
		// explicitly so the agent does not attempt a config-mutating
		// auth_login/auth_logout, which are CLI/local-only surfaces absent here.
		RuleWhenHosted(true,
			"Hosted instance notice: a Portal OAuth identity is already established for the current request and authenticated operations run as that user. Do NOT call auth_login or auth_logout (they are unavailable on this hosted surface); identity cannot be switched mid-session.").
		Rule("Access policy (quota trumps a subscription): before a paid/metered action, check the user's access via account_quota (discover it with search_tools query \"quota\"). Its has_quota flag is authoritative — if true, granted quota covers the user and they need NO subscription, so proceed without asking about one. Only when has_quota is false, check account_subscription (search_tools query \"subscription\"): if subscribed, proceed; if not subscribed, surface the returned web_url deep-link so the human opens the web app to subscribe — you can neither subscribe on their behalf nor treat a subscription as a substitute when quota is available.").
		Flow(toolforge.Flow("auth", "Authenticate").
			Steps("auth_status", "auth_sso", "auth_resume", "auth_status").
			Detail(toolforge.Static("Run auth_status; if unauthenticated, call auth_sso and poll auth_resume with the returned handle until the human completes the browser sign-in.").
				When(hostenv.FeatMCPApps,
					"On this host you can also call open_app with app=\"sso_signin\" to render an interactive sign-in card for the human."))).
		Flow(toolforge.Flow("vault_create", "Create a vault").
			Steps("vault_create", "vault_create_resume", "vault_status").
			Detail(toolforge.Static("Call vault_create with a profile name; poll vault_create_resume with the returned handle; confirm with vault_status until unlocked.").
				When(hostenv.FeatMCPApps,
					"On this host you can also call open_app with app=\"vault_create\" to render the interactive vault creation wizard."))).
		Flow(toolforge.Flow("vault_restore", "Restore a vault").
			Steps("vault_restore", "vault_restore_resume", "vault_status").
			Detail(toolforge.Static("Call vault_restore; poll vault_restore_resume with the returned handle; confirm with vault_status until unlocked.").
				When(hostenv.FeatMCPApps,
					"On this host you can also call open_app with app=\"vault_restore\" to render the interactive restore wizard."))).
		Flow(toolforge.Flow("upload", "Upload new content (creates + pins)").
			Steps("capabilities").
			// The byte route is a decision, not a fixed upload_file: a model
			// that reads steps first still sees upload_url / upload_data as the
			// route for a public URL / raw inline bytes. The mint tail (<host
			// PUT> + upload_status) lives inside the mint branch; <host PUT> is
			// an out-of-band action, not an MCP tool, but naming it keeps the
			// chain from looking complete at the mint response.
			Decision(byteRouteDecision(nil)).
			Detail(uploadDetailDesc)).
		Flow(toolforge.Flow("vault_upload", "Store a file in a vault").
			Steps("capabilities").
			// Vault storage is always vault_put_file (no vault-from-CID tool);
			// the decision surfaces the byte source so steps-first models do not
			// route vault bytes through the IPFS-only upload_url / upload_data.
			Decision(vaultByteRouteDecision()).
			Detail(vaultUploadDetailDesc)).
		Flow(toolforge.Flow("download", "Download IPFS content to a file").
			Steps("capabilities", "download_file").
			Detail(downloadDetailDesc)).
		Flow(toolforge.Flow("vault_download", "Download a file from a vault").
			Steps("capabilities", "vault_get_file").
			Detail(vaultDownloadDetailDesc)).
		Flow(toolforge.Flow("vault_share", "Share from a vault").
			Steps("vault_status", "vault_share", "vault_verify").
			Detail(toolforge.Static("Ensure the vault is unlocked (vault_status), then call vault_share with the vault_path to generate a shareable link (control its lifetime with expiry). Local reads (vault_get_file / vault cat / vault_stats) work any time after a staged PUT; only share/send require durability across profiles. Only durable (status: durable) files can be shared: if vault_share or vault_send returns {code:'not_durable', ...}, run vault_flush (non-blocking, returns an accepted job { job_id, profile, path? }), poll vault_flush_status(job_id) or vault_stat until status: durable, then share/send again. If a file stays non-durable across polls, read vault_stat's flush_started_at, flush_attempts and flush_error: a flushing file shows a flush_started_at and a rising flush_attempts with no error, a failed file shows flush_attempts plus a non-empty flush_error, and a staged file that never started shows zero attempts/no error and an empty flush_started_at — compare now against flush_started_at to tell a long host upload from a hung pin. The recipient accepts the share with vault_share_accept (accept_state 'pinned' — an independent pin of the same object key, NOT a digest failure), which is directly visible on tools/list; vault_verify on a freshly pinned object reports digest_verified 'not_applicable' until first get/decrypt/deep verify — treat accept_state 'pinned' (not a digest signal) as the success indicator. For multi-profile swarms, list profiles with vault_profiles and hand off a file with vault_send (or pass profile=<name> when more than one profile is unlocked — vault ops return profile_required otherwise)."))).
		Flow(toolforge.Flow("vault_sync", "Sync and verify vault state").
			Steps("vault_status", "vault_sync", "vault_verify").
			Detail(toolforge.Static("vault_sync reconciles the local vault cache from the indexer; vault_verify checks file integrity. Run both after creating or restoring on a new device, or when share state may have changed. Related utilities are discoverable via search_tools(category=vault): vault_ls, vault_stat, vault_tag_add, vault_tag_rm, vault_version_restore."))).
		Flow(toolforge.Flow("pins", "Manage pins").
			Steps("pins_add", "pins_list", "pins_status", "pins_rm").
			Detail(toolforge.Static("pins_add imports content already on IPFS by external CID; it is NOT for use after an upload tool (which already pins). pins_status takes one cid; pins_rm requires confirm and exactly one of cids or all."))).
		Flow(toolforge.Flow("publish_website", "Publish a website").
			// The byte route comes first (real upload tools produce the CID),
			// then the domain/websites_create choice is nested under each branch.
			// Every step here is a real tool, so the guide's "steps resolve to
			// real tools" invariant holds on every host.
			Decision(byteRouteDecision(publishDomainDecision()))).
		Flow(toolforge.Flow("ens_publish", "Point an ENS/onchain domain at IPFS content").
			// ENS domains do not use the website system — they resolve via an
			// IPNS-based contenthash set onchain in the ENS resolver. The byte
			// route reuses the existing upload chooser to produce a CID, then
			// ens_point publishes it under the domain's IPNS key and returns
			// the contenthash + wallet guidance. ens_point/ens_unpoint are
			// behind progressive disclosure (never curated), so name them here
			// and steer the agent to search for them rather than expecting
			// them on tools/list. The final contenthash is set by the USER's
			// wallet/ENS manager — the agent surfaces the value and options,
			// never assumes a specific wallet.
			Decision(byteRouteDecision(
				toolforge.Decision("Point the ENS name at the CID?",
					toolforge.Branch("Yes — point the ENS/onchain domain at the content").
						Steps("ens_point").
						Detail(toolforge.Static("Search for ens_point (search_tools query \"ens\"), then call it with the onchain domain (e.g. vitalik.eth) and the cid from the upload. It creates or reuses the domain's IPNS key, publishes the CID, and returns the contenthash (ipns://<ipns-name>) plus a verify URL (eth.limo for .eth). The returned next_steps are onchain: the user sets the ENS resolver's contenthash field to the returned value from their own wallet or the ENS manager (app.ens.domains), the ENS SDK (ethers.js), or a wallet with ENS support. Do NOT assume a specific wallet. After the onchain transaction confirms, verify at the returned verify URL.")),
					toolforge.Branch("No — only publish the content to IPFS/IPNS, no onchain pointing").
						Steps("websites_create").
						Detail(toolforge.Static("Treat it as a normal website publish: websites_create with the cid. ENS pointing is only applied when the user explicitly wants their ENS name to resolve to the content.")),
				),
			))).
		Flow(toolforge.Flow("update_website", "Update an existing website").
			Steps("websites_get", "websites_update", "websites_validate").
			Detail(toolforge.Static("Update a deployed website's content without recreating it. 1) websites_get <domain> first to capture the current target_type and dns_hosting_enabled — never guess them. 2) If the new CID is external, pins_add it first; updating an unpinned CID returns CidNotPinned. 3) websites_update <domain> with the new cid (target-type is inherited when omitted; change it only when intentionally switching IPFS<->IPNS). 4) websites_validate. If DNS hosting is managed, validation may report the old CID right after the update — that is reconciliation lag, not failure; re-call websites_validate without starting a new flow.").
				Then(cdnDeployNoticeClause))).
		Resolve(p)

	// The resolved guide is filtered to the server surface: flows whose
	// underlying tools are not registered on this surface (e.g. the Sia vault
	// flows on a hosted server) are dropped so the guide never advertises an
	// unregisterable action.
	return filterGuideFlows(spec, p.Surface)
}

// flowSurface maps each agent_guide flow name to the surface flag that gates
// it. Flows not listed are gated by no flag (always kept).
var flowSurface = map[string]func(Surface) bool{
	"auth":            Surface.AccountOn,
	"vault_create":    Surface.VaultOn,
	"vault_restore":   Surface.VaultOn,
	"vault_upload":    Surface.VaultOn,
	"vault_download":  Surface.VaultOn,
	"vault_share":     Surface.VaultOn,
	"vault_sync":      Surface.VaultOn,
	"upload":          Surface.UploadOn,
	"download":        Surface.UploadOn,
	"pins":            Surface.PinsOn,
	"publish_website": Surface.WebsitesOn,
	"update_website":  Surface.WebsitesOn,
	"ens_publish":     Surface.ENSOn,
}

// filterGuideFlows drops resolved flows whose surface flag is disabled.
func filterGuideFlows(guide toolforge.AgentGuide, s Surface) toolforge.AgentGuide {
	if s.IsZero() {
		return guide
	}
	kept := guide.Flows[:0]
	for _, f := range guide.Flows {
		if gate, ok := flowSurface[f.Name]; ok {
			if !gate(s) {
				continue
			}
		}
		kept = append(kept, f)
	}
	guide.Flows = kept
	return guide
}

// NewAgentGuideDescriptor returns a static, no-input tool that orients an agent
// to the primary Pinner flows and how to chain them. It is the "start here"
// surface added in the v5 audit: deterministic structured guidance, so a model
// does not have to discover the flows by probing tool descriptions. The guide
// content is composed via the platform DSL and adapted based on the calling
// client's platform profile so file-input and download-sink guidance match the
// transport's capabilities; because it is host-aware it is re-resolved per
// request rather than at startup.
// agentGuideDescription is shared between the static Description (tools/list)
// and the Fallback MCPTarget so the tool carries a target list for uniformity
// (it is a direct-only tool and does not enter the catalog).
const agentGuideDescription = "Orientation for autonomous agents: the primary Pinner flows (auth, vault_create, vault_restore, upload, vault_upload, download, vault_download, vault_share, vault_sync, pins, publish_website, ens_publish) as ordered tool chains or decision trees, plus operational rules. On hosts that render MCP Apps, the guide includes open_app as the single launcher for human-facing interactive views. Call this first to learn how to drive Pinner before probing individual tools."

func NewAgentGuideDescriptor() model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:          "agent_guide",
		Title:         "Pinner agent guide",
		Description:   agentGuideDescription,
		Category:      model.CategoryCore,
		OpenWorldHint: false, // static local guidance payload; changes no state
		MCPTargets:    toolforge.MCPTargets(toolforge.Fallback(agentGuideDescription)),
		InputSchema:   toolargs.ToolSchemaFor[wizard.NoInput](),
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			guide := buildAgentGuide(profileFromRequest(request))
			return model.ToolResult{StructuredContent: guide, Text: toolargs.ResultJSONText(guide)}, nil
		},
	}
}
