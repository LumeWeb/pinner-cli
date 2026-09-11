package mcp

import (
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/schematext"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// guideFragments holds the prose shared by multiple guide flows/branches so
// playwright copy lives in one place instead of being inlined per branch.
// Each fragment is a complete, self-punctuated toolforge.DescBuilder so it
// participates in the same feature-gated, per-profile resolution as the
// schemas and descriptions. Splice fragments together with DescBuilder.Then.

// publishCidLead is the host-agnostic lead for every publish_website branch:
// the CID may come from ANY upload tool the chooser picks (upload_file mint +
// PUT, upload_url for a public URL, upload_data for raw bytes), not only
// upload_file. publish_website consumes whatever CID was produced.
var publishCidLead = toolforge.Static("First obtain a CID via the upload flow's byte-route chooser, then websites_create consumes it directly regardless of which upload tool produced the CID.")

// htmlRootClause is the publish_website guidance about wrap/auto-naming.
// Wrapped HTML uploads are auto-named to index.html so a site resolves at its
// root; an explicit name moves it under /name. Identical across every publish
// branch.
var htmlRootClause = toolforge.Static("Upload with wrap=true and do NOT set an explicit name for HTML — the tool auto-names wrapped HTML to index.html so the site resolves at its root. An explicit name like \"starter-site\" is honored as-is and the site will only be reachable at /starter-site, not /.")

// validateAfterCreateClause is the post-create validation guidance shared by
// the platform-subdomain publish branches.
var validateAfterCreateClause = toolforge.Static("After creation, call websites_validate to confirm DNS propagation.")

// cdnDeployNoticeClause tells the agent to inform the user that a newly
// published or updated site can take up to 5 minutes to fully deploy to the
// CDN, so it may not be reachable at its URL immediately even though
// create/validation succeeded. CDN deployment is independent of validation
// (platform-domain validation is instant).
var cdnDeployNoticeClause = toolforge.Static("Let the user know that after publishing, the site can take up to 5 minutes to fully deploy to the CDN, so it may not be reachable at its URL immediately even though create/validation succeeded.")

// reconcileNoSleep is the reconciliation-lag guidance for hosts with no sleep
// tool: pending validation is lag, not failure, so re-check later.
var reconcileNoSleep = toolforge.Static("On this host there is no sleep tool: if validation is still pending or failing, treat that as reconciliation lag rather than failure and re-call websites_validate after other work, without starting a new flow.")

// reconcilePlain is the reconciliation-lag guidance used when no sleep-tool
// caveat is needed. Shares the same decision (re-check rather than fail).
var reconcilePlain = toolforge.Static("If validation is still pending or failing, treat it as reconciliation lag and re-call websites_validate after other work, without starting a new flow.")

// hnsNamespaceClause is the Handshake (alt-root) namespace guidance appended to
// custom-domain website flows whose domain may be an HNS name. It covers the
// two HNS hosting shapes: native HNS (portal builds the delegation bundle;
// the user publishes the parent NS/DS/GLUE records on-chain in the HNS wallet)
// and on-chain managed (the name's NS record points at an external
// contract, so the backend coerces the binding to status onchain_managed with
// no delegation records at all, and ownership is proven via a TXT token).
var hnsNamespaceClause = toolforge.Static("For a Handshake (alt-root) name such as acme/ (i.e. the user has a Handshake domain, not an ICANN TLD domain), pass {\"namespace\": \"hns\"} alongside the website so the site binds under the HNS namespace.").Then(hnsOnchainClause)

// hnsOnchainClause tells the agent how to read and act on the two possible
// HNS bind outcomes in the same domain: status onchain_managed (no
// records to publish) versus the normal delegation flow.
var hnsOnchainClause = toolforge.Static("Read the created/bound domain's status next:").Sentences(
	"On status onchain_managed the HNS name's DNS is served by an external contract on the Handshake chain, so there are NO delegation records to publish and pinner://websites/<domain>/dns-requirements returns no parent/authoritative records — the site works and ownership is verified via a TXT token through the HNS resolver. Do not call websites_domains_convert_onchain (it is already on-chain managed) and do not wait for delegation.",
	"On any other status (records_generated, waiting_delegation), read pinner://websites/<domain>/dns-requirements for the HNS delegation bundle and publish the parent NS/DS/GLUE records on-chain in the HNS wallet; with managed DNS the authoritative side is handled for you. To migrate a portal-managed HNS name whose DNS now lives in an external contract, call websites_domains_convert_onchain — it is one-way and destructive (deletes Pinner's managed zone/DNSSEC), so it requires confirm=true.",
)

// The ONE shared archive-mode input clause for static-site ZIP upload
// guidance. The feature gate is the contract: a host that can hand Pinner a
// file object (FeatFileHostInput) uploads via the host file argument; every
// other host must convert a transport-scoped source. BOTH the guide-fragment
// SiteZIP lead (siteBundleUpload below) and the agent-guide upload-flow
// detail (agent_guide.go, tool-gated with WhenPred) compose these exact
// strings, so the wording can never diverge again ("with the host file
// argument (or a convert source)" in the guide detail was exactly that
// drift — the parenthetical collapsed the feature gate the fragment keeps
// exclusive).
const (
	siteZIPHostFileInput      = "with the host file argument and archive_mode=convert"
	siteZIPConvertSourceInput = "with a convert source ({{SOURCES}}) and archive_mode=convert"
)

// The shared file_input_policy=host_file_first routing prose: the policy lead
// (when can the agent pass a host file reference?) plus the ONE base
// routing sentence for the transport-scoped fallback. Every flow that routes
// bytes composes these fragments and appends only its operation-specific
// suffix (e.g. "plus the destination vault_path") after them — never a
// per-flow hand copy of the policy text (the upload flow's colon form vs the
// vault flow's paren form was exactly that drift). {{SOURCES}} is
// substituted with the resolved surface's source modes at render time.
const (
	// hostFilePolicyLead is the policy gate: file_input_policy=host_file_first
	// means a host-held file goes through the file reference directly.
	hostFilePolicyLead = "If capabilities' file_input_policy is host_file_first (only when your client can hand Pinner a {download_url, file_id} file object), pass a host-provided file reference directly."

	// transportSourceRouting is the base fallback clause, lowercase opener and
	// no punctuation: callers prefix the connective ("Otherwise " / "otherwise
	// " as a mid-sentence tail) and append operation-specific suffixes before
	// the period.
	transportSourceRouting = "use a transport-scoped source ({{SOURCES}})"
)

// siteBundleUpload composes the "how to upload a static site bundle" guidance
// block. The upload call self-gates on FeatFileHostInput (a host that can hand
// over a file object uploads via the `file` argument, others must convert a
// transport source), and the trailing instructions are discrete sentences —
// each independently gateable — so the wrapper-index.html rule and the
// no-mint-for-a-host-held-ZIP rule can never drift from the tool schema.
func siteBundleUpload() toolforge.DescBuilder {
	// The bundle lead composes the shared schematext SiteZIPBundle
	// enumeration — the same asset list the upload detail, the guide summary,
	// and the toolforge site-ZIP clauses compose — never a hand-copied list,
	// and the input clauses are the shared siteZIP*Input consts above.
	return toolforge.Static("For a static site bundle ("+schematext.SiteZIPBundle+"): call upload_file").
		Unless(hostenv.FeatFileHostInput, siteZIPConvertSourceInput).
		When(hostenv.FeatFileHostInput, siteZIPHostFileInput).
		Sentences(
			"The entire directory tree becomes a single directory DAG and the returned CID is the publishable directory CID.",
			"Do NOT upload individual assets.",
			// The layout-check and rejection sentences compose the canonical
			// schematext archive-root fragments — the same prose the toolforge
			// upload_file description composes, so the root contract cannot
			// drift between the guide and the tool schema.
			schematext.ArchiveRootCheck,
			schematext.RootRejectClause,
		).
		SentencesWhenAny([]hostenv.Feature{hostenv.FeatFileHostInput, hostenv.FeatSourcePath},
			// The ZIP-context host-held rule composes the canonical
			// schematext.HostHeldNoMint behavior fragment — the same prose the
			// toolforge upload_file / vault_put_file host-file clauses compose.
			schematext.HostHeldNoMint,
		)
}
