package mcp

import (
	"context"
	"strings"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/wizard"
)

// GuideFlow describes one chained flow an agent can drive end-to-end.
// Simple flows use Steps directly. Branching flows use Decision so the agent
// picks the correct path deterministically instead of guessing.
type GuideFlow struct {
	Name     string         `json:"name"`               // flow identifier, e.g. auth
	Title    string         `json:"title"`              // short human label
	Steps    []string       `json:"steps,omitempty"`    // ordered tools (simple flows)
	Detail   string         `json:"detail,omitempty"`   // one-line guidance
	Decision *GuideDecision `json:"decision,omitempty"` // branching flows
}

// GuideDecision models a branching point in a flow. The agent evaluates each
// Branch's When clause and follows the first match.
type GuideDecision struct {
	Question string        `json:"question"` // what to decide
	Branches []GuideBranch `json:"branches"` // ordered branches
}

// GuideBranch is one path through a decision. When is a natural-language
// condition; Steps is the ordered tool chain for that path; Detail is
// guidance; Next allows nested decisions.
type GuideBranch struct {
	When   string         `json:"when"`  // condition for this branch
	Steps  []string       `json:"steps"` // ordered tools for this branch
	Detail string         `json:"detail,omitempty"`
	Next   *GuideDecision `json:"next,omitempty"` // nested decision if needed
}

// AgentGuide is the structured payload returned by the agent_guide tool.
type AgentGuide struct {
	Summary string      `json:"summary"`
	Flows   []GuideFlow `json:"flows"`
	// Rules holds operational invariants the agent must not violate.
	Rules []string `json:"rules,omitempty"`
}

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

// guideSourceModes returns the transport-scoped source modes the resolved
// profile actually supports. The guide only advertises modes the transport can
// serve, so a host is never directed to a mode its transport cannot perform
// (e.g. source.mode=path from a remote HTTP host).
func guideSourceModes(profile *hostenv.PlatformProfile) []string {
	modes := make([]string, 0, 3)
	if profile.Has(hostenv.FeatSourcePath) {
		modes = append(modes, "source.mode=path")
	}
	if profile.Has(hostenv.FeatSourceMint) {
		modes = append(modes, "source.mode=mint")
	}
	if profile.Has(hostenv.FeatSourceURL) || profile.Has(hostenv.FeatSourceData) {
		modes = append(modes, "source.mode=url/data")
	}
	return modes
}

// buildAgentGuide constructs the AgentGuide, adapting upload/download
// guidance based on the detected platform profile's feature set.
func buildAgentGuide(profile *hostenv.PlatformProfile) AgentGuide {
	hasFileInput := profile.Has(hostenv.FeatFileHostInput)
	hasSinkDrop := profile.Has(hostenv.FeatSinkDrop)

	// --- Summary -----------------------------------------------------------

	summary := "Start here. Drive Pinner through these primary flows; each step is a tool. Check the current state first, then follow the matching flow. Once a wizard session is active, stay in it: always call the returned next_step_schema via the wizard step tool — do not abandon the wizard to rediscover low-level tools. A static website ZIP (index.html, CSS, JS, images, nested pages) is always a single directory DAG: call upload_file"
	if hasFileInput {
		summary += " with a host file argument IF capabilities' file_input_policy is host_file_first (your client can hand Pinner a {download_url, file_id} object), otherwise a convert-capable transport source"
	} else {
		summary += " with a convert-capable transport source (" + strings.Join(guideSourceModes(profile), " or ") + ")"
	}
	summary += ", then publish the resulting directory CID. Do NOT mint a presigned curl URL for a file your host already holds unless capabilities directs you to a transport-scoped source. For guided, interactive website onboarding (human-in-the-loop, step-by-step DNS setup), use the website-onboarding prompt and the websites_wizard tools instead of the publish_website flow."

	// --- Upload detail -----------------------------------------------------

	uploadDetail := "Check capabilities to pick the byte source THIS client is told to use. "
	if hasFileInput {
		uploadDetail += "If capabilities' file_input_policy is host_file_first (only when your client can hand Pinner a {download_url, file_id} file object), pass a host-provided file reference directly. Otherwise use a transport-scoped source: " + strings.Join(guideSourceModes(profile), " or ") + ". "
	} else {
		uploadDetail += "Use a transport-scoped source: " + strings.Join(guideSourceModes(profile), " or ") + ". "
	}
	uploadDetail += "The returned CID is already pinned — use it directly in websites_create/update; do NOT call pins_add after an upload"
	if profile.Has(hostenv.FeatSourceMint) {
		uploadDetail += ". When using source.mode=mint, poll upload_status with the returned upload_handle for the CID"
	}
	uploadDetail += ". Static site bundle rule: a ZIP containing index.html, CSS, JS, images, or nested pages is a single directory DAG — call upload_file"
	if hasFileInput {
		uploadDetail += " with the host file argument (or a convert source) and archive_mode=convert"
	} else {
		uploadDetail += " with a convert source (" + strings.Join(guideSourceModes(profile), " or ") + ") and archive_mode=convert"
	}
	uploadDetail += ", not individual assets."

	// --- Vault upload detail ----------------------------------------------

	vaultUploadDetail := "Check capabilities to pick the byte source THIS client is told to use. "
	if hasFileInput {
		vaultUploadDetail += "If capabilities' file_input_policy is host_file_first (only when your client can hand Pinner a {download_url, file_id} file object), pass a host-provided file reference directly. Otherwise use a transport-scoped source (" + strings.Join(guideSourceModes(profile), " or ") + ") plus the destination vault_path. "
	} else {
		vaultUploadDetail += "Use a transport-scoped source (" + strings.Join(guideSourceModes(profile), " or ") + ") plus the destination vault_path. "
	}
	if profile.Has(hostenv.FeatSourceMint) {
		vaultUploadDetail += "When using source.mode=mint, poll upload_status with the returned upload_handle for the CID."
	}

	// --- Download detail ---------------------------------------------------

	downloadDetail := "Check capabilities' download_sink_modes; call download_file with ipfs_path (CID or CID/path) and a supported sink. sink=local writes the bytes to a host-side output_path on the MCP server's own disk (available on every transport)"
	if hasSinkDrop {
		downloadDetail += "; sink=drop (when advertised) returns a one-time HTTP GET filedrop link to pull from out of band with curl -o or a browser."
	} else {
		downloadDetail += "."
	}

	// --- Vault download detail --------------------------------------------

	vaultDownloadDetail := "Check capabilities' download_sink_modes and that the vault is unlocked; call vault_get_file with vault_path and a supported sink. sink=local writes the decrypted bytes to a host-side output_path on the MCP server's own disk"
	if hasSinkDrop {
		vaultDownloadDetail += "; sink=drop (when advertised) returns a one-time HTTP GET filedrop link."
	} else {
		vaultDownloadDetail += "."
	}

	// --- Publish-website branch detail suffixes ---------------------------

	// The generic and domain branches both mention "call upload_file with
	// the host file argument".  When the host cannot build file references,
	// rephrase to use conver source.
	siteUploadClause := "call upload_file with the host file argument and archive_mode=convert"
	if !hasFileInput {
		siteUploadClause = "call upload_file with a convert source (" + strings.Join(guideSourceModes(profile), " or ") + ") and archive_mode=convert"
	}

	return AgentGuide{
		Summary: summary,
		Rules: []string{
			"Website archive invariant: before publishing any generated static-site archive, verify that index.html is at the archive root. Never publish an archive where the entire site is wrapped in a single parent directory (e.g. site.zip/mysite/index.html). The correct layout is site.zip/index.html. If the first path component wraps the entire site, rebuild the archive from the directory's contents, not the directory itself.",
			"Website CID structure: a website CID must be a directory whose root contains index.html. Gateways serve /index.html at the directory path. Uploading an archive with archive_mode=convert produces a directory CID whose structure mirrors the archive — if the archive has a wrapper directory, the CID will too, and the site will not resolve at /. The tool will reject a CID that has no root index.html or is wrapped in a single parent directory.",
		},
		Flows: []GuideFlow{
			{
				Name:   "auth",
				Title:  "Authenticate",
				Steps:  []string{"auth_status", "auth_sso", "auth_resume", "auth_status"},
				Detail: "Run auth_status; if unauthenticated, call auth_sso and poll auth_resume with the returned handle until the human completes the browser sign-in.",
			},
			{
				Name:   "vault_create",
				Title:  "Create a vault",
				Steps:  []string{"vault_create", "vault_create_resume", "vault_status"},
				Detail: "Call vault_create with a profile name; poll vault_create_resume with the returned handle; confirm with vault_status until unlocked.",
			},
			{
				Name:   "vault_restore",
				Title:  "Restore a vault",
				Steps:  []string{"vault_restore", "vault_restore_resume", "vault_status"},
				Detail: "Call vault_restore; poll vault_restore_resume with the returned handle; confirm with vault_status until unlocked.",
			},
			{
				Name:   "upload",
				Title:  "Upload new content (creates + pins)",
				Steps:  []string{"capabilities", "upload_file"},
				Detail: uploadDetail,
			},
			{
				Name:   "vault_upload",
				Title:  "Store a file in a vault",
				Steps:  []string{"capabilities", "vault_put_file"},
				Detail: vaultUploadDetail,
			},
			{
				Name:   "download",
				Title:  "Download IPFS content to a file",
				Steps:  []string{"capabilities", "download_file"},
				Detail: downloadDetail,
			},
			{
				Name:   "vault_download",
				Title:  "Download a file from a vault",
				Steps:  []string{"capabilities", "vault_get_file"},
				Detail: vaultDownloadDetail,
			},
			{
				Name:   "pins",
				Title:  "Manage pins",
				Steps:  []string{"pins_add", "pins_list", "pins_status", "pins_rm"},
				Detail: "pins_add imports content already on IPFS by external CID; it is NOT for use after an upload tool (which already pins). pins_status takes one cid; pins_rm requires confirm and exactly one of cids or all.",
			},
			{
				Name:  "publish_website",
				Title: "Publish a website",
				Decision: &GuideDecision{
					Question: "Does the user have a domain or subdomain label preference?",
					Branches: []GuideBranch{
						{
							When:   "No — generic request (e.g. \"create me a website\", \"publish this\", \"host this\")",
							Steps:  []string{"upload_file", "websites_create", "websites_validate"},
							Detail: "Upload with wrap=true and do NOT set an explicit name for HTML — the tool auto-names wrapped HTML to index.html so the site resolves at its root. An explicit name like \"starter-site\" is honored as-is and the site will only be reachable at /starter-site, not /. Call websites_create with only {\"cid\": \"<cid>\"} — no domain, no label, no platform. The platform auto-generates a subdomain and manages DNS. Do NOT invent a label or call websites_platform_domain_availability. Do not infer a desire for custom naming from a generic request to create or publish a website. After creation, call websites_validate to confirm DNS propagation. If validation fails, wait ~30-60s and retry. For a static site bundle (ZIP containing index.html, CSS, JS, images, nested pages): " + siteUploadClause + " — the entire directory tree becomes a single directory DAG and the returned CID is the publishable directory CID. Do NOT mint a presigned curl URL for a ZIP the host already holds, and do NOT upload individual assets. Before uploading a site ZIP, verify that index.html is at the archive root, not wrapped in a parent directory. The tool will reject a CID whose root has no index.html or is wrapped in a single wrapper directory.",
						},
						{
							When:   "Yes — user explicitly supplied or requested a specific label (e.g. \"call it acme\", \"use myapp\")",
							Steps:  []string{"upload_file", "websites_platform_domains_list", "websites_platform_domain_availability", "websites_create", "websites_validate"},
							Detail: "Upload with wrap=true and do NOT set an explicit name for HTML — the tool auto-names wrapped HTML to index.html so the site resolves at its root. An explicit name like \"starter-site\" is honored as-is and the site will only be reachable at /starter-site, not /. List platform roots with websites_platform_domains_list, then check the label is claimable with websites_platform_domain_availability <label>, then call websites_create with {\"cid\": \"<cid>\", \"platform\": true, \"label\": \"<label>\"}. Only use this branch when the user explicitly named a label — never invent one to perform the availability step. After creation, call websites_validate to confirm DNS propagation. If validation fails, wait ~30-60s and retry.",
						},
						{
							When:   "Yes — user owns a custom domain (e.g. example.com)",
							Steps:  []string{"upload_file", "websites_create", "websites_validate"},
							Detail: "Upload with wrap=true and do NOT set an explicit name for HTML — the tool auto-names wrapped HTML to index.html so the site resolves at its root. An explicit name like \"starter-site\" is honored as-is and the site will only be reachable at /starter-site, not /. Call websites_create with {\"cid\": \"<cid>\", \"website\": \"<domain>\"}. The domain is used directly as a custom domain (not a platform subdomain). Read pinner://websites/<domain>/dns-requirements for DNS records to publish. If dns_hosting=true (managed), DNS is reconciled asynchronously — wait ~30-60s and retry websites_validate. If self-managed, publish the _dnslink TXT and validation TXT before calling websites_validate.",
						},
					},
				},
			},
		},
	}
}

// NewAgentGuideDescriptor returns a static, no-input tool that orients an agent
// to the primary Pinner flows and how to chain them. It is the "start here"
// surface added in the v5 audit: deterministic structured guidance, so a model
// does not have to discover the flows by probing tool descriptions. The guide
// content is adapted based on the calling client's platform profile so that
// file-input and download-sink guidance matches the transport's capabilities.
func NewAgentGuideDescriptor() model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        "agent_guide",
		Title:       "Pinner agent guide",
		Description: "Orientation for autonomous agents: the primary Pinner flows (auth, vault_create, vault_restore, upload, vault_upload, download, vault_download, pins, publish_website) as ordered tool chains or decision trees, plus operational rules. Call this first to learn how to drive Pinner before probing individual tools.",
		Category:    model.CategoryCore,
		InputSchema: toolargs.ToolSchemaFor[wizard.NoInput](),
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			guide := buildAgentGuide(profileFromRequest(request))
			return model.ToolResult{StructuredContent: guide, Text: toolargs.ResultJSONText(guide)}, nil
		},
	}
}
