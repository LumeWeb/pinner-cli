package wizard

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/invopop/jsonschema"
	"github.com/looplab/fsm"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/websites"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"

	"go.lumeweb.com/pinner-cli/internal/mcp/auth"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/toolargs"
)

// --- FSM state constants ---

// Websites wizard FSM states.
const (
	StateWebsitesInit          = "init"
	StateWebsitesAuthCheck     = "auth_check"
	StateWebsitesContentSource = "content_source"
	StateWebsitesTargetType    = "target_type"
	StateWebsitesDomain        = "domain"
	StateWebsitesDNSMode       = "dns_mode"
	StateWebsitesCreate        = "create"
	StateWebsitesDNSSetup      = "dns_setup"
	StateWebsitesValidate      = "validate"
	StateWebsitesComplete      = "complete"
)

// Websites wizard FSM events.
const (
	EventWebsitesStart     = "start"
	EventWebsitesAuthOK    = "auth_ok"
	EventWebsitesContent   = "content_done"
	EventWebsitesTarget    = "target_done"
	EventWebsitesDomainSet = "domain_done"
	EventWebsitesDNSMode   = "dns_mode_done"
	EventWebsitesCreated   = "created"
	EventWebsitesDNSSet    = "dns_setup_done"
	EventWebsitesValidated = "validate_done"
	EventWebsitesAbort     = "abort"
)

// Domain wizard FSM states.
const (
	StateDomainInit            = "domain_init"
	StateDomainAuthCheck       = "domain_auth_check"
	StateDomainWebsite         = "domain_website"
	StateDomainName            = "domain_name"
	StateDomainNamespace       = "domain_namespace"
	StateDomainBind            = "domain_bind"
	StateDomainDelegationSetup = "domain_delegation_setup"
	StateDomainVerify          = "domain_verify"
	StateDomainComplete        = "domain_complete"
)

// Domain wizard FSM events.
const (
	EventDomainStart      = "domain_start"
	EventDomainAuthOK     = "domain_auth_ok"
	EventDomainWebsite    = "domain_website_done"
	EventDomainName       = "domain_name_done"
	EventDomainNamespace  = "domain_namespace_done"
	EventDomainBound      = "domain_bound"
	EventDomainDelegation = "domain_delegation_done"
	EventDomainVerified   = "domain_verified"
	EventDomainAbort      = "domain_abort"
)

// Setup wizard FSM states.
const (
	StateSetupInit       = "init"
	StateSetupAuth       = "auth"
	StateSetupConfig     = "config"
	StateSetupCompletion = "completion"
	StateSetupTutorial   = "tutorial"
	StateSetupComplete   = "complete"
)

// Setup wizard FSM events.
const (
	EventSetupStart      = "start"
	EventSetupAuthDone   = "auth_done"
	EventSetupConfigDone = "config_done"
	EventSetupCompDone   = "completion_done"
	EventSetupTutDone    = "tutorial_done"
	EventSetupAbort      = "abort"
)

// --- Enum types ---

// ContentSourceChoice specifies whether the user has a CID or needs to upload.
type ContentSourceChoice string

const (
	ContentSourceCID    ContentSourceChoice = "cid"
	ContentSourceUpload ContentSourceChoice = "upload"
)

// Valid reports whether the choice is a recognized value.
func (c ContentSourceChoice) Valid() bool {
	return c == ContentSourceCID || c == ContentSourceUpload
}

// TargetTypeValue specifies IPFS or IPNS content addressing.
type TargetTypeValue string

const (
	TargetTypeIPFS TargetTypeValue = "ipfs"
	TargetTypeIPNS TargetTypeValue = "ipns"
)

// Valid reports whether the target type is a recognized value.
func (t TargetTypeValue) Valid() bool {
	return t == TargetTypeIPFS || t == TargetTypeIPNS
}

// DNSModeValue specifies managed or self-managed DNS.
type DNSModeValue string

const (
	DNSModeManaged     DNSModeValue = "managed"
	DNSModeSelfManaged DNSModeValue = "self_managed"
)

// Valid reports whether the DNS mode is a recognized value.
func (m DNSModeValue) Valid() bool {
	return m == DNSModeManaged || m == DNSModeSelfManaged
}

// AuthStepChoiceValue specifies the setup auth step action.
type AuthStepChoiceValue string

const (
	AuthChoiceCreate AuthStepChoiceValue = "create_account"
	AuthChoiceSignIn AuthStepChoiceValue = "sign_in"
	AuthChoiceSkip   AuthStepChoiceValue = "skip"
)

// Valid reports whether the auth choice is a recognized value.
func (a AuthStepChoiceValue) Valid() bool {
	return a == AuthChoiceCreate || a == AuthChoiceSignIn || a == AuthChoiceSkip
}

// ConfigStepChoiceValue specifies the setup config step action.
type ConfigStepChoiceValue string

const (
	ConfigChoiceDefaults ConfigStepChoiceValue = "use_defaults"
	ConfigChoiceCustom   ConfigStepChoiceValue = "custom_endpoint"
	ConfigChoiceSkip     ConfigStepChoiceValue = "skip"
)

// Valid reports whether the config choice is a recognized value.
func (c ConfigStepChoiceValue) Valid() bool {
	return c == ConfigChoiceDefaults || c == ConfigChoiceCustom || c == ConfigChoiceSkip
}

// --- Input structs ---

// ContentSourceInput is the input for the content_source step.
type ContentSourceInput struct {
	Choice ContentSourceChoice `json:"choice" jsonschema:"enum=cid,enum=upload,description=Whether you have a CID ready or need to upload content first"`
	CID    string              `json:"cid,omitempty" jsonschema:"description=The IPFS CID (required when choice is cid)"`
}

// TargetTypeInput is the input for the target_type step.
type TargetTypeInput struct {
	Type TargetTypeValue `json:"type" jsonschema:"enum=ipfs,enum=ipns,description=Content addressing type: ipfs (immutable, content-addressed) or ipns (mutable name)"`
}

// WebsitesDomainSourceValue is the source of a website's domain.
type WebsitesDomainSourceValue string

const (
	// WebsitesDomainSourcePlatform uses a platform-provided (free) subdomain,
	// e.g. myapp.<platform-root>. This is the default when no domain is given.
	WebsitesDomainSourcePlatform WebsitesDomainSourceValue = "platform_subdomain"
	// WebsitesDomainSourceCustom uses a domain the user owns.
	WebsitesDomainSourceCustom WebsitesDomainSourceValue = "custom_domain"
)

// Valid reports whether the source value is one of the supported values.
func (v WebsitesDomainSourceValue) Valid() bool {
	return v == WebsitesDomainSourcePlatform || v == WebsitesDomainSourceCustom
}

// WebsitesDomainInput is the input for the domain step. It supports either a
// custom domain the user owns, or a platform-provided (free) subdomain. If no
// source is given, custom is inferred when a domain is supplied, otherwise it
// defaults to platform_subdomain so agents never have to invent a domain.
//
// For a custom domain: set source=custom_domain and domain.
// For a platform subdomain: set source=platform_subdomain with generate=true
// (default — platform auto-generates the label) or provide an explicit label
// only when the user has specifically requested one.
type WebsitesDomainInput struct {
	Source            WebsitesDomainSourceValue `json:"source,omitempty" jsonschema:"enum=platform_subdomain,enum=custom_domain,description=How the website domain is obtained. Defaults to platform_subdomain when no domain is given; defaults to custom_domain when a domain is supplied. If the user has no domain, use platform_subdomain."`
	Domain            string                    `json:"domain,omitempty" jsonschema:"description=The custom domain (e.g. example.com) when source=custom_domain. Not needed for platform_subdomain: the wizard derives the platform root automatically (or use platform_domain to pin a specific root)."`
	Label             string                    `json:"label,omitempty" jsonschema:"description=Explicit subdomain label under a platform root when source=platform_subdomain (e.g. myapp for myapp.<root>). Only use when the user has explicitly supplied or requested a specific label; otherwise prefer generate=true."`
	Generate          bool                      `json:"generate,omitempty" jsonschema:"description=Ask the platform to auto-generate a subdomain label. This is the default when no label preference exists. The wizard derives the platform root automatically; no label or FQDN is needed."`
	PlatformDomain    string                    `json:"platform_domain,omitempty" jsonschema:"description=Platform (free-subdomain) root domain to claim under (e.g. pinned.site). Defaults to domain when claiming."`
	PlatformNamespace string                    `json:"platform_namespace,omitempty" jsonschema:"description=Namespace within the platform domain to claim under (default icann)."`
}

// DNSModeInput is the input for the dns_mode step.
type DNSModeInput struct {
	Mode DNSModeValue `json:"mode" jsonschema:"enum=managed,enum=self_managed,description=managed = Pinner manages DNS, self_managed = user configures DNS records"`
}

// CreateInput is the input for the create step (explicit confirmation).
type CreateInput struct {
	Confirm bool `json:"confirm" jsonschema:"description=Must be true to confirm website creation (the website can be deleted later via websites_delete)"`
}

// ValidateInput is the input for the validate step.
type ValidateInput struct {
	Retry bool `json:"retry,omitempty" jsonschema:"description=Set to true to retry validation (useful when DNS propagation is pending)"`
}

// NamespaceValue specifies the domain namespace for a domain binding.
type NamespaceValue string

const (
	NamespaceICANN NamespaceValue = "icann"
	NamespaceHNS   NamespaceValue = "hns"
)

// Valid reports whether the namespace is a recognized value.
func (n NamespaceValue) Valid() bool {
	return n == NamespaceICANN || n == NamespaceHNS
}

// WebsiteInput is the input for the website selection step.
type WebsiteInput struct {
	WebsiteID string `json:"website_id" jsonschema:"description=The numeric ID of the website to bind the domain to"`
}

// DomainNameInput is the input for the domain name step. It supports either
// binding a plain domain or claiming a platform (free-subdomain) subdomain:
// set label (or generate=true) plus an optional platform_domain root to claim
// a subdomain, instead of providing a plain owned domain.
type DomainNameInput struct {
	Domain string `json:"domain" jsonschema:"description=The domain name to bind (e.g. mydomain, staging.example.com) or the platform root domain when claiming a free subdomain (e.g. pinned.site)"`
	// Platform (free-subdomain) claiming: supply label or generate=true to
	// claim a subdomain instead of binding a plain owned domain.
	Label             string `json:"label,omitempty" jsonschema:"description=Explicit subdomain label to claim under a platform domain (e.g. myblog for myblog.pinned.site)"`
	Generate          bool   `json:"generate,omitempty" jsonschema:"description=Ask the platform to auto-generate a subdomain label instead of supplying one"`
	PlatformDomain    string `json:"platform_domain,omitempty" jsonschema:"description=Platform (free-subdomain) root domain to claim under. Defaults to domain when claiming."`
	PlatformNamespace string `json:"platform_namespace,omitempty" jsonschema:"description=Namespace within the platform domain to claim under"`
}

// NamespaceInput is the input for the namespace step.
type NamespaceInput struct {
	Namespace NamespaceValue `json:"namespace" jsonschema:"enum=icann,enum=hns,description=The domain namespace: icann (traditional) or hns (Handshake)"`
}

// BindInput is the input for the bind domain step (explicit confirmation).
type BindInput struct {
	Confirm bool `json:"confirm" jsonschema:"description=Must be true to confirm binding the domain"`
}

// DomainVerifyInput is the input for the domain verify step.
type DomainVerifyInput struct {
	Retry bool `json:"retry,omitempty" jsonschema:"description=Set to true to retry verification (useful when DNS propagation is pending)"`
}

// SetupAuthInput is the input for the setup auth step. Credentials are NOT
// accepted here: sign_in starts an out-of-band login the human completes in a
// browser, so passwords and OTP codes never transit the MCP/LLM channel.
type SetupAuthInput struct {
	Choice AuthStepChoiceValue `json:"choice" jsonschema:"enum=create_account,enum=sign_in,enum=skip,description=Whether to create a new account, sign in, or skip auth"`
	Email  string              `json:"email,omitempty" jsonschema:"description=Email address (required for create_account and sign_in)"`
}

// SetupConfigInput is the input for the setup config step.
type SetupConfigInput struct {
	Choice   ConfigStepChoiceValue `json:"choice" jsonschema:"enum=use_defaults,enum=custom_endpoint,enum=skip,description=Use default endpoint, custom endpoint, or skip config"`
	Endpoint string                `json:"endpoint,omitempty" jsonschema:"description=Custom API endpoint URL (required when choice is custom_endpoint)"`
	Secure   bool                  `json:"secure,omitempty" jsonschema:"description=Use HTTPS for API calls"`
}

// SetupCompletionInput is the input for the setup shell completion step.
type SetupCompletionInput struct {
	Shell string `json:"shell,omitempty" jsonschema:"enum=bash,enum=zsh,enum=fish,enum=pwsh,description=Shell name for completion setup (informational; user runs 'pinner completion <shell>')"`
}

// NoInput is the input for steps that take no parameters.
type NoInput struct{}

// --- Step response ---

// StepResponse is the structured JSON returned by wizard step tools.
// It tells the agent the current step, the next step name, and the JSON
// schema describing the input the next step expects.
type StepResponse struct {
	SessionID      string             `json:"session_id"`
	CurrentStep    string             `json:"current_step"`
	NextStep       string             `json:"next_step,omitempty"`
	NextStepSchema *jsonschema.Schema `json:"next_step_schema,omitempty"`
	Complete       bool               `json:"complete,omitempty"`
	Message        string             `json:"message,omitempty"`
	Error          string             `json:"error,omitempty"`
}

// --- Exported test helpers ---

// BuildStepResponseForTest is an exported wrapper of buildStepResponse for
// use by external test packages. It constructs a StepResponse from the
// current session state.
func BuildStepResponseForTest(sess *session.Session) StepResponse {
	return buildStepResponse(sess)
}

// NewWebsitesFSMForTest creates a websites wizard FSM for testing.
func NewWebsitesFSMForTest() *fsm.FSM {
	return fsm.NewFSM(StateWebsitesInit, websitesFSMEvents(), nil)
}

// NewSetupFSMForTest creates a setup wizard FSM for testing.
func NewSetupFSMForTest() *fsm.FSM {
	return fsm.NewFSM(StateSetupInit, setupFSMEvents(), nil)
}

// --- FSM builders ---

// websitesFSMEvents returns the event descriptors for the websites wizard FSM.
func websitesFSMEvents() []fsm.EventDesc {
	return []fsm.EventDesc{
		{Name: EventWebsitesStart, Src: []string{StateWebsitesInit}, Dst: StateWebsitesAuthCheck},
		{Name: EventWebsitesAbort, Src: []string{StateWebsitesInit}, Dst: StateWebsitesComplete},
		{Name: EventWebsitesAuthOK, Src: []string{StateWebsitesAuthCheck}, Dst: StateWebsitesContentSource},
		{Name: EventWebsitesAbort, Src: []string{StateWebsitesAuthCheck}, Dst: StateWebsitesComplete},
		{Name: EventWebsitesContent, Src: []string{StateWebsitesContentSource}, Dst: StateWebsitesTargetType},
		{Name: EventWebsitesAbort, Src: []string{StateWebsitesContentSource}, Dst: StateWebsitesComplete},
		{Name: EventWebsitesTarget, Src: []string{StateWebsitesTargetType}, Dst: StateWebsitesDomain},
		{Name: EventWebsitesAbort, Src: []string{StateWebsitesTargetType}, Dst: StateWebsitesComplete},
		{Name: EventWebsitesDomainSet, Src: []string{StateWebsitesDomain}, Dst: StateWebsitesDNSMode},
		{Name: EventWebsitesAbort, Src: []string{StateWebsitesDomain}, Dst: StateWebsitesComplete},
		{Name: EventWebsitesDNSMode, Src: []string{StateWebsitesDNSMode}, Dst: StateWebsitesCreate},
		{Name: EventWebsitesAbort, Src: []string{StateWebsitesDNSMode}, Dst: StateWebsitesComplete},
		{Name: EventWebsitesCreated, Src: []string{StateWebsitesCreate}, Dst: StateWebsitesDNSSetup},
		{Name: EventWebsitesAbort, Src: []string{StateWebsitesCreate}, Dst: StateWebsitesComplete},
		{Name: EventWebsitesDNSSet, Src: []string{StateWebsitesDNSSetup}, Dst: StateWebsitesValidate},
		{Name: EventWebsitesAbort, Src: []string{StateWebsitesDNSSetup}, Dst: StateWebsitesComplete},
		{Name: EventWebsitesValidated, Src: []string{StateWebsitesValidate}, Dst: StateWebsitesComplete},
		{Name: EventWebsitesAbort, Src: []string{StateWebsitesValidate}, Dst: StateWebsitesComplete},
	}
}

// setupFSMEvents returns the event descriptors for the setup wizard FSM.
func setupFSMEvents() []fsm.EventDesc {
	return []fsm.EventDesc{
		{Name: EventSetupStart, Src: []string{StateSetupInit}, Dst: StateSetupAuth},
		{Name: EventSetupAbort, Src: []string{StateSetupInit}, Dst: StateSetupComplete},
		{Name: EventSetupAuthDone, Src: []string{StateSetupAuth}, Dst: StateSetupConfig},
		{Name: EventSetupAbort, Src: []string{StateSetupAuth}, Dst: StateSetupComplete},
		{Name: EventSetupConfigDone, Src: []string{StateSetupConfig}, Dst: StateSetupCompletion},
		{Name: EventSetupAbort, Src: []string{StateSetupConfig}, Dst: StateSetupComplete},
		{Name: EventSetupCompDone, Src: []string{StateSetupCompletion}, Dst: StateSetupTutorial},
		{Name: EventSetupAbort, Src: []string{StateSetupCompletion}, Dst: StateSetupComplete},
		{Name: EventSetupTutDone, Src: []string{StateSetupTutorial}, Dst: StateSetupComplete},
		{Name: EventSetupAbort, Src: []string{StateSetupTutorial}, Dst: StateSetupComplete},
	}
}

// domainFSMEvents returns the event descriptors for the domain wizard FSM.
func domainFSMEvents() []fsm.EventDesc {
	return []fsm.EventDesc{
		{Name: EventDomainStart, Src: []string{StateDomainInit}, Dst: StateDomainAuthCheck},
		{Name: EventDomainAbort, Src: []string{StateDomainInit}, Dst: StateDomainComplete},
		{Name: EventDomainAuthOK, Src: []string{StateDomainAuthCheck}, Dst: StateDomainWebsite},
		{Name: EventDomainAbort, Src: []string{StateDomainAuthCheck}, Dst: StateDomainComplete},
		{Name: EventDomainWebsite, Src: []string{StateDomainWebsite}, Dst: StateDomainName},
		{Name: EventDomainAbort, Src: []string{StateDomainWebsite}, Dst: StateDomainComplete},
		{Name: EventDomainName, Src: []string{StateDomainName}, Dst: StateDomainNamespace},
		{Name: EventDomainAbort, Src: []string{StateDomainName}, Dst: StateDomainComplete},
		{Name: EventDomainNamespace, Src: []string{StateDomainNamespace}, Dst: StateDomainBind},
		{Name: EventDomainAbort, Src: []string{StateDomainNamespace}, Dst: StateDomainComplete},
		{Name: EventDomainBound, Src: []string{StateDomainBind}, Dst: StateDomainDelegationSetup},
		{Name: EventDomainAbort, Src: []string{StateDomainBind}, Dst: StateDomainComplete},
		{Name: EventDomainDelegation, Src: []string{StateDomainDelegationSetup}, Dst: StateDomainVerify},
		{Name: EventDomainAbort, Src: []string{StateDomainDelegationSetup}, Dst: StateDomainComplete},
		{Name: EventDomainVerified, Src: []string{StateDomainVerify}, Dst: StateDomainComplete},
		{Name: EventDomainAbort, Src: []string{StateDomainVerify}, Dst: StateDomainComplete},
	}
}

// --- Websites wizard step definitions ---
var emptySchema = &jsonschema.Schema{
	Type:       "object",
	Properties: nil,
}

// --- Websites wizard step definitions ---

// WebsitesWizardDeps holds the dependencies needed to build and run the
// websites wizard session steps.
type WebsitesWizardDeps struct {
	CfgMgr           config.Manager
	WebsitesService  WebsitesService
	WebsitesResource WebsitesResourceProvider
	WebsitesFactory  WebsitesWizardFactory
}

// platformAvailability probes which platform (free-subdomain) roots can
// host the candidate label. It uses the websites resource provider (which
// carries CheckPlatformDomainAvailability through to the portal).
func platformAvailability(ctx context.Context, deps WebsitesWizardDeps, label string) (*ipfs.PlatformAvailabilityResponse, error) {
	if deps.WebsitesResource == nil {
		return nil, fmt.Errorf("platform domain availability is unavailable in this context")
	}
	resp, err := deps.WebsitesResource.CheckPlatformDomainAvailability(ctx, label)
	if err != nil {
		return nil, fmt.Errorf("platform subdomain availability check failed: %w", err)
	}
	return resp, nil
}

// firstAvailableRoot returns the first available platform root from the
// availability response, or "" if none is available.
func firstAvailableRoot(resp *ipfs.PlatformAvailabilityResponse) string {
	if resp == nil {
		return ""
	}
	for _, r := range resp.Results {
		if r.Available {
			return r.PlatformDomain
		}
	}
	return ""
}

// listRoots returns the comma-separated list of platform roots for error
// messages.
func listRoots(resp *ipfs.PlatformAvailabilityResponse) string {
	if resp == nil {
		return ""
	}
	var roots []string
	for _, r := range resp.Results {
		roots = append(roots, r.PlatformDomain)
	}
	return strings.Join(roots, ", ")
}

// firstPlatformDomain returns the first enabled platform root from the
// platform-domain list, or "" if none is available.
func firstPlatformDomain(resp *ipfs.PlatformDomainListResponse) string {
	if resp == nil {
		return ""
	}
	for _, r := range resp.Data {
		if r.Enabled {
			return r.Domain
		}
	}
	return ""
}

// listPlatformDomainNames returns the comma-separated list of platform roots
// for error messages.
func listPlatformDomainNames(resp *ipfs.PlatformDomainListResponse) string {
	if resp == nil {
		return ""
	}
	var roots []string
	for _, r := range resp.Data {
		roots = append(roots, r.Domain)
	}
	return strings.Join(roots, ", ")
}

// buildWebsitesSteps returns the StepDef slice for the websites wizard.
// Each step's handler decodes JSON input, validates it, and mutates the
// WebsitesWizardState state stored in the session.
func buildWebsitesSteps(deps WebsitesWizardDeps) []session.StepDef {
	return []session.StepDef{
		{
			Name:  StateWebsitesAuthCheck,
			Event: EventWebsitesAuthOK,
			Handler: func(ctx context.Context, sess *session.Session, _ json.RawMessage) (string, error) {
				if deps.CfgMgr.Config().AuthToken == "" {
					return "", fmt.Errorf("authentication required: run 'pinner auth' or set --auth-token")
				}
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[NoInput]()
			},
		},
		{
			Name:  StateWebsitesContentSource,
			Event: EventWebsitesContent,
			Handler: func(ctx context.Context, sess *session.Session, input json.RawMessage) (string, error) {
				w := sess.State().(WebsitesWizardState)
				var in ContentSourceInput
				if err := json.Unmarshal(input, &in); err != nil {
					return "", fmt.Errorf("invalid input: %w", err)
				}
				if !in.Choice.Valid() {
					return "", fmt.Errorf("invalid choice: %s (expected \"cid\" or \"upload\")", in.Choice)
				}
				if in.Choice == ContentSourceUpload {
					return "", fmt.Errorf("content upload required: run 'pinner upload <file>' first, then restart the wizard")
				}
				if in.CID == "" {
					return "", fmt.Errorf("cid cannot be empty when choice is \"cid\"")
				}
				if err := NewWebsiteStateMachine(w).PrepareContent(in.CID); err != nil {
					return "", err
				}
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[ContentSourceInput]()
			},
		},
		{
			Name:  StateWebsitesTargetType,
			Event: EventWebsitesTarget,
			Handler: func(ctx context.Context, sess *session.Session, input json.RawMessage) (string, error) {
				w := sess.State().(WebsitesWizardState)
				var in TargetTypeInput
				if err := json.Unmarshal(input, &in); err != nil {
					return "", fmt.Errorf("invalid input: %w", err)
				}
				if !in.Type.Valid() {
					return "", fmt.Errorf("invalid target type: %s (expected \"ipfs\" or \"ipns\")", in.Type)
				}
				w.SetTargetType(string(in.Type))
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[TargetTypeInput]()
			},
		},
		{
			Name:  StateWebsitesDomain,
			Event: EventWebsitesDomainSet,
			Handler: func(ctx context.Context, sess *session.Session, input json.RawMessage) (string, error) {
				w := sess.State().(WebsitesWizardState)
				var in WebsitesDomainInput
				if err := json.Unmarshal(input, &in); err != nil {
					return "", fmt.Errorf("invalid input: %w", err)
				}

				// Resolve the domain source. An explicit source wins; otherwise
				// a supplied domain implies custom (backward compatible), and a
				// missing domain defaults to a platform subdomain so agents
				// never have to invent a domain.
				custom := false
				source := in.Source
				switch source {
				case "":
					if in.Domain != "" {
						source = WebsitesDomainSourceCustom
						custom = true
					} else {
						source = WebsitesDomainSourcePlatform
					}
				case WebsitesDomainSourceCustom:
					custom = true
				case WebsitesDomainSourcePlatform:
				default:
					return "", fmt.Errorf("invalid domain source: %s (expected \"platform_subdomain\" or \"custom_domain\")", in.Source)
				}
				machine := NewWebsiteStateMachine(w)
				if err := machine.ChooseDomainSource(string(source)); err != nil {
					return "", err
				}

				if custom {
					if in.Domain == "" {
						return "", fmt.Errorf("domain cannot be empty when source=custom_domain")
					}
					if err := machine.MarkClaimed(in.Domain); err != nil {
						return "", err
					}
					return "", nil
				}

				// Neither label nor generate: default to generate=true as documented
				// in the schema, agent_guide, and prompt templates.
				if !in.Generate && in.Label == "" {
					in.Generate = true
				}

				// Platform (free) subdomain path: label -> availability/root ->
				// exact FQDN. Mirrors the DomainWizard claim semantics.
				w.SetLabel(in.Label)
				w.SetGenerate(in.Generate)
				w.SetPlatformNamespace(in.PlatformNamespace)

				if in.Label != "" {
					root := in.PlatformDomain
					if root == "" {
						root = in.Domain
					}
					if root == "" {
						resp, err := platformAvailability(ctx, deps, in.Label)
						if err != nil {
							return "", err
						}
						root = firstAvailableRoot(resp)
						if root == "" {
							return "", fmt.Errorf("no available platform root for label %q (available roots: %s)", in.Label, listRoots(resp))
						}
					}
					w.SetPlatformDomain(root)
					if err := machine.MarkClaimed(in.Label + "." + root); err != nil {
						return "", err
					}
					return "", nil
				}

				// No label: auto-generate. The tool derives the platform root from an
				// explicit platform_domain (or domain), else lists the enabled
				// platform roots and picks the first one, so the agent never has to
				// supply an FQDN. Enabled roots are those the platform advertises as
				// accepting subdomain claims; availability cannot be enumerated here
				// because CheckPlatformDomainAvailability requires a specific label.
				// The same root is used as the create FQDN and as the platform_domain
				// that the later BindDomain mints under, keeping the created website
				// and the claimed subdomain consistent.
				if in.Generate {
					root := in.PlatformDomain
					if root == "" {
						root = in.Domain
					}
					if root == "" {
						if deps.WebsitesResource == nil {
							return "", fmt.Errorf("platform domain listing is unavailable in this context")
						}
						resp, err := deps.WebsitesResource.ListPlatformDomains(ctx)
						if err != nil {
							return "", fmt.Errorf("list platform domains: %w", err)
						}
						root = firstPlatformDomain(resp)
						if root == "" {
							return "", fmt.Errorf("no available platform root to auto-generate a subdomain under (available roots: %s)", listPlatformDomainNames(resp))
						}
					}
					w.SetPlatformDomain(root)
					if err := machine.MarkClaimed(root); err != nil {
						return "", err
					}
					return "", nil
				}

			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[WebsitesDomainInput]()
			},
		},
		{
			Name:  StateWebsitesDNSMode,
			Event: EventWebsitesDNSMode,
			Handler: func(ctx context.Context, sess *session.Session, input json.RawMessage) (string, error) {
				w := sess.State().(WebsitesWizardState)
				var in DNSModeInput
				if err := json.Unmarshal(input, &in); err != nil {
					return "", fmt.Errorf("invalid input: %w", err)
				}
				if !in.Mode.Valid() {
					return "", fmt.Errorf("invalid DNS mode: %s (expected \"managed\" or \"self_managed\")", in.Mode)
				}
				w.SetDNSHosting(in.Mode == DNSModeManaged)
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[DNSModeInput]()
			},
		},
		{
			Name:  StateWebsitesCreate,
			Event: EventWebsitesCreated,
			Handler: func(ctx context.Context, sess *session.Session, input json.RawMessage) (string, error) {
				w := sess.State().(WebsitesWizardState)
				var in CreateInput
				if err := json.Unmarshal(input, &in); err != nil {
					return "", fmt.Errorf("invalid input: %w", err)
				}
				if !in.Confirm {
					return "", fmt.Errorf("confirmation required: set confirm=true to create the website")
				}

				// Call WebsitesService directly; the CLI wizard's create method is unexported.
				targetType := w.TargetType()
				if targetType == "" {
					targetType = string(TargetTypeIPFS)
				}
				dnsHosting := w.DNSHosting()
				isPlatform := w.DomainSource() == string(WebsitesDomainSourcePlatform)
				req := ipfs.WebsiteRequest{
					TargetHash: w.CID(),
					TargetType: targetType,
				}
				if isPlatform {
					// Platform (free) subdomains are DNS-managed by the platform and
					// are claimed atomically at create; no domain is supplied.
					managed := true
					req.DnsHostingEnabled = &managed
					w.SetDNSHosting(true)
					if pd := w.PlatformDomain(); pd != "" {
						req.PlatformDomain = &pd
					}
					if pns := w.PlatformNamespace(); pns != "" {
						req.PlatformNamespace = &pns
					}
					if w.Generate() {
						g := true
						req.Generate = &g
					} else if label := w.Label(); label != "" {
						req.Label = &label
					}
				} else {
					if w.Domain() == "" {
						return "", fmt.Errorf("website domain is not set")
					}
					domain := w.Domain()
					req.Domain = &domain
					req.DnsHostingEnabled = &dnsHosting
				}
				machine := NewWebsiteStateMachine(w)

				// CreateWithOptions is not idempotent; once Website is set (a prior
				// create succeeded), resume the lifecycle transition instead of
				// re-creating (which would orphan the prior site and mint a duplicate
				// subdomain).
				existing := w.Website()
				resume := existing != nil &&
					(w.LifecycleState() == LifecycleFailed || w.LifecycleState() == LifecycleBinding)
				website := existing
				if !resume {
					created, err := deps.WebsitesService.CreateWithOptions(ctx, req)
					if err != nil {
						return "", fmt.Errorf("website creation failed: %w", websites.TranslateError(err))
					}
					website = created
					if err := machine.MarkDeployed(); err != nil {
						return "", err
					}
				}
				if err := machine.BeginBind(website); err != nil {
					return "", err
				}

				// The subdomain (when claimed) is minted atomically at create time,
				// so the created website's domain IS the authoritative serving FQDN.
				// Reflect it in state so later steps report the claimed subdomain.
				minted := ""
				if website != nil {
					minted = website.Domain
				}
				if err := machine.BindSucceeded(minted); err != nil {
					return "", err
				}
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[CreateInput]()
			},
		},
		{
			Name:  StateWebsitesDNSSetup,
			Event: EventWebsitesDNSSet,
			Handler: func(_ context.Context, _ *session.Session, _ json.RawMessage) (string, error) {
				// DNS setup is informational; no state mutation needed.
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[NoInput]()
			},
		},
		{
			Name:  StateWebsitesValidate,
			Event: EventWebsitesValidated,
			Handler: func(ctx context.Context, sess *session.Session, input json.RawMessage) (string, error) {
				w := sess.State().(WebsitesWizardState)
				var in ValidateInput
				if len(input) > 0 && string(input) != "null" {
					if err := json.Unmarshal(input, &in); err != nil {
						return "", fmt.Errorf("invalid input: %w", err)
					}
				}
				_ = in // The retry flag is informational; validation always runs.

				website := w.Website()
				if website == nil {
					return "", fmt.Errorf("website not created yet")
				}

				// Call WebsitesService directly; the CLI wizard's validate method is unexported.
				id := fmt.Sprintf("%d", website.Id)
				machine := NewWebsiteStateMachine(w)
				machine.StartValidation()
				result, err := deps.WebsitesService.Validate(ctx, id)
				if err != nil {
					return "", fmt.Errorf("validation failed: %w", websites.TranslateError(err))
				}
				w.SetValidationResult(result)
				machine.ValidateSucceeded()
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[ValidateInput]()
			},
		},
	}
}

// --- Domain wizard step definitions ---

// DomainWizardDeps holds the dependencies needed to build and run the
// domain addition wizard session steps.
type DomainWizardDeps struct {
	CfgMgr          config.Manager
	WebsitesService WebsitesService
	DomainFactory   DomainWizardFactory
}

// buildDomainSteps returns the StepDef slice for the domain addition wizard.
// Each step's handler decodes JSON input, validates it, and mutates the
// DomainWizardState state stored in the session.
func buildDomainSteps(deps DomainWizardDeps) []session.StepDef {
	return []session.StepDef{
		{
			Name:  StateDomainAuthCheck,
			Event: EventDomainAuthOK,
			Handler: func(ctx context.Context, sess *session.Session, _ json.RawMessage) (string, error) {
				if deps.CfgMgr.Config().AuthToken == "" {
					return "", fmt.Errorf("authentication required: run 'pinner auth' or set --auth-token")
				}
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[NoInput]()
			},
		},
		{
			Name:  StateDomainWebsite,
			Event: EventDomainWebsite,
			Handler: func(ctx context.Context, sess *session.Session, input json.RawMessage) (string, error) {
				w := sess.State().(DomainWizardState)
				var in WebsiteInput
				if err := json.Unmarshal(input, &in); err != nil {
					return "", fmt.Errorf("invalid input: %w", err)
				}
				if in.WebsiteID == "" {
					return "", fmt.Errorf("website_id cannot be empty")
				}

				websites, err := deps.WebsitesService.List(ctx, websites.ListOptions{})
				if err != nil {
					return "", fmt.Errorf("failed to load websites: %w", err)
				}
				matched := false
				for _, ws := range websites {
					if fmt.Sprintf("%d", ws.Id) == in.WebsiteID {
						w.SetWebsiteID(in.WebsiteID)
						w.SetWebsiteDomain(ws.Domain)
						matched = true
						break
					}
				}
				if !matched {
					return "", fmt.Errorf("website with ID %q not found; list available websites with 'pinner websites list'", in.WebsiteID)
				}
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[WebsiteInput]()
			},
		},
		{
			Name:  StateDomainName,
			Event: EventDomainName,
			Handler: func(_ context.Context, sess *session.Session, input json.RawMessage) (string, error) {
				w := sess.State().(DomainWizardState)
				var in DomainNameInput
				if err := json.Unmarshal(input, &in); err != nil {
					return "", fmt.Errorf("invalid input: %w", err)
				}
				if in.Domain == "" {
					return "", fmt.Errorf("domain cannot be empty")
				}
				w.SetDomain(in.Domain)
				w.SetLabel(in.Label)
				w.SetGenerate(in.Generate)
				w.SetPlatformNamespace(in.PlatformNamespace)
				// A label or generate request marks a platform subdomain claim.
				// Default the platform root to the supplied domain when not
				// given explicitly (the domain is then the platform root).
				if in.PlatformDomain != "" {
					w.SetPlatformDomain(in.PlatformDomain)
				} else if in.Generate || in.Label != "" {
					w.SetPlatformDomain(in.Domain)
				} else {
					w.SetPlatformDomain("")
				}
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[DomainNameInput]()
			},
		},
		{
			Name:  StateDomainNamespace,
			Event: EventDomainNamespace,
			Handler: func(_ context.Context, sess *session.Session, input json.RawMessage) (string, error) {
				w := sess.State().(DomainWizardState)
				var in NamespaceInput
				if err := json.Unmarshal(input, &in); err != nil {
					return "", fmt.Errorf("invalid input: %w", err)
				}
				if !in.Namespace.Valid() {
					return "", fmt.Errorf("invalid namespace: %s (expected \"icann\" or \"hns\")", in.Namespace)
				}
				w.SetNamespace(string(in.Namespace))
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[NamespaceInput]()
			},
		},
		{
			Name:  StateDomainBind,
			Event: EventDomainBound,
			Handler: func(ctx context.Context, sess *session.Session, input json.RawMessage) (string, error) {
				w := sess.State().(DomainWizardState)
				var in BindInput
				if err := json.Unmarshal(input, &in); err != nil {
					return "", fmt.Errorf("invalid input: %w", err)
				}
				if !in.Confirm {
					return "", fmt.Errorf("confirmation required: set confirm=true to bind the domain")
				}
				if w.Domain() == "" {
					return "", fmt.Errorf("domain name not set")
				}
				if w.Namespace() == "" {
					return "", fmt.Errorf("namespace not set")
				}

				req := ipfs.DomainRequest{
					Domain:    w.Domain(),
					Namespace: w.Namespace(),
				}
				// Pass platform (free-subdomain) claim fields through so the
				// portal can mint a subdomain at bind time (label supplied
				// explicitly, or auto-generated via generate). Consistent with
				// the websites_domains_add catalogop.
				if pd := w.PlatformDomain(); pd != "" {
					req.PlatformDomain = &pd
				}
				if pns := w.PlatformNamespace(); pns != "" {
					req.PlatformNamespace = &pns
				}
				if w.Generate() {
					g := true
					req.Generate = &g
				}
				if label := w.Label(); label != "" {
					req.Label = &label
				}
				domainResp, err := deps.WebsitesService.BindDomain(ctx, w.WebsiteID(), req)
				if err != nil {
					return "", fmt.Errorf("domain binding failed: %w", err)
				}
				w.SetResult(domainResp)
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[BindInput]()
			},
		},
		{
			Name:  StateDomainDelegationSetup,
			Event: EventDomainDelegation,
			Handler: func(ctx context.Context, sess *session.Session, _ json.RawMessage) (string, error) {
				w := sess.State().(DomainWizardState)
				if w.Result() == nil {
					return "", fmt.Errorf("domain not bound yet")
				}
				// Fetch delegation requirements so the service confirms the binding is
				// resolvable; rendering is informational and omitted from the MCP flow.
				domainID := strconv.Itoa(int(w.Result().Id))
				if _, err := deps.WebsitesService.GetDomainDNSRequirements(ctx, w.WebsiteID(), domainID); err != nil {
					return "", fmt.Errorf("failed to fetch delegation requirements: %w", err)
				}
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[NoInput]()
			},
		},
		{
			Name:  StateDomainVerify,
			Event: EventDomainVerified,
			Handler: func(ctx context.Context, sess *session.Session, input json.RawMessage) (string, error) {
				w := sess.State().(DomainWizardState)
				var in DomainVerifyInput
				if len(input) > 0 && string(input) != "null" {
					if err := json.Unmarshal(input, &in); err != nil {
						return "", fmt.Errorf("invalid input: %w", err)
					}
				}
				_ = in // The retry flag is informational; verification always runs.

				if w.Result() == nil {
					return "", fmt.Errorf("domain not bound yet")
				}
				domainID := strconv.Itoa(int(w.Result().Id))
				verified, err := deps.WebsitesService.VerifyDomain(ctx, w.WebsiteID(), domainID)
				if err != nil {
					return "", fmt.Errorf("domain verification failed: %w", err)
				}
				// A nil verification response must not clobber the bound result.
				if verified != nil {
					w.SetResult(verified)
				}
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[DomainVerifyInput]()
			},
		},
	}
}

// --- Setup wizard step definitions ---

// RestoreRunner completes a vault restore on the host from a recovery
// mnemonic provided by the human (via a browser form, never through the MCP
// channel). It is implemented in pkg/cli (wrapping the shared restoreVault
// code) and injected so the MCP package can drive restores without importing
// the CLI package.
type RestoreRunner interface {
	// RestoreProfileName returns the profile that a pending restore targets.
	RestoreProfileName() string
	// RunRestore completes a restore for the given profile and mnemonic,
	// returning the restored vault ID. On a device that requires browser
	// approval, onApproval is called with the Sia approval URL before
	// RunRestore blocks waiting for that approval, so the caller can surface
	// the URL to the human (e.g. via an OOB status page).
	RunRestore(ctx context.Context, profile, mnemonic string, onApproval func(approvalURL string)) (string, error)
}

// CreateRunner provisions and activates a new vault on the host, symmetric with
// RestoreRunner: it GENERATES a fresh recovery seed, drives the Sia browser
// approval, registers + activates the vault, and returns the fresh seed
// host-side for a one-time seed_url. Implemented in internal/cli over the
// shared Provisioner.Create path so the MCP layer can drive agent-mode creates
// without importing the CLI package. The seed is returned for OOB delivery only
// and is never placed on the MCP channel.
type CreateRunner interface {
	// RunCreate provisions and activates a new vault for the given profile,
	// returning the active vault ID plus the freshly generated seed (host-side
	// presentation only, for a one-time seed_url) and its 0600 seed-file path.
	// On a device that requires browser approval, onApproval is called with the
	// Sia approval URL before RunCreate blocks waiting for that approval.
	RunCreate(ctx context.Context, profile string, onApproval func(approvalURL string)) (vaultID, seed, seedPath string, err error)
}

// SetupWizardDeps holds the dependencies needed to build and run the
// setup wizard session steps and OOB restore path.
type SetupWizardDeps struct {
	CfgMgr       config.Manager
	AuthService  auth.AuthService
	SetupFactory SetupWizardFactory
	// OutOfBand completes sign-in in a browser so credentials never transit
	// the MCP/LLM channel. It may be nil in tests that drive auth directly.
	OutOfBand *auth.OutOfBandLogin
	// Restore completes a vault restore from a human-supplied mnemonic via a
	// one-time browser form (never through the MCP channel). It may be nil in
	// tests that don't exercise OOB restore.
	Restore RestoreRunner
	// Create provisions + activates a new vault (generating a fresh seed) via a
	// one-time browser page (never through the MCP channel). It may be nil in
	// tests that don't exercise OOB create. Symmetric with Restore; the only
	// difference is seed origin (generated vs provided).
	Create CreateRunner
}

// buildSetupSteps returns the StepDef slice for the setup wizard.
func buildSetupSteps(deps SetupWizardDeps) []session.StepDef {
	return []session.StepDef{
		{
			Name:  StateSetupAuth,
			Event: EventSetupAuthDone,
			Handler: func(ctx context.Context, sess *session.Session, input json.RawMessage) (string, error) {
				var in SetupAuthInput
				if err := json.Unmarshal(input, &in); err != nil {
					return "", fmt.Errorf("invalid input: %w", err)
				}
				if !in.Choice.Valid() {
					return "", fmt.Errorf("invalid auth choice: %s", in.Choice)
				}
				if in.Choice == AuthChoiceSkip {
					return "", nil
				}
				if in.Choice == AuthChoiceCreate {
					return "", fmt.Errorf("account creation must be done at https://pinner.xyz/register, then sign in")
				}
				// Sign in is completed out-of-band in a browser so the password
				// and any OTP never transit the MCP/LLM channel.
				if deps.OutOfBand == nil {
					return "", fmt.Errorf("sign_in requires out-of-band login, which is unavailable in this configuration")
				}
				if in.Email == "" {
					return "", fmt.Errorf("email is required for sign_in")
				}
				url, done, loginErr := deps.OutOfBand.PendingOutcome(sess.ID, in.Email)
				if loginErr != nil {
					// The out-of-band login failed or expired. Restart it so the
					// user can retry, and keep the session on auth.
					var err error
					_, url, err = deps.OutOfBand.Begin(sess.ID, in.Email)
					if err != nil {
						return "", fmt.Errorf("failed to start out-of-band login: %w", err)
					}
					return "Out-of-band sign-in required. Ask the user to open this URL in their browser and complete sign-in: " + url + ". Then call setup_auth again with choice=\"sign_in\" to continue.", nil
				}
				if !done {
					if url == "" {
						// First request for this email: start the login.
						var err error
						_, url, err = deps.OutOfBand.Begin(sess.ID, in.Email)
						if err != nil {
							return "", fmt.Errorf("failed to start out-of-band login: %w", err)
						}
					}
					return "Out-of-band sign-in required. Ask the user to open this URL in their browser and complete sign-in: " + url + ". Then call setup_auth again with choice=\"sign_in\" to continue.", nil
				}
				// Credentials were verified in the browser; advance.
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[SetupAuthInput]()
			},
		},
		{
			Name:  StateSetupConfig,
			Event: EventSetupConfigDone,
			Handler: func(ctx context.Context, sess *session.Session, input json.RawMessage) (string, error) {
				var in SetupConfigInput
				if err := json.Unmarshal(input, &in); err != nil {
					return "", fmt.Errorf("invalid input: %w", err)
				}
				if !in.Choice.Valid() {
					return "", fmt.Errorf("invalid config choice: %s", in.Choice)
				}
				// The choice -> persistent CLI config write is shared (see
				// config_fields.go): the same keys the CLI config path uses.
				if err := applySetupConfig(deps.CfgMgr, in); err != nil {
					return "", err
				}
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[SetupConfigInput]()
			},
		},
		{
			Name:  StateSetupCompletion,
			Event: EventSetupCompDone,
			Handler: func(_ context.Context, _ *session.Session, input json.RawMessage) (string, error) {
				var in SetupCompletionInput
				if len(input) > 0 && string(input) != "null" {
					if err := json.Unmarshal(input, &in); err != nil {
						return "", fmt.Errorf("invalid input: %w", err)
					}
				}
				// Shell completion is informational for MCP agents.
				_ = in
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[SetupCompletionInput]()
			},
		},
		{
			Name:  StateSetupTutorial,
			Event: EventSetupTutDone,
			Handler: func(_ context.Context, _ *session.Session, _ json.RawMessage) (string, error) {
				return "", nil
			},
			Schema: func(_ *session.Session) *jsonschema.Schema {
				return toolargs.SchemaFor[NoInput]()
			},
		},
	}
}

// --- Session creation helpers ---

// NewWebsitesSession creates a new wizard session for the websites wizard,
// with the FSM and step definitions wired up. The returned session is
// stored in the given SessionStore.
func NewWebsitesSession(store *session.SessionStore, deps WebsitesWizardDeps) (*session.Session, error) {
	wizard := deps.WebsitesFactory()
	steps := buildWebsitesSteps(deps)
	fsmInst := fsm.NewFSM(StateWebsitesInit, websitesFSMEvents(), nil)
	sess, err := store.Create(wizard, fsmInst, steps)
	if err != nil {
		return nil, err
	}

	// Transition to the first step.
	if err := fsmInst.Event(context.Background(), EventWebsitesStart); err != nil {
		store.Delete(sess.ID)
		return nil, fmt.Errorf("failed to start wizard: %w", err)
	}

	return sess, nil
}

// NewSetupSession creates a new wizard session for the setup wizard.
func NewSetupSession(store *session.SessionStore, deps SetupWizardDeps) (*session.Session, error) {
	wizard := deps.SetupFactory()
	steps := buildSetupSteps(deps)
	fsmInst := fsm.NewFSM(StateSetupInit, setupFSMEvents(), nil)
	sess, err := store.Create(wizard, fsmInst, steps)
	if err != nil {
		return nil, err
	}

	if err := fsmInst.Event(context.Background(), EventSetupStart); err != nil {
		store.Delete(sess.ID)
		return nil, fmt.Errorf("failed to start wizard: %w", err)
	}

	return sess, nil
}

// NewDomainSession creates a new domain addition wizard session, with the
// FSM and step definitions wired up. The returned session is stored in the
// given SessionStore.
func NewDomainSession(store *session.SessionStore, deps DomainWizardDeps) (*session.Session, error) {
	wizard := deps.DomainFactory()
	steps := buildDomainSteps(deps)
	fsmInst := fsm.NewFSM(StateDomainInit, domainFSMEvents(), nil)
	sess, err := store.Create(wizard, fsmInst, steps)
	if err != nil {
		return nil, err
	}

	if err := fsmInst.Event(context.Background(), EventDomainStart); err != nil {
		store.Delete(sess.ID)
		return nil, fmt.Errorf("failed to start wizard: %w", err)
	}

	return sess, nil
}

// --- Response builder ---

// schemaRequiresInput reports whether a wizard step schema actually expects
// mandatory form fields. Steps with all-optional fields (e.g. ValidateInput's
// optional `retry`, DomainVerifyInput) auto-advance on the StepResponse path,
// so only a schema with at least one REQUIRED field triggers a native form
// elicitation. This preserves the invariant that auto-advancing steps never
// require a form.
func schemaRequiresInput(schema *jsonschema.Schema) bool {
	if schema == nil {
		return false
	}
	return len(schema.Required) > 0
}

// elicitForStep builds the native form elicitation for the current step,
// carrying the session id across the round-trip via a signed requestState.
// Returns nil if the step's schema cannot be encoded (caller falls back to
// StepResponse).
func elicitForStep(sessionID string, resp StepResponse, now time.Time) *model.ElicitationSpec {
	if !schemaRequiresInput(resp.NextStepSchema) {
		return nil
	}
	schema, err := json.Marshal(resp.NextStepSchema)
	if err != nil {
		return nil
	}
	// Sign the session id so the echoed requestState is integrity-protected
	// (and expires), per the 2026-07-28 spec's MUST on requestState.
	state, err := session.MintWizardRequestState(sessionID, now)
	if err != nil {
		return nil
	}
	return &model.ElicitationSpec{
		ID:           "input",
		Message:      fmt.Sprintf("Step '%s' needs input.", resp.CurrentStep),
		FormSchema:   json.RawMessage(schema),
		RequestState: state,
	}
}

// rePresentFormOnFailure re-emits the native form for the current step after a
// failed form retry, carrying the validation error in the message so the user
// can correct the submission instead of seeing a blank form. Returns nil when
// the step no longer needs input (caller falls back to StepResponse).
func rePresentFormOnFailure(sessionID string, resp StepResponse, cause error, now time.Time) *model.ElicitationSpec {
	spec := elicitForStep(sessionID, resp, now)
	if spec == nil {
		return nil
	}
	if cause != nil {
		spec.Message = fmt.Sprintf("Step '%s' needs input: %s", resp.CurrentStep, cause.Error())
	}
	return spec
}

// buildStepResponse constructs a StepResponse from the current session state.
func buildStepResponse(sess *session.Session) StepResponse {
	resp := StepResponse{
		SessionID:   sess.ID,
		CurrentStep: sess.FSM.Current(),
	}

	if sess.FSM.Current() == StateWebsitesComplete || sess.FSM.Current() == StateSetupComplete || sess.FSM.Current() == StateDomainComplete {
		resp.Complete = true
		resp.NextStep = ""
		return resp
	}

	// Look up the current step to get its schema.
	step, ok := sess.CurrentStep()
	if ok {
		resp.NextStep = step.Name
		if step.Schema != nil {
			resp.NextStepSchema = step.Schema(sess)
		}
	} else {
		// No step def for the current state: likely complete.
		resp.Complete = true
	}

	return resp
}

// --- MCP tool registration ---

func wizardEntry(name, description string, schema json.RawMessage, handler model.PinnerToolHandler) *model.ToolEntry {
	return &model.ToolEntry{Name: name, Description: description, Category: model.CategoryWizard, InputSchema: schema, Handler: handler}
}

func wizardStepSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"session_id":{"type":"string","description":"Wizard session ID returned by the wizard start tool"},"input":{"type":"object","description":"Step input matching the next_step_schema from the previous response"}},"required":["session_id"]}`)
}

// WizardStepInput is the typed argument shape for a wizard step tool. It is
// decoded from the request arguments via decodeToolArgs so handlers never cast
// map[string]any values by hand.
type WizardStepInput struct {
	// SessionID is the session handle returned by the wizard start tool.
	SessionID string `json:"session_id"`
	// Input is the raw step input (matching the current step's schema).
	Input json.RawMessage `json:"input"`
	// RequestState is the opaque session id echoed back on an input_required
	// elicitation retry, used when the client does not echo SessionID.
	RequestState string `json:"request_state"`
}

func marshalWizardResponse(resp StepResponse) (model.ToolResult, error) {
	raw, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return model.ToolResult{}, fmt.Errorf("failed to marshal step response: %w", err)
	}
	return model.ToolResult{IsError: resp.Error != "", Text: string(raw)}, nil
}

// toolAdder is the narrow slice of the hub's tool catalog the wizard needs to
// register its start/step tools. The hub's *ToolCatalog satisfies it.
type toolAdder interface {
	Add(entry *model.ToolEntry)
}

func registerWizardStart(catalog toolAdder, name, description string, start func() (*session.Session, error)) {
	catalog.Add(wizardEntry(name, description, json.RawMessage(`{"type":"object","properties":{}}`), func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
		sess, err := start()
		if err != nil {
			return marshalWizardResponse(StepResponse{Error: fmt.Sprintf("failed to create session: %v", err)})
		}
		return marshalWizardResponse(buildStepResponse(sess))
	}))
}

func registerWizardStep(catalog toolAdder, name, description string, store *session.SessionStore, completionMessage string) {
	catalog.Add(wizardEntry(name, description, wizardStepSchema(), func(ctx context.Context, req model.ToolRequest) (model.ToolResult, error) {
		in, err := toolargs.DecodeToolArgs[WizardStepInput](req)
		if err != nil {
			return marshalWizardResponse(StepResponse{Error: fmt.Sprintf("invalid step arguments: %v", err)})
		}
		sessionID := in.SessionID
		if sessionID == "" && in.RequestState != "" {
			// A retry after an input_required elicitation may not echo the
			// original arguments (only the form content); recover the session
			// from the signed requestState we set on the elicitation. Verify it
			// first so a tampered/forged token fails closed instead of being
			// used as a session id.
			verified, err := session.VerifyWizardRequestState(in.RequestState, time.Now())
			if err != nil {
				return marshalWizardResponse(StepResponse{Error: "session_id is required"})
			}
			sessionID = verified
		}
		if sessionID == "" {
			return marshalWizardResponse(StepResponse{Error: "session_id is required"})
		}
		sess, err := store.Get(sessionID)
		if err != nil {
			return marshalWizardResponse(StepResponse{SessionID: sessionID, Error: err.Error()})
		}
		input := in.Input
		if len(input) == 0 || string(input) == "null" {
			// Treat an absent or explicit-null input as the empty object so
			// AdvanceSession sees `{}` (a client sending `"input":null` must
			// not be differentiated from an omitted input).
			input = json.RawMessage(`{}`)
		}
		info, err := session.AdvanceSession(ctx, sess, input)
		if err != nil {
			resp := buildStepResponse(sess)
			resp.Error = err.Error()
			resp.Message = fmt.Sprintf("step '%s' failed the session remains in state '%s', you may retry", resp.CurrentStep, resp.CurrentStep)
			return marshalWizardResponse(resp)
		}
		resp := buildStepResponse(sess)
		if info != "" {
			// The step relayed informational output (e.g. an out-of-band login
			// URL) and is NOT complete. Surface it as a plain Message with no
			// error framing; the session stays on the current step.
			resp.Message = info
			return marshalWizardResponse(resp)
		}
		if resp.Complete {
			resp.Message = completionMessage
			return marshalWizardResponse(resp)
		}
		// Always return the in-band StepResponse carrying next_step_schema.
		// The wizard is driven by the agent passing JSON `input` to each step
		// call. The agent-facing templates (prompttemplates/*.tmpl) must match
		// this contract and must not instruct clients to wait for an
		// input_required form, which is never emitted.
		return marshalWizardResponse(resp)
	}))
}

func RegisterWizardTools(catalog toolAdder, store *session.SessionStore, wDeps WebsitesWizardDeps, sDeps SetupWizardDeps, dDeps DomainWizardDeps) error {
	registerWizardStart(catalog, "domains_wizard_start", "Start a new domain addition wizard session.", func() (*session.Session, error) { return NewDomainSession(store, dDeps) })
	registerWizardStep(catalog, "domains_wizard_step", "Advance a domain addition wizard session by one step.", store, "Domains wizard completed successfully.")
	registerWizardStart(catalog, "websites_wizard_start", "Start a new websites creation wizard session.", func() (*session.Session, error) { return NewWebsitesSession(store, wDeps) })
	registerWizardStep(catalog, "websites_wizard_step", "Advance a websites wizard session by one step.", store, "Websites wizard completed successfully.")
	registerWizardStart(catalog, "setup_wizard_start", "Start a new setup wizard session.", func() (*session.Session, error) { return NewSetupSession(store, sDeps) })
	registerWizardStep(catalog, "setup_wizard_step", "Advance a setup wizard session by one step.", store, "Setup wizard completed successfully.")
	return nil
}
