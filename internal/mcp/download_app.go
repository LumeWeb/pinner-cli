package mcp

import (
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcpapp"
)

// This file wires the two "Download to File" MCP Apps (IPFS and vault) onto the
// shared AppView lib layer. Each pairs a sink-aware download tool (download_file
// / vault_get_file) with a ui:// view that a UI-capable host renders as a
// download panel.
//
// Unlike the upload apps, a download app has NO app-only mint/status helper and
// NO out-of-band byte push: the download tools themselves resolve the bytes and
// return either a one-time HTTP GET filedrop `fetch_url` (sink=drop, pulled by
// the browser <a download> / curl) or a host-side `output_path` (sink=local).
// The app calls the model-facing tool over callServerTool — mirroring the
// read-only vault_browser pattern — then renders the returned link or path.
// No file bytes ever cross the MCP/LLM channel.

// IPFSDownloadAppURI is the ui:// resource serving the "Download from IPFS" app.
const IPFSDownloadAppURI = "ui://downloads/ipfs.html"

// VaultDownloadAppURI is the ui:// resource serving the "Download from Vault" app.
const VaultDownloadAppURI = "ui://downloads/vault.html"

// renderIPFSDownloadAppHTML renders the complete "Download from IPFS" app
// document (ui://downloads/ipfs.html). The shared shell (doctype/<head>/inline
// theme) and the ESM module (shared ext-apps bootstrap + download logic) come
// from mcpapp.RenderMcpAppDoc; only the visible body form is authored in templ.
func renderIPFSDownloadAppHTML() string {
	return mcpapp.RenderMcpAppDoc("Download from IPFS", mcpapp.IPFSDownloadAppForm(), mcpapp.AppModule("ipfs-download"))
}

// renderVaultDownloadAppHTML renders the complete "Download from Vault" app
// document (ui://downloads/vault.html).
func renderVaultDownloadAppHTML() string {
	return mcpapp.RenderMcpAppDoc("Download from Vault", mcpapp.VaultDownloadAppForm(), mcpapp.AppModule("vault-download"))
}

// RegisterIPFSDownloadApp wires the "Download from IPFS" MCP App: attaches the
// ui:// view to the download_file tool and registers the
// ui://downloads/ipfs.html HTML resource. The view calls download_file over
// callServerTool and needs no app-only helper because the tool itself returns
// the fetch_url / output_path for the sink it resolves.
func RegisterIPFSDownloadApp(srv *sdk.Server, catalog *ToolCatalog) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	if catalog == nil {
		return fmt.Errorf("nil tool catalog")
	}
	return RegisterAppView(srv, catalog, AppView{
		URI:           IPFSDownloadAppURI,
		Name:          "ipfs-download",
		Title:         "Download from IPFS",
		Description:   "Download IPFS content (CID or CID/path) to a file.",
		HTML:          renderIPFSDownloadAppHTML(),
		PrefersBorder: true,
		AttachTo:      []string{"download_file"},
	})
}

// RegisterVaultDownloadApp wires the "Download from Vault" MCP App: attaches the
// ui:// view to the vault_get_file tool and registers the
// ui://downloads/vault.html HTML resource. Like the IPFS variant, it calls
// vault_get_file over callServerTool and needs no app-only helper.
func RegisterVaultDownloadApp(srv *sdk.Server, catalog *ToolCatalog) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	if catalog == nil {
		return fmt.Errorf("nil tool catalog")
	}
	return RegisterAppView(srv, catalog, AppView{
		URI:           VaultDownloadAppURI,
		Name:          "vault-download",
		Title:         "Download from Vault",
		Description:   "Download a file from your encrypted Pinner vault.",
		HTML:          renderVaultDownloadAppHTML(),
		PrefersBorder: true,
		AttachTo:      []string{"vault_get_file"},
	})
}
