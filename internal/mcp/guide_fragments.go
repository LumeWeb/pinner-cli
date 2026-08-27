package mcp

import (
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// guideFragments holds the prose shared by multiple guide flows/branches so
// playwright copy lives in one place instead of being inlined per branch.
// Each fragment is a complete, self-punctuated toolforge.DescBuilder so it
// participates in the same feature-gated, per-profile resolution as the
// schemas and descriptions. Splice fragments together with DescBuilder.Then.

// htmlRootClause is the publish_website guidance about wrap/auto-naming.
// Wrapped HTML uploads are auto-named to index.html so a site resolves at its
// root; an explicit name moves it under /name. Identical across every publish
// branch.
var htmlRootClause = toolforge.Static("Upload with wrap=true and do NOT set an explicit name for HTML — the tool auto-names wrapped HTML to index.html so the site resolves at its root. An explicit name like \"starter-site\" is honored as-is and the site will only be reachable at /starter-site, not /.")

// validateAfterCreateClause is the post-create validation guidance shared by
// the platform-subdomain publish branches.
var validateAfterCreateClause = toolforge.Static("After creation, call websites_validate to confirm DNS propagation.")

// reconcileNoSleep is the reconciliation-lag guidance for hosts with no sleep
// tool: pending validation is lag, not failure, so re-check later.
var reconcileNoSleep = toolforge.Static("On this host there is no sleep tool: if validation is still pending or failing, treat that as reconciliation lag rather than failure and re-call websites_validate after other work, without starting a new flow.")

// reconcilePlain is the reconciliation-lag guidance used when no sleep-tool
// caveat is needed. Shares the same decision (re-check rather than fail).
var reconcilePlain = toolforge.Static("If validation is still pending or failing, treat it as reconciliation lag and re-call websites_validate after other work, without starting a new flow.")

// siteBundleUpload composes the full "how to upload a static site bundle"
// sentence: a ZIP of index.html/CSS/JS/images is a single directory DAG. It
// self-gates on FeatFileHostInput — a host that can hand over a file object
// uploads via the `file` argument, others must convert a transport source —
// and always ends with the wrapper-index.html rejection rule so the flow
// guidance can never drift from the tool schema.
func siteBundleUpload() toolforge.DescBuilder {
	return toolforge.Static("For a static site bundle (ZIP containing index.html, CSS, JS, images, nested pages): call upload_file").
		Unless(hostenv.FeatFileHostInput, "with a convert source ({{SOURCES}}) and archive_mode=convert").
		When(hostenv.FeatFileHostInput, "with the host file argument and archive_mode=convert").
		Static("— the entire directory tree becomes a single directory DAG and the returned CID is the publishable directory CID. Do NOT mint a presigned curl URL for a ZIP the host already holds, and do NOT upload individual assets. Before uploading a site ZIP, verify that index.html is at the archive root, not wrapped in a parent directory. The tool will reject a CID whose root has no index.html or is wrapped in a single wrapper directory.")
}
