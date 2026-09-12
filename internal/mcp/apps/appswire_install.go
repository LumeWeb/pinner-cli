package apps

// This file adapts the shared module installers
// (go.lumeweb.com/pinner/mcp/appswire) onto the CLI's process-global registry
// adapter: appswire's InstallContext needs the *AppRegistry instance and a
// view-only render func — both composition-owned facts this package holds.

import (
	mcpapps "go.lumeweb.com/mcpplane/apps"
	sdk "go.lumeweb.com/mcpplane/sdk"
	mcptransfer "go.lumeweb.com/mcpplane/transfer"

	"go.lumeweb.com/pinner/canvas"
	"go.lumeweb.com/pinner/mcp/appswire"

	"go.lumeweb.com/pinner-cli/internal/mcpapp"
)

// GlobalRegistry exposes the process-global app registry so shared module
// installers write view + tool→view state into the SAME registry this
// package's registration API wraps (never a sibling instance that would split
// the association state).
func GlobalRegistry() *mcpapps.AppRegistry { return registry }

// RenderAppView adapts the CLI's RenderAppDoc to appswire's view-only
// RenderFunc shape. The document title is the shared table's ResourceTitle —
// the same canonical title this package's per-view render functions (and the
// hosted composition root) pass through, so CLI documents stay byte-identical
// across seams. An unknown view defers to RenderAppDoc's loud panic semantics.
func RenderAppView(view canvas.View) string {
	for _, spec := range appswire.All() {
		if spec.View == view {
			return mcpapp.RenderAppDoc(view, spec.ResourceTitle)
		}
	}
	return mcpapp.RenderAppDoc(view, string(view))
}

// UploadManagerAppURI returns the shared table's ui:// resource URI for the
// Upload to IPFS view — the single URI every connectDomains / unregister /
// resource-lookup call site uses, so the wire URI cannot drift from the table
// the launcher descriptor and view installer resolve against. The empty return
// covers a table that dropped the row (a build-time divergence).
func UploadManagerAppURI() string {
	v, ok := appswire.SpecForLauncher(appswire.LauncherUploadManager)
	if !ok {
		return ""
	}
	return v.URI
}

// InstallUploadManagerApp wires the shared Upload to IPFS app seam
// (appswire.UploadManagerInstaller): the module owns the view assembly, the
// CSP connectDomains from the coordinator's origins, and the submit/status
// helper tools; the CLI supplies the server, catalog, registry, and renderer.
func InstallUploadManagerApp(srv *sdk.Server, catalog AppCatalog, hp *mcptransfer.Upload) error {
	return appswire.UploadManagerInstaller(hp)(appswire.InstallContext{
		Server:   srv,
		Catalog:  catalog,
		Registry: registry,
		Render:   RenderAppView,
	})
}
