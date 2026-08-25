package wizard

import (
	"context"
	"errors"
	"fmt"

	ipfs "go.lumeweb.com/ipfs-sdk"

	"github.com/looplab/fsm"
)

// The website wizard is split into three small state machines instead of one
// monolithic linear chain. Each FSM owns a distinct concern and only mutates
// its own state, so a field written by one machine is never also written by
// another, and recovery from a failed claim becomes an explicit guarded
// transition rather than an ad-hoc setter. The outer websitesFSMEvents() chain
// still drives step progression; these machines model the resource lifecycle,
// content deployment, and async operation state underneath it.
//
// The WebsiteStateMachine reads the current state of each sub-machine from the
// underlying WebsitesWizardState (LifecycleState/ContentState/OpState) and
// fires transitions through a looplab FSM, writing the new sub-machine state
// back onto the flat state object, which remains the single in-memory source
// of truth.

// WebsiteLifecycleState models the lifecycle of the website resource itself.
type WebsiteLifecycleState string

const (
	LifecycleDraft   WebsiteLifecycleState = "draft"   // intent known, nothing claimed
	LifecycleClaimed WebsiteLifecycleState = "claimed" // domain source chosen (custom or platform root)
	LifecycleBinding WebsiteLifecycleState = "binding" // create submitted, subdomain bind in flight
	LifecycleLive    WebsiteLifecycleState = "live"    // website created and validated
	LifecycleFailed  WebsiteLifecycleState = "failed"  // bind/claim failed, retryable
)

// WebsiteContentState models content deployment state for the website.
type WebsiteContentState string

const (
	ContentNew      WebsiteContentState = "new"      // no content target selected yet
	ContentReady    WebsiteContentState = "ready"    // a CID is selected and ready to deploy
	ContentDeployed WebsiteContentState = "deployed" // the website targets the CID
)

// WebsiteOpState models an async operation (DNS reconcile, validation, bind).
type WebsiteOpState string

const (
	OpPending   WebsiteOpState = "pending"   // not started
	OpRunning   WebsiteOpState = "running"   // in flight
	OpSucceeded WebsiteOpState = "succeeded" // completed
	OpFailed    WebsiteOpState = "failed"    // completed with error
)

// Event names for the three sub-machines.
const (
	lifecycleEvChooseSource = "choose_source"
	lifecycleEvBindStart    = "bind_start"
	lifecycleEvBindSuccess  = "bind_success"
	lifecycleEvBindFail     = "bind_fail"
	lifecycleEvRetry        = "retry"

	contentEvPrepared = "content_prepared"
	contentEvDeployed = "content_deployed"

	opEvStart   = "op_start"
	opEvSuccess = "op_success"
	opEvFail    = "op_fail"
)

// lifecycleEvents returns the website lifecycle transition table.
func lifecycleEvents() []fsm.EventDesc {
	return []fsm.EventDesc{
		{Name: lifecycleEvChooseSource, Src: []string{string(LifecycleDraft)}, Dst: string(LifecycleClaimed)},
		{Name: lifecycleEvBindStart, Src: []string{string(LifecycleClaimed), string(LifecycleBinding), string(LifecycleFailed)}, Dst: string(LifecycleBinding)},
		{Name: lifecycleEvBindSuccess, Src: []string{string(LifecycleBinding)}, Dst: string(LifecycleLive)},
		{Name: lifecycleEvBindFail, Src: []string{string(LifecycleBinding)}, Dst: string(LifecycleFailed)},
		{Name: lifecycleEvRetry, Src: []string{string(LifecycleFailed)}, Dst: string(LifecycleBinding)},
	}
}

// contentEvents returns the content deployment transition table.
func contentEvents() []fsm.EventDesc {
	return []fsm.EventDesc{
		{Name: contentEvPrepared, Src: []string{string(ContentNew), string(ContentReady)}, Dst: string(ContentReady)},
		{Name: contentEvDeployed, Src: []string{string(ContentReady)}, Dst: string(ContentDeployed)},
	}
}

// opEvents returns the async operation transition table.
func opEvents() []fsm.EventDesc {
	return []fsm.EventDesc{
		{Name: opEvStart, Src: []string{string(OpPending), string(OpSucceeded), string(OpFailed)}, Dst: string(OpRunning)},
		{Name: opEvSuccess, Src: []string{string(OpRunning)}, Dst: string(OpSucceeded)},
		{Name: opEvFail, Src: []string{string(OpRunning)}, Dst: string(OpFailed)},
	}
}

// WebsiteStateMachine owns all mutations to WebsitesWizardState. Callers
// invoke one of its transition methods instead of calling the flat setters
// directly, so every write is guarded by a legal state transition.
type WebsiteStateMachine struct {
	// w is the flat state backed by the in-memory session object.
	w WebsitesWizardState
}

// NewWebsiteStateMachine returns a machine bound to w.
func NewWebsiteStateMachine(w WebsitesWizardState) *WebsiteStateMachine {
	return &WebsiteStateMachine{w: w}
}

// lifecycleCurrent returns the persisted lifecycle state, defaulting to Draft
// for a freshly created state object.
func (m *WebsiteStateMachine) lifecycleCurrent() WebsiteLifecycleState {
	if s := m.w.LifecycleState(); s != "" {
		return s
	}
	return LifecycleDraft
}

// contentCurrent returns the persisted content state, defaulting to New.
func (m *WebsiteStateMachine) contentCurrent() WebsiteContentState {
	if s := m.w.ContentState(); s != "" {
		return s
	}
	return ContentNew
}

// opCurrent returns the persisted op state, defaulting to Pending.
func (m *WebsiteStateMachine) opCurrent() WebsiteOpState {
	if s := m.w.OpState(); s != "" {
		return s
	}
	return OpPending
}

// fire transitions f with event, returning an error only when the event is
// not legal from the current state. looplab rejects self-transitions
// (src==dst) as errors; those are treated as successful no-ops so retries are
// idempotent. The caller persists f.Current() after a successful fire.
func (m *WebsiteStateMachine) fire(f *fsm.FSM, event string) error {
	if !f.Can(event) {
		return fmt.Errorf("no transition for %q from %s", event, f.Current())
	}
	// Can() is true even for a self-loop whose src==dst; looplab errors on
	// those, so ignore the return and use f.Current() as authoritative.
	_ = f.Event(context.Background(), event)
	return nil
}

// ChooseDomainSource marks the domain source (custom or platform) as chosen.
// It is idempotent: once the lifecycle has left draft, re-entering the domain
// step (e.g. after a validation error in a later branch of the same handler)
// must not fail. A retry that switches the source is still persisted while the
// lifecycle is only claimed (pre-create); once the website exists the source
// is fixed and a change is rejected so the caller reconciles state instead of
// silently routing through a stale source.
func (m *WebsiteStateMachine) ChooseDomainSource(source string) error {
	// Still drafting: choose it and persist.
	if m.lifecycleCurrent() == LifecycleDraft {
		f := fsm.NewFSM(string(m.lifecycleCurrent()), lifecycleEvents(), nil)
		if err := m.fire(f, lifecycleEvChooseSource); err != nil {
			return fmt.Errorf("cannot choose domain source in %s: %w", f.Current(), err)
		}
		m.w.SetLifecycleState(WebsiteLifecycleState(f.Current()))
		m.w.SetDomainSource(source)
		return nil
	}
	// Already past draft. A retry that switches custom<->platform is legal while
	// the source is only claimed (website not created yet). The previously
	// claimed domain no longer matches the new source, so it is dropped to keep
	// source and domain consistent for the create step.
	if m.lifecycleCurrent() == LifecycleClaimed {
		if cur := m.w.DomainSource(); cur != "" && cur != source {
			m.w.SetDomainSource(source)
			m.w.SetDomain("")
		}
		return nil
	}
	// Website exists (binding/live/failed): the source is fixed; reject a change
	// rather than silently persisting a stale or divergent source.
	if cur := m.w.DomainSource(); cur != "" && cur != source {
		return fmt.Errorf("cannot change domain source from %q to %q after the website was created", cur, source)
	}
	return nil
}

// MarkClaimed records the resolved FQDN (custom domain or platform subdomain).
// It is only legal before a website is created; reassigning a domain after the
// website exists is rejected.
func (m *WebsiteStateMachine) MarkClaimed(domain string) error {
	if m.lifecycleCurrent() == LifecycleLive || m.lifecycleCurrent() == LifecycleBinding {
		return fmt.Errorf("cannot change domain on a website in %s state", m.lifecycleCurrent())
	}
	// Choosing and claiming happen in the same domain step; if the source was
	// already chosen (draft->claimed completed) claiming is a no-op.
	if m.lifecycleCurrent() != LifecycleClaimed {
		f := fsm.NewFSM(string(m.lifecycleCurrent()), lifecycleEvents(), nil)
		if err := m.fire(f, lifecycleEvChooseSource); err != nil {
			return fmt.Errorf("cannot claim domain in %s: %w", f.Current(), err)
		}
		m.w.SetLifecycleState(WebsiteLifecycleState(f.Current()))
	}
	m.w.SetDomain(domain)
	return nil
}

// PrepareContent records that the given CID is ready to deploy.
func (m *WebsiteStateMachine) PrepareContent(cid string) error {
	f := fsm.NewFSM(string(m.contentCurrent()), contentEvents(), nil)
	if err := m.fire(f, contentEvPrepared); err != nil {
		return fmt.Errorf("cannot prepare content in %s: %w", f.Current(), err)
	}
	m.w.SetContentState(WebsiteContentState(f.Current()))
	m.w.SetCID(cid)
	return nil
}

// BeginBind moves the website into the binding state and records the created
// website. Call after deps.WebsitesService.CreateWithOptions succeeds. It is a
// no-op when already binding (e.g. retrying after a failed claim).
func (m *WebsiteStateMachine) BeginBind(website *ipfs.WebsiteItem) error {
	f := fsm.NewFSM(string(m.lifecycleCurrent()), lifecycleEvents(), nil)
	if err := m.fire(f, lifecycleEvBindStart); err != nil {
		return fmt.Errorf("cannot start bind in %s: %w", f.Current(), err)
	}
	m.w.SetLifecycleState(WebsiteLifecycleState(f.Current()))
	m.w.SetWebsite(website)
	return nil
}

// MarkDeployed moves content from ready to deployed after the website exists.
// It is idempotent: retrying the create step after a failed bind (content
// already deployed) must not wedge, so an already-deployed state is a no-op.
func (m *WebsiteStateMachine) MarkDeployed() error {
	if m.contentCurrent() == ContentDeployed {
		return nil
	}
	f := fsm.NewFSM(string(m.contentCurrent()), contentEvents(), nil)
	if err := m.fire(f, contentEvDeployed); err != nil {
		return fmt.Errorf("cannot mark deployed in %s: %w", f.Current(), err)
	}
	m.w.SetContentState(WebsiteContentState(f.Current()))
	return nil
}

// BindSucceeded records a successful platform (or custom) bind. mintedDomain,
// when non-empty, is the authoritative subdomain reflected from the bind
// response (mint-mismatch handling lives here, guarded by the transition).
func (m *WebsiteStateMachine) BindSucceeded(mintedDomain string) error {
	f := fsm.NewFSM(string(m.lifecycleCurrent()), lifecycleEvents(), nil)
	if err := m.fire(f, lifecycleEvBindSuccess); err != nil {
		return fmt.Errorf("cannot record bind success in %s: %w", f.Current(), err)
	}
	m.w.SetLifecycleState(WebsiteLifecycleState(f.Current()))
	if mintedDomain != "" && mintedDomain != m.w.Domain() {
		m.w.SetDomain(mintedDomain)
		if web := m.w.Website(); web != nil {
			web.Domain = mintedDomain
			m.w.SetWebsite(web)
		}
	}
	m.endOp(opEvSuccess)
	return nil
}

// BindFailed records a failed claim as a retryable lifecycle failure. It is a
// no-op when already failed.
func (m *WebsiteStateMachine) BindFailed() error {
	f := fsm.NewFSM(string(m.lifecycleCurrent()), lifecycleEvents(), nil)
	if err := m.fire(f, lifecycleEvBindFail); err != nil {
		return fmt.Errorf("cannot record bind failure in %s: %w", f.Current(), err)
	}
	m.w.SetLifecycleState(WebsiteLifecycleState(f.Current()))
	m.endOp(opEvFail)
	return nil
}

// StartValidation begins the validate operation.
func (m *WebsiteStateMachine) StartValidation() error {
	m.beginOp(opEvStart)
	return nil
}

// ValidateSucceeded records a successful validation.
func (m *WebsiteStateMachine) ValidateSucceeded() error {
	m.endOp(opEvSuccess)
	return nil
}

func (m *WebsiteStateMachine) beginOp(event string) {
	f := fsm.NewFSM(string(m.opCurrent()), opEvents(), nil)
	if err := m.fire(f, event); err != nil {
		return // op transitions are best-effort; they never block the wizard
	}
	m.w.SetOpState(WebsiteOpState(f.Current()))
}

func (m *WebsiteStateMachine) endOp(event string) {
	f := fsm.NewFSM(string(m.opCurrent()), opEvents(), nil)
	if err := m.fire(f, event); err != nil {
		return
	}
	m.w.SetOpState(WebsiteOpState(f.Current()))
}

// errNilWizardState is reported when a state machine method is invoked on a
// machine whose underlying state was never provided.
var errNilWizardState = errors.New("website wizard state is nil")
