package mcp

import (
	"context"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/wizard"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
)

// GuideFlow describes one chained flow an agent can drive end-to-end.
type GuideFlow struct {
	Name   string   `json:"name"`   // flow identifier, e.g. auth
	Title  string   `json:"title"`  // short human label
	Steps  []string `json:"steps"`  // ordered tools / milestones in the flow
	Detail string   `json:"detail"` // one-line guidance
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
				Title:  "Upload a file to IPFS",
				Steps:  []string{"capabilities", "upload_file", "upload_status"},
				Detail: "Check capabilities; if upload_file is available, call it with a transport-scoped source (host path in co-located stdio mode, a minted one-time presigned HTTP PUT endpoint in remote mode, or url/data on the OpenAI tunnel), then monitor with upload_status for the CID.",
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
				Detail: "pins_add takes required cids; pins_status takes one cid; pins_rm requires confirm and exactly one of cids or all.",
			},
		},
	}
	return model.ToolDescriptor{
		Name:        "agent_guide",
		Title:       "Pinner agent guide",
		Description: "Orientation for autonomous agents: the primary Pinner flows (auth, vault_create, vault_restore, upload, vault_upload, download, vault_download, pins) as ordered tool chains. Call this first to learn how to drive Pinner before probing individual tools.",
		Category:    model.CategoryCore,
		InputSchema: toolargs.ToolSchemaFor[wizard.NoInput](),
		Handler: func(ctx context.Context, request model.ToolRequest) (model.ToolResult, error) {
			return model.ToolResult{StructuredContent: guide, Text: "Pinner agent guide."}, nil
		},
	}
}
