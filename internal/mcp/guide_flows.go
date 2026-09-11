package mcp

// ONE declarative flow-spec source for EVERY agent_guide flow.
// guideFlowSpecs is the single production source of:
//
//   - the guide's flow inventory: buildAgentGuideFor derives each resolved
//     GuideFlow (name, title, ordered steps, decision, detail prose) from this
//     table — a flow registered without a definition here can never reach the
//     wire, because the restricted-surface filter drops every flow it cannot
//     verify against this table;
//   - the onboarding "start here" membership: the flows flagged onboarding
//     supply isPrimaryTool, Onboarding() and ListingPolicy.IsOnboarded
//     through the shared onboardingPredicate (the four primary flows; the
//     auth flow's deliberate repeated auth_status step is de-duplicated by the
//     derivation);
//   - the initialize instructions' primary flow chains
//     (instructionsFlowLines derives its lines from the onboarding entries);
//   - the static agentGuideDescription flow enumeration (derived from the
//     table's declaration order — there is no second named-flow literal).
//
// A flow-spec entry declares the flow's fixed step chain (steps), the steps
// whose tools must exist for the flow to be drivable AT ALL (required — a
// missing prerequisite drops the whole flow before any branch is considered),
// and an optional decorate callback that attaches the flow's Decision and
// Detail prose. Detail prose that names a tool gates that mention through the
// per-server availability (guideAvailability.toolPred): a hosted/minimal
// server must never see auth_sso, auth_resume, a vault tool, or an upload
// tool it lacks — in steps, in detail text, or in a nested decision branch.

import (
	"strings"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// guideFlowSpec is one agent_guide flow: its guide name, display title, the
// surface flag gating the whole flow, its ordered fixed tool steps, the tool
// steps that MUST exist on the completed surface for the flow to survive, and
// an optional decorate callback attaching Decision/Detail prose. onboarding
// marks the four "start here" primary flows that also drive the onboarding
// membership.
type guideFlowSpec struct {
	name  string
	title string
	// onboarding marks a primary flow: its steps join the "start here"
	// onboarding membership (isPrimaryTool / Onboarding() / policy override
	// semantics) and the initialize instructions' primary chain lines.
	onboarding bool
	gate       func(DomainScope) bool
	// steps is the flow's fixed (non-decision) ordered step chain.
	steps []string
	// required lists steps that must be AVAILABLE on the completed surface
	// for the flow to survive filtering at all: a flow whose essential tool
	// never registered is dropped whole, so its prose can never recommend it.
	required []string
	// decorate, when non-nil, attaches the flow's Decision and Detail prose to
	// the flow builder (which already carries name/title/steps). It receives
	// the per-server availability so tool-naming prose can gate itself.
	decorate func(b *toolforge.GuideFlowBuilder, avail *guideAvailability) *toolforge.GuideFlowBuilder
}

// flowBuilder renders the spec as a flow builder for the given availability.
func (s guideFlowSpec) flowBuilder(avail *guideAvailability) *toolforge.GuideFlowBuilder {
	b := toolforge.Flow(s.name, s.title).Steps(s.steps...)
	if s.decorate != nil {
		b = s.decorate(b, avail)
	}
	return b
}

// guideFlowSpecs is the single flow-spec table, in guide (and static
// description) declaration order. Adding a flow here extends the guide, the
// onboarding membership when flagged, and the derived description — nothing
// else needs to change.
var guideFlowSpecs = []guideFlowSpec{
	{name: "auth", title: "Authenticate", onboarding: true,
		gate:     func(s DomainScope) bool { return s.AccountOn() },
		steps:    []string{"auth_status", "auth_sso", "auth_resume", "auth_status"},
		decorate: authFlowDetail},
	{name: "vault_create", title: "Create a vault", onboarding: true,
		gate:     func(s DomainScope) bool { return s.VaultOn() },
		steps:    []string{"vault_create", "vault_create_resume", "vault_status"},
		decorate: vaultCreateFlowDetail},
	{name: "vault_restore", title: "Restore a vault", onboarding: true,
		gate:     func(s DomainScope) bool { return s.VaultOn() },
		steps:    []string{"vault_restore", "vault_restore_resume", "vault_status"},
		decorate: vaultRestoreFlowDetail},
	{name: "upload", title: "Upload new content (creates + pins)",
		gate:  func(s DomainScope) bool { return s.UploadOn() },
		steps: []string{"capabilities"}, required: []string{"upload_file"},
		decorate: uploadFlowDetail},
	{name: "vault_upload", title: "Store a file in a vault",
		gate:  func(s DomainScope) bool { return s.VaultOn() },
		steps: nil, required: []string{"vault_put_file"},
		decorate: vaultUploadFlowDetail},
	{name: "download", title: "Download IPFS content to a file",
		gate:  func(s DomainScope) bool { return s.UploadOn() },
		steps: []string{"capabilities", "download_file"}, required: []string{"download_file"},
		decorate: downloadFlowDetail},
	{name: "vault_download", title: "Download a file from a vault",
		gate:  func(s DomainScope) bool { return s.VaultOn() },
		steps: []string{"capabilities", "vault_get_file"}, required: []string{"vault_get_file"},
		decorate: vaultDownloadFlowDetail},
	{name: "vault_share", title: "Share from a vault",
		gate:  func(s DomainScope) bool { return s.VaultOn() },
		steps: []string{"vault_status", "vault_share", "vault_verify"}, required: []string{"vault_share"},
		decorate: vaultShareFlowDetail},
	{name: "vault_sync", title: "Sync and verify vault state",
		gate:  func(s DomainScope) bool { return s.VaultOn() },
		steps: []string{"vault_status", "vault_sync", "vault_verify"}, required: []string{"vault_sync"},
		decorate: vaultSyncFlowDetail},
	{name: "pins", title: "Manage pins", onboarding: true,
		gate:     func(s DomainScope) bool { return s.PinsOn() },
		steps:    []string{"pins_add", "pins_list", "pins_status", "pins_rm"},
		decorate: pinsFlowDetail},
	{name: "publish_website", title: "Publish a website",
		gate:  func(s DomainScope) bool { return s.WebsitesOn() },
		steps: nil, required: []string{"websites_create"},
		decorate: publishWebsiteFlowDetail},
	{name: "ens_publish", title: "Point an ENS/onchain domain at IPFS content",
		gate:     func(s DomainScope) bool { return s.ENSOn() },
		steps:    nil,
		decorate: ensPublishFlowDetail},
	{name: "update_website", title: "Update an existing website",
		gate:  func(s DomainScope) bool { return s.WebsitesOn() },
		steps: []string{"websites_get", "websites_update", "websites_validate"}, required: []string{"websites_update"},
		decorate: updateWebsiteFlowDetail},
}

// flowsFromSpecs appends EVERY flow-spec entry's resolved flow builder to the
// guide spec, in table (declaration) order. This is the guide's single flow
// inventory: both the onboarding-primary and the residual flows render from
// the same table, so a flow cannot appear on the guide wire without a spec
// entry (and the restricted-surface filter drops every flow the table cannot
// verify).
func flowsFromSpecs(g *toolforge.GuideSpec, avail *guideAvailability) *toolforge.GuideSpec {
	for _, s := range guideFlowSpecs {
		g.Flow(s.flowBuilder(avail))
	}
	return g
}

// guideFlowSpecFor returns the spec of a flow by name, or false.
func guideFlowSpecFor(name string) (guideFlowSpec, bool) {
	for _, s := range guideFlowSpecs {
		if s.name == name {
			return s, true
		}
	}
	return guideFlowSpec{}, false
}

// guideFlowGate looks up the surface gate of a flow from its flow-spec
// definition. ok=false means the flow declares NO definition metadata and must
// be treated as unverifiable (dropped by the restricted-surface filter).
func guideFlowGate(name string, s DomainScope) (on bool, ok bool) {
	spec, defined := guideFlowSpecFor(name)
	if !defined {
		return false, false
	}
	if s.IsZero() {
		return true, true
	}
	return spec.gate(s), true
}

// ---------------------------------------------------------------------------
// Onboarding membership (the four primary flows + agent_guide itself)
// ---------------------------------------------------------------------------

// onboardingPredicate is the ONE shared onboarding-membership evaluation
// helper: an explicit override (the policy's Onboarding set, threaded through
// withPolicy → buildCatalog → catalog.OnboardingOverride) replaces the builtin
// primary-tool predicate; an empty override defers to isPrimaryTool. Both
// ListingPolicy.IsOnboarded and ToolCatalog.Onboarding consult this
// single helper, so there is exactly one override semantics — a second copy in
// either call site could silently disagree with the other.
func onboardingPredicate(override []string) func(string) bool {
	if len(override) == 0 {
		return isPrimaryTool
	}
	membership := make(map[string]struct{}, len(override))
	for _, n := range override {
		membership[n] = struct{}{}
	}
	return func(name string) bool {
		_, ok := membership[name]
		return ok
	}
}

// isPrimaryTool reports whether a tool belongs to the onboarding "start here"
// set surfaced on an empty/help search: exactly the tool steps in the four
// onboarding-primary agent_guide flows plus the agent_guide tool itself.
// Derived from guideFlowSpecs so the onboarding membership has one production
// source.
func isPrimaryTool(name string) bool {
	if name == "agent_guide" {
		return true
	}
	for _, s := range guideFlowSpecs {
		if !s.onboarding {
			continue
		}
		for _, step := range s.steps {
			if step == name {
				return true
			}
		}
	}
	return false
}

// primaryOnboardingNames returns the de-duplicated onboarding membership in
// stable order: agent_guide first, then each onboarding flow's steps in guide
// order. This is the bounded 13-name "start here" set the onboarding listing
// serves; it exists as a derivation seam so characterization tests pin the
// set's size and shape without hard-coding a second full copy of the names.
func primaryOnboardingNames() []string {
	names := []string{"agent_guide"}
	seen := map[string]bool{"agent_guide": true}
	for _, s := range guideFlowSpecs {
		if !s.onboarding {
			continue
		}
		for _, step := range s.steps {
			if !seen[step] {
				seen[step] = true
				names = append(names, step)
			}
		}
	}
	return names
}

// guideFlowNames returns the flow names in the spec table's declaration order
// — the order the static guide description enumerates. It exists as the
// derivation seam for agentGuideDescription (and its tests) so the description
// can never name a flow absent from the table, nor omit one present.
func guideFlowNames() []string {
	names := make([]string, 0, len(guideFlowSpecs))
	for _, s := range guideFlowSpecs {
		names = append(names, s.name)
	}
	return names
}

// ---------------------------------------------------------------------------
// Flow detail prose (tool-naming segments gate on per-server availability)
// ---------------------------------------------------------------------------

// toolPred is the availability predicate for desc-builder gating: the named
// tools (ALL of them) must be present on the completed surface. nil
// availability (the legacy/test path) is always-present, keeping historical
// prose.
func (a *guideAvailability) toolPred(names ...string) hostenv.Predicate {
	return func(hostenv.PlatformProfile) bool {
		for _, n := range names {
			if !a.toolPresent(n) {
				return false
			}
		}
		return true
	}
}

// headlessPrimitiveCandidates is the ONE ordered example list the open_app
// description (open_app.go) and the MCP-Apps guide rule both name for
// autonomous headless work. It must always be consumed through
// headlessPrimitiveExamplesFor, which gates every name on the completed
// per-server surface, so a hosted/minimal assembly never advertises a
// headless primitive it does not register.
var headlessPrimitiveCandidates = []string{"vault_status", "vault_put_file", "pins_list", "auth_sso", "pins_add"}

// headlessPrimitiveExamplesFor renders the availability-gated example
// headless-tool list shared by the open_app description and the agent guide's
// MCP-Apps rule: a literal list here would name tools that a hosted/restricted
// surface never registered. With no available candidates it degrades to the
// bare label instead of naming absent tools.
func headlessPrimitiveExamplesFor(available func(string) bool) string {
	var examples []string
	for _, n := range headlessPrimitiveCandidates {
		if available(n) {
			examples = append(examples, n)
		}
	}
	if len(examples) == 0 {
		return "headless primitives"
	}
	return "headless primitives (" + strings.Join(examples, ", ") + ", ...)"
}

// headlessPrimitiveExamples gates the shared candidate list on THIS
// guide's completed-surface availability.
func (a *guideAvailability) headlessPrimitiveExamples() string {
	return headlessPrimitiveExamplesFor(a.toolPresent)
}

// authFlowDetail attaches the auth flow's prose: only the steps and prose the
// completed surface actually registered (a hosted assembly names neither
// auth_sso nor auth_resume).
func authFlowDetail(b *toolforge.GuideFlowBuilder, avail *guideAvailability) *toolforge.GuideFlowBuilder {
	d := toolforge.Static("Run auth_status to check the current authentication state first.").
		WhenPred(avail.toolPred("auth_sso", "auth_resume"),
			"If unauthenticated, call auth_sso and poll auth_resume with the returned handle until the human completes the browser sign-in.").
		WhenPred(avail.appsPredicate("sso_signin"),
			"On this host you can also call open_app with app=\"sso_signin\" to render an interactive sign-in card for the human.")
	return b.Detail(d)
}

// vaultCreateFlowDetail attaches the vault-create wizard prose.
func vaultCreateFlowDetail(b *toolforge.GuideFlowBuilder, avail *guideAvailability) *toolforge.GuideFlowBuilder {
	return b.Detail(toolforge.Static("Call vault_create with a profile name; poll vault_create_resume with the returned handle; confirm with vault_status until unlocked.").
		WhenPred(avail.appsPredicate("vault_create"),
			"On this host you can also call open_app with app=\"vault_create\" to render the interactive vault creation wizard."))
}

// vaultRestoreFlowDetail attaches the vault-restore wizard prose.
func vaultRestoreFlowDetail(b *toolforge.GuideFlowBuilder, avail *guideAvailability) *toolforge.GuideFlowBuilder {
	return b.Detail(toolforge.Static("Call vault_restore; poll vault_restore_resume with the returned handle; confirm with vault_status until unlocked.").
		WhenPred(avail.appsPredicate("vault_restore"),
			"On this host you can also call open_app with app=\"vault_restore\" to render the interactive restore wizard."))
}

// uploadFlowDetail attaches the upload flow's decision + detail.
func uploadFlowDetail(b *toolforge.GuideFlowBuilder, avail *guideAvailability) *toolforge.GuideFlowBuilder {
	return b.Decision(byteRouteDecision(nil)).
		Detail(uploadDetailDescFor(avail))
}

// vaultUploadFlowDetail attaches the vault-upload flow's decision + detail.
func vaultUploadFlowDetail(b *toolforge.GuideFlowBuilder, avail *guideAvailability) *toolforge.GuideFlowBuilder {
	return b.Decision(vaultByteRouteDecision()).
		Detail(vaultUploadDetailDesc)
}

// downloadFlowDetail attaches the download flow's detail.
func downloadFlowDetail(b *toolforge.GuideFlowBuilder, _ *guideAvailability) *toolforge.GuideFlowBuilder {
	return b.Detail(downloadDetailDesc)
}

// vaultDownloadFlowDetail attaches the vault-download flow's detail.
func vaultDownloadFlowDetail(b *toolforge.GuideFlowBuilder, _ *guideAvailability) *toolforge.GuideFlowBuilder {
	return b.Detail(vaultDownloadDetailDesc)
}

// vaultShareFlowDetail attaches the vault-share flow's detail.
func vaultShareFlowDetail(b *toolforge.GuideFlowBuilder, _ *guideAvailability) *toolforge.GuideFlowBuilder {
	return b.Detail(vaultShareDetailDesc)
}

// vaultSyncFlowDetail attaches the vault-sync flow's detail: the
// related-utilities sentence names specific vault search results, so it is
// only emitted when those tools are actually registered.
func vaultSyncFlowDetail(b *toolforge.GuideFlowBuilder, avail *guideAvailability) *toolforge.GuideFlowBuilder {
	d := vaultSyncDetailLead.
		WhenPred(avail.toolPred("vault_ls", "vault_stat", "vault_tag_add", "vault_tag_rm", "vault_version_restore"),
			"Related utilities are discoverable via search_tools(category=storage): vault_ls, vault_stat, vault_tag_add, vault_tag_rm, vault_version_restore.")
	return b.Detail(d)
}

// pinsFlowDetail attaches the pins flow's detail; the pins_rm usage sentence
// is only emitted when pins_rm is registered.
func pinsFlowDetail(b *toolforge.GuideFlowBuilder, avail *guideAvailability) *toolforge.GuideFlowBuilder {
	return b.Detail(toolforge.Static("pins_add imports content already on IPFS by external CID; it is NOT for use after an upload tool (which already pins). pins_status takes one cid.").
		WhenPred(avail.toolPred("pins_rm"), "pins_rm requires confirm and exactly one of cids or all."))
}

// publishWebsiteFlowDetail attaches the publish_website flow's decision tree.
func publishWebsiteFlowDetail(b *toolforge.GuideFlowBuilder, _ *guideAvailability) *toolforge.GuideFlowBuilder {
	return b.Decision(byteRouteDecision(publishDomainDecision()))
}

// ensPublishFlowDetail attaches the ens_publish flow's decision tree.
func ensPublishFlowDetail(b *toolforge.GuideFlowBuilder, _ *guideAvailability) *toolforge.GuideFlowBuilder {
	return b.Decision(byteRouteDecision(
		toolforge.Decision("Point the ENS name at the CID?",
			toolforge.Branch("Yes — point the ENS/onchain domain at the content").
				Steps("ens_point").
				Detail(toolforge.Static("Search for ens_point (search_tools query \"ens\"), then call it with the onchain domain (e.g. vitalik.eth) and the cid from the upload. It creates or reuses the domain's IPNS key, publishes the CID, and returns the contenthash (ipns://<ipns-name>) plus a verify URL (eth.limo for .eth). The returned next_steps are onchain: the user sets the ENS resolver's contenthash field to the returned value from their own wallet or the ENS manager (app.ens.domains), the ENS SDK (ethers.js), or a wallet with ENS support. Do NOT assume a specific wallet. After the onchain transaction confirms, verify at the returned verify URL.")),
			toolforge.Branch("No — only publish the content to IPFS/IPNS, no onchain pointing").
				Steps("websites_create").
				Detail(toolforge.Static("Treat it as a normal website publish: websites_create with the cid. ENS pointing is only applied when the user explicitly wants their ENS name to resolve to the content.")),
		),
	))
}

// updateWebsiteFlowDetail attaches the update flow's detail.
func updateWebsiteFlowDetail(b *toolforge.GuideFlowBuilder, _ *guideAvailability) *toolforge.GuideFlowBuilder {
	return b.Detail(updateWebsiteDetailDesc)
}
