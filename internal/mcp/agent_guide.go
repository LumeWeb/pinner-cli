package mcp

import (
	"context"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/wizard"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
)

// GuideFlow describes one chained flow an agent can drive end-to-end.
// Simple flows use Steps directly. Branching flows use Decision so the agent
// picks the correct path deterministically instead of guessing.
type GuideFlow struct {
	Name     string          `json:"name"`               // flow identifier, e.g. auth
	Title    string          `json:"title"`              // short human label
	Steps    []string        `json:"steps,omitempty"`    // ordered tools (simple flows)
	Detail   string          `json:"detail,omitempty"`   // one-line guidance
	Decision *GuideDecision `json:"decision,omitempty"` // branching flows
}

// GuideDecision models a branching point in a flow. The agent evaluates each
// Branch's When clause and follows the first match.
type GuideDecision struct {
	Question string        `json:"question"`           // what to decide
	Branches []GuideBranch `json:"branches"`          // ordered branches
}

// GuideBranch is one path through a decision. When is a natural-language
// condition; Steps is the ordered tool chain for that path; Detail is
// guidance; Next allows nested decisions.
type GuideBranch struct {
	When   string          `json:"when"`            // condition for this branch
	Steps  []string        `json:"steps"`           // ordered tools for this branch
	Detail string          `json:"detail,omitempty"`
	Next   *GuideDecision  `json:"next,omitempty"`  // nested decision if needed
}

// AgentGuide is the structured payload returned by the agent_guide tool.
type AgentGuide struct {
	Summary string      `json:"summary"`
	Flows   []GuideFlow `json:"flows"`
}

// NewAgentGuideDescriptor returns a static, no-input tool that orients an agent
// to the primary Pinner flows and how to chain them. It is the "start here"
// surface added in the v5 audit: deterministic structured guidance, so a model
// does not have to discover the flows by probing tool descriptions.
func NewAgentGuideDescriptor() model.ToolDescriptor {
	guide := AgentGuide{
		Summary: "Start here. Drive Pinner through these primary flows; each step is a tool. Check the current state first, then follow the matching flow.",
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
				Steps:  []string{"capabilities", "upload_file", "upload_status"},
				Detail: "Check capabilities; call upload_file with a transport-scoped source (host path in co-located stdio, a minted presigned HTTP PUT in remote mode, or url/data on the OpenAI tunnel), then poll upload_status for the CID. The returned CID is already pinned — use it directly in websites_create/update; do NOT call pins_add after an upload.",
			},
			{
				Name:   "vault_upload",
				Title:  "Store a file in a vault",
				Steps:  []string{"capabilities", "vault_put_file", "upload_status"},
				Detail: "Check capabilities; if vault_put_file is available and the target vault is unlocked, call it with a transport-scoped source (host path in co-located stdio mode, a minted presigned PUT in remote mode, or url/data on the OpenAI tunnel) plus the destination vault_path, then monitor with upload_status for the CID.",
			},
			{
				Name:   "download",
				Title:  "Download IPFS content to a file",
				Steps:  []string{"capabilities", "download_file"},
				Detail: "Check capabilities' download_sink_modes; call download_file with ipfs_path (CID or CID/path) and a supported sink. sink=local writes the bytes to a host-side output_path on the MCP server's own disk (available on every transport); sink=drop (when advertised) returns a one-time HTTP GET filedrop link to pull from out of band with curl -o or a browser.",
			},
			{
				Name:   "vault_download",
				Title:  "Download a file from a vault",
				Steps:  []string{"capabilities", "vault_get_file"},
				Detail: "Check capabilities' download_sink_modes and that the vault is unlocked; call vault_get_file with vault_path and a supported sink. sink=local writes the decrypted bytes to a host-side output_path on the MCP server's own disk; sink=drop (when advertised) returns a one-time HTTP GET filedrop link.",
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
							When:  "No — generic request (e.g. \"create me a website\", \"publish this\", \"host this\")",
							Steps: []string{"upload_file", "websites_create"},
							Detail: "Call websites_create with only --cid (no domain, no label). The platform auto-generates a subdomain (generate=true) and manages DNS. Do NOT invent a label or call websites_platform_domain_availability. Do not infer a desire for custom naming from a generic request to create or publish a website.",
						},
						{
							When:  "Yes — user explicitly supplied or requested a specific label (e.g. \"call it acme\", \"use myapp\")",
							Steps: []string{"upload_file", "websites_platform_domains_list", "websites_platform_domain_availability", "websites_create"},
							Detail: "List platform roots with websites_platform_domains_list, then check the label is claimable with websites_platform_domain_availability <label>, then call websites_create with --platform --label <label>. Only use this branch when the user explicitly named a label — never invent one to perform the availability step.",
						},
						{
							When:  "Yes — user owns a custom domain (e.g. example.com)",
							Steps: []string{"upload_file", "websites_create"},
							Detail: "Call websites_create with <domain> --cid <cid>. The domain is used directly as a custom domain (not a platform subdomain).",
						},
					},
				},
			},
		},
	}
	return model.ToolDescriptor{
		Name:        "agent_guide",
		Title:       "Pinner agent guide",
		Description: "Orientation for autonomous agents: the primary Pinner flows (auth, vault_create, vault_restore, upload, vault_upload, download, vault_download, pins, publish_website) as ordered tool chains or decision trees. Call this first to learn how to drive Pinner before probing individual tools.",
		Category:    model.CategoryCore,
		InputSchema: toolargs.ToolSchemaFor[wizard.NoInput](),
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			return model.ToolResult{StructuredContent: guide, Text: "Pinner agent guide."}, nil
		},
	}
}
