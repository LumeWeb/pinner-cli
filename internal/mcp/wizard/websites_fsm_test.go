package wizard_test

import (
	"testing"

	ipfs "go.lumeweb.com/ipfs-sdk"

	wizard "go.lumeweb.com/pinner-cli/internal/mcp/wizard"
)

// TestWebsitesFSM_CustomDomainFlow drives the full custom-domain path through
// the state machine and asserts each composite state after every transition.
func TestWebsitesFSM_CustomDomainFlow(t *testing.T) {
	w := &testWebsitesWizard{}
	m := wizard.NewWebsiteStateMachine(w)

	if err := m.ChooseDomainSource(string(wizard.WebsitesDomainSourceCustom)); err != nil {
		t.Fatalf("ChooseDomainSource: %v", err)
	}
	if w.DomainSource() != string(wizard.WebsitesDomainSourceCustom) {
		t.Fatalf("domain source = %q, want custom", w.DomainSource())
	}
	if err := m.MarkClaimed("example.com"); err != nil {
		t.Fatalf("MarkClaimed: %v", err)
	}
	if w.Domain() != "example.com" {
		t.Fatalf("domain = %q, want example.com", w.Domain())
	}

	if err := m.MarkDeployed(); err == nil {
		t.Fatalf("MarkDeployed before content prepared should fail (no CID)")
	}

	if err := m.PrepareContent("QmTestHash123"); err != nil {
		t.Fatalf("PrepareContent: %v", err)
	}
	if w.CID() != "QmTestHash123" {
		t.Fatalf("cid = %q", w.CID())
	}
	if err := m.MarkDeployed(); err != nil {
		t.Fatalf("MarkDeployed: %v", err)
	}

	if err := m.BeginBind(&ipfs.WebsiteItem{}); err != nil {
		t.Fatalf("BeginBind: %v", err)
	}
	if w.Website() == nil {
		t.Fatalf("website not recorded")
	}

	if err := m.BindSucceeded(""); err != nil {
		t.Fatalf("BindSucceeded: %v", err)
	}
	if w.Website() == nil {
		t.Fatalf("website lost after bind")
	}

	// A domain change on a live website must be rejected.
	if err := m.MarkClaimed("other.com"); err == nil {
		t.Fatalf("MarkClaimed on live website should fail")
	}
}

// TestWebsitesFSM_PlatformGenerateMintMismatch verifies that when generate is
// used the bind response's minted subdomain is reflected back, and that a
// failed claim is recorded as a retryable lifecycle failure.
func TestWebsitesFSM_PlatformGenerateMintMismatch(t *testing.T) {
	w := &testWebsitesWizard{
		generate: true,
	}
	m := wizard.NewWebsiteStateMachine(w)

	if err := m.ChooseDomainSource(string(wizard.WebsitesDomainSourcePlatform)); err != nil {
		t.Fatalf("ChooseDomainSource: %v", err)
	}
	if err := m.MarkClaimed("pinned.site"); err != nil {
		t.Fatalf("MarkClaimed: %v", err)
	}
	if err := m.PrepareContent("QmTestHash123"); err != nil {
		t.Fatalf("PrepareContent: %v", err)
	}
	if err := m.MarkDeployed(); err != nil {
		t.Fatalf("MarkDeployed: %v", err)
	}

	website := &ipfs.WebsiteItem{Domain: "pinned.site"}
	if err := m.BeginBind(website); err != nil {
		t.Fatalf("BeginBind: %v", err)
	}

	// Bind mint-mismatch: the platform mints zebra.pinned.site, the state must
	// reflect the authoritative FQDN, not the create-time root.
	if err := m.BindSucceeded("zebra.pinned.site"); err != nil {
		t.Fatalf("BindSucceeded: %v", err)
	}
	if w.Domain() != "zebra.pinned.site" {
		t.Fatalf("domain = %q, want zebra.pinned.site (mint-mismatch reflection)", w.Domain())
	}
	if got := w.Website().Domain; got != "zebra.pinned.site" {
		t.Fatalf("website domain = %q, want zebra.pinned.site", got)
	}
}

// TestWebsitesFSM_PlatformBindFailureRecovery verifies a failed bind moves the
// lifecycle into a retryable failed state and that a subsequent bind retry can
// recover to live.
func TestWebsitesFSM_PlatformBindFailureRecovery(t *testing.T) {
	w := &testWebsitesWizard{generate: true}
	m := wizard.NewWebsiteStateMachine(w)

	if err := m.ChooseDomainSource(string(wizard.WebsitesDomainSourcePlatform)); err != nil {
		t.Fatalf("ChooseDomainSource: %v", err)
	}
	if err := m.MarkClaimed("pinned.site"); err != nil {
		t.Fatalf("MarkClaimed: %v", err)
	}
	if err := m.PrepareContent("QmTestHash123"); err != nil {
		t.Fatalf("PrepareContent: %v", err)
	}
	if err := m.MarkDeployed(); err != nil {
		t.Fatalf("MarkDeployed: %v", err)
	}
	if err := m.BeginBind(&ipfs.WebsiteItem{}); err != nil {
		t.Fatalf("BeginBind: %v", err)
	}

	if err := m.BindFailed(); err != nil {
		t.Fatalf("BindFailed: %v", err)
	}
	// The website still exists (it was created), recovery is a retryable bind.
	if w.Website() == nil {
		t.Fatalf("website should survive a failed claim")
	}

	// Retry the claim: binding again and succeeding recovers to live.
	if err := m.BeginBind(w.Website()); err != nil {
		t.Fatalf("retry BeginBind: %v", err)
	}
	if err := m.BindSucceeded("zebra.pinned.site"); err != nil {
		t.Fatalf("retry BindSucceeded: %v", err)
	}
	if w.Domain() != "zebra.pinned.site" {
		t.Fatalf("domain after recovery = %q, want zebra.pinned.site", w.Domain())
	}
}

// TestWebsitesFSM_IllegalTransitions asserts illegal transitions are rejected
// and leave state unchanged.
func TestWebsitesFSM_IllegalTransitions(t *testing.T) {
	w := &testWebsitesWizard{}
	m := wizard.NewWebsiteStateMachine(w)

	// BeginBind from draft (no domain claimed) is illegal.
	if err := m.BeginBind(&ipfs.WebsiteItem{}); err == nil {
		t.Fatalf("BeginBind from draft should fail")
	}
	if w.Website() != nil {
		t.Fatalf("website set despite illegal transition")
	}

	// ValidateSucceeded should be a no-op rather than an error (ops best-effort).
	if err := m.ValidateSucceeded(); err != nil {
		t.Fatalf("ValidateSucceeded should not error: %v", err)
	}
}

// TestWebsitesFSM_ChooseDomainSourceIdempotent verifies that re-entering the
// domain step (which happens on any error path inside the handler after the
// source is chosen) does not wedge the wizard with a failed transition.
func TestWebsitesFSM_ChooseDomainSourceIdempotent(t *testing.T) {
	w := &testWebsitesWizard{}
	m := wizard.NewWebsiteStateMachine(w)

	if err := m.ChooseDomainSource(string(wizard.WebsitesDomainSourceCustom)); err != nil {
		t.Fatalf("first ChooseDomainSource: %v", err)
	}
	// Simulate a retry after a later branch of the domain handler errored
	// (lifecycle already claimed): must be a successful no-op.
	if err := m.ChooseDomainSource(string(wizard.WebsitesDomainSourceCustom)); err != nil {
		t.Fatalf("ChooseDomainSource retry must be idempotent: %v", err)
	}
	if w.DomainSource() != string(wizard.WebsitesDomainSourceCustom) {
		t.Fatalf("domain source = %q", w.DomainSource())
	}
	// Claiming after choosing still works.
	if err := m.MarkClaimed("example.com"); err != nil {
		t.Fatalf("MarkClaimed after idempotent choose: %v", err)
	}
}

// TestWebsitesFSM_ChooseDomainSourceRetrySwitch verifies a retry that switches
// the source (custom -> platform) while the lifecycle is only claimed persists
// the new source instead of silently dropping it, and that once the website
// exists a source change is rejected.
func TestWebsitesFSM_ChooseDomainSourceRetrySwitch(t *testing.T) {
	w := &testWebsitesWizard{}
	m := wizard.NewWebsiteStateMachine(w)

	if err := m.ChooseDomainSource(string(wizard.WebsitesDomainSourceCustom)); err != nil {
		t.Fatalf("first choose custom: %v", err)
	}
	if err := m.MarkClaimed("example.com"); err != nil {
		t.Fatalf("MarkClaimed: %v", err)
	}

	// Retry switching to platform while still claimed: must persist, not no-op.
	if err := m.ChooseDomainSource(string(wizard.WebsitesDomainSourcePlatform)); err != nil {
		t.Fatalf("switch source while claimed: %v", err)
	}
	if w.DomainSource() != string(wizard.WebsitesDomainSourcePlatform) {
		t.Fatalf("domain source after switch = %q, want platform_subdomain", w.DomainSource())
	}

	// Once the website exists (binding), a source change must be rejected.
	if err := m.PrepareContent("QmTestHash123"); err != nil {
		t.Fatalf("PrepareContent: %v", err)
	}
	if err := m.MarkDeployed(); err != nil {
		t.Fatalf("MarkDeployed: %v", err)
	}
	if err := m.BeginBind(&ipfs.WebsiteItem{}); err != nil {
		t.Fatalf("BeginBind: %v", err)
	}
	if err := m.ChooseDomainSource(string(wizard.WebsitesDomainSourceCustom)); err == nil {
		t.Fatalf("source change after website creation should fail")
	}
}

// TestWebsitesFSM_MarkDeployedIdempotent verifies that retrying the create step
// after a failed bind (content already deployed) does not wedge, allowing the
// documented bind retry to proceed.
func TestWebsitesFSM_MarkDeployedIdempotent(t *testing.T) {
	w := &testWebsitesWizard{}
	m := wizard.NewWebsiteStateMachine(w)

	if err := m.ChooseDomainSource(string(wizard.WebsitesDomainSourcePlatform)); err != nil {
		t.Fatalf("ChooseDomainSource: %v", err)
	}
	if err := m.MarkClaimed("pinned.site"); err != nil {
		t.Fatalf("MarkClaimed: %v", err)
	}
	if err := m.PrepareContent("QmTestHash123"); err != nil {
		t.Fatalf("PrepareContent: %v", err)
	}
	if err := m.MarkDeployed(); err != nil {
		t.Fatalf("MarkDeployed: %v", err)
	}
	// Retry after a failed bind: MarkDeployed and BeginBind must be no-ops, and
	// a re-bind must be able to proceed to success.
	if err := m.MarkDeployed(); err != nil {
		t.Fatalf("MarkDeployed retry must be idempotent: %v", err)
	}
	if err := m.BeginBind(&ipfs.WebsiteItem{}); err != nil {
		t.Fatalf("BeginBind retry: %v", err)
	}
	if err := m.BindFailed(); err != nil {
		t.Fatalf("BindFailed: %v", err)
	}
	if err := m.BeginBind(w.Website()); err != nil {
		t.Fatalf("recovery BeginBind: %v", err)
	}
	if err := m.BindSucceeded("zebra.pinned.site"); err != nil {
		t.Fatalf("recovery BindSucceeded: %v", err)
	}
	if w.Domain() != "zebra.pinned.site" {
		t.Fatalf("domain after recovery = %q", w.Domain())
	}
}
