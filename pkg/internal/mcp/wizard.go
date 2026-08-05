package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/invopop/jsonschema"
	"github.com/looplab/fsm"
	"github.com/mark3labs/mcp-go/mcp"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
	portalsdk "go.lumeweb.com/portal-sdk"
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

// DomainInput is the input for the domain step.
type DomainInput struct {
	Domain string `json:"domain" jsonschema:"description=The domain name for the website (e.g. example.com)"`
}

// DNSModeInput is the input for the dns_mode step.
type DNSModeInput struct {
	Mode DNSModeValue `json:"mode" jsonschema:"enum=managed,enum=self_managed,description=managed = Pinner manages DNS, self_managed = user configures DNS records"`
}

// CreateInput is the input for the create step (explicit confirmation).
type CreateInput struct {
	Confirm bool `json:"confirm" jsonschema:"description=Must be true to confirm website creation (irreversible operation)"`
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

// DomainNameInput is the input for the domain name step.
type DomainNameInput struct {
	Domain string `json:"domain" jsonschema:"description=The domain name to bind (e.g. mydomain or staging.example.com)"`
}

// NamespaceInput is the input for the namespace step.
type NamespaceInput struct {
	Namespace NamespaceValue `json:"namespace" jsonschema:"enum=icann,enum=hns,description=The domain namespace: icann (traditional) or hns (Handshake)"`
}

// BindInput is the input for the bind domain step (explicit confirmation).
type BindInput struct {
	Confirm bool `json:"confirm" jsonschema:"description=Must be true to confirm binding the domain (irreversible operation)"`
}

// DomainVerifyInput is the input for the domain verify step.
type DomainVerifyInput struct {
	Retry bool `json:"retry,omitempty" jsonschema:"description=Set to true to retry verification (useful when DNS propagation is pending)"`
}

// SetupAuthInput is the input for the setup auth step.
type SetupAuthInput struct {
	Choice   AuthStepChoiceValue `json:"choice" jsonschema:"enum=create_account,enum=sign_in,enum=skip,description=Whether to create a new account, sign in, or skip auth"`
	Email    string              `json:"email,omitempty" jsonschema:"description=Email address (required for create_account and sign_in)"`
	Password string              `json:"password,omitempty" jsonschema:"description=Password (required for create_account and sign_in)"`
	OTPCode  string              `json:"otp_code,omitempty" jsonschema:"description=OTP code if 2FA is enabled during sign_in"`
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
func BuildStepResponseForTest(sess *Session) StepResponse {
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

// --- JSON schema helpers ---

// schemaReflector reflects Go struct types into JSON schemas using
// struct tags for field descriptions, enums, and constraints.
var schemaReflector = &jsonschema.Reflector{
	DoNotReference: true,
	Anonymous:      true,
}

// schemaFor returns a JSON schema describing the expected input shape for
// a wizard step, derived from the struct type T via reflection. Struct
// fields use jsonschema tags (enum, description, required) to control
// the emitted schema.
func schemaFor[T any]() *jsonschema.Schema {
	var v T
	return schemaReflector.ReflectFromType(reflect.TypeOf(v))
}

// emptySchema is the schema for steps that take no input.
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

// buildWebsitesSteps returns the StepDef slice for the websites wizard.
// Each step's handler decodes JSON input, validates it, and mutates the
// WebsitesWizardState state stored in the session.
func buildWebsitesSteps(deps WebsitesWizardDeps) []StepDef {
	return []StepDef{
		{
			Name:  StateWebsitesAuthCheck,
			Event: EventWebsitesAuthOK,
			Handler: func(ctx context.Context, sess *Session, _ json.RawMessage) error {
				if deps.CfgMgr.Config().AuthToken == "" {
					return fmt.Errorf("authentication required: run 'pinner auth' or set --auth-token")
				}
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[NoInput]()
			},
		},
		{
			Name:  StateWebsitesContentSource,
			Event: EventWebsitesContent,
			Handler: func(ctx context.Context, sess *Session, input json.RawMessage) error {
				w := sess.State().(WebsitesWizardState)
				var in ContentSourceInput
				if err := json.Unmarshal(input, &in); err != nil {
					return fmt.Errorf("invalid input: %w", err)
				}
				if !in.Choice.Valid() {
					return fmt.Errorf("invalid choice: %s (expected \"cid\" or \"upload\")", in.Choice)
				}
				if in.Choice == ContentSourceUpload {
					return fmt.Errorf("content upload required: run 'pinner upload <file>' first, then restart the wizard")
				}
				if in.CID == "" {
					return fmt.Errorf("cid cannot be empty when choice is \"cid\"")
				}
				w.SetCID(in.CID)
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[ContentSourceInput]()
			},
		},
		{
			Name:  StateWebsitesTargetType,
			Event: EventWebsitesTarget,
			Handler: func(ctx context.Context, sess *Session, input json.RawMessage) error {
				w := sess.State().(WebsitesWizardState)
				var in TargetTypeInput
				if err := json.Unmarshal(input, &in); err != nil {
					return fmt.Errorf("invalid input: %w", err)
				}
				if !in.Type.Valid() {
					return fmt.Errorf("invalid target type: %s (expected \"ipfs\" or \"ipns\")", in.Type)
				}
				w.SetTargetType(string(in.Type))
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[TargetTypeInput]()
			},
		},
		{
			Name:  StateWebsitesDomain,
			Event: EventWebsitesDomainSet,
			Handler: func(ctx context.Context, sess *Session, input json.RawMessage) error {
				w := sess.State().(WebsitesWizardState)
				var in DomainInput
				if err := json.Unmarshal(input, &in); err != nil {
					return fmt.Errorf("invalid input: %w", err)
				}
				if in.Domain == "" {
					return fmt.Errorf("domain cannot be empty")
				}
				w.SetDomain(in.Domain)
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[DomainInput]()
			},
		},
		{
			Name:  StateWebsitesDNSMode,
			Event: EventWebsitesDNSMode,
			Handler: func(ctx context.Context, sess *Session, input json.RawMessage) error {
				w := sess.State().(WebsitesWizardState)
				var in DNSModeInput
				if err := json.Unmarshal(input, &in); err != nil {
					return fmt.Errorf("invalid input: %w", err)
				}
				if !in.Mode.Valid() {
					return fmt.Errorf("invalid DNS mode: %s (expected \"managed\" or \"self_managed\")", in.Mode)
				}
				w.SetDNSHosting(in.Mode == DNSModeManaged)
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[DNSModeInput]()
			},
		},
		{
			Name:  StateWebsitesCreate,
			Event: EventWebsitesCreated,
			Handler: func(ctx context.Context, sess *Session, input json.RawMessage) error {
				w := sess.State().(WebsitesWizardState)
				var in CreateInput
				if err := json.Unmarshal(input, &in); err != nil {
					return fmt.Errorf("invalid input: %w", err)
				}
				if !in.Confirm {
					return fmt.Errorf("confirmation required: set confirm=true to create the website")
				}

				// Call WebsitesService directly; the CLI wizard's create method is unexported.
				targetType := w.TargetType()
				if targetType == "" {
					targetType = string(TargetTypeIPFS)
				}
				dnsHosting := w.DNSHosting()
				req := ipfs.WebsiteRequest{
					Domain:            w.Domain(),
					TargetHash:        w.CID(),
					TargetType:        targetType,
					DnsHostingEnabled: &dnsHosting,
				}
				website, err := deps.WebsitesService.CreateWithOptions(ctx, req)
				if err != nil {
					return fmt.Errorf("website creation failed: %w", err)
				}
				w.SetWebsite(website)
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[CreateInput]()
			},
		},
		{
			Name:  StateWebsitesDNSSetup,
			Event: EventWebsitesDNSSet,
			Handler: func(_ context.Context, _ *Session, _ json.RawMessage) error {
				// DNS setup is informational; no state mutation needed.
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[NoInput]()
			},
		},
		{
			Name:  StateWebsitesValidate,
			Event: EventWebsitesValidated,
			Handler: func(ctx context.Context, sess *Session, input json.RawMessage) error {
				w := sess.State().(WebsitesWizardState)
				var in ValidateInput
				if len(input) > 0 && string(input) != "null" {
					if err := json.Unmarshal(input, &in); err != nil {
						return fmt.Errorf("invalid input: %w", err)
					}
				}
				_ = in // The retry flag is informational; validation always runs.

				website := w.Website()
				if website == nil {
					return fmt.Errorf("website not created yet")
				}

				// Call WebsitesService directly; the CLI wizard's validate method is unexported.
				id := fmt.Sprintf("%d", website.Id)
				result, err := deps.WebsitesService.Validate(ctx, id)
				if err != nil {
					return fmt.Errorf("validation failed: %w", err)
				}
				w.SetValidationResult(result)
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[ValidateInput]()
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
func buildDomainSteps(deps DomainWizardDeps) []StepDef {
	return []StepDef{
		{
			Name:  StateDomainAuthCheck,
			Event: EventDomainAuthOK,
			Handler: func(ctx context.Context, sess *Session, _ json.RawMessage) error {
				if deps.CfgMgr.Config().AuthToken == "" {
					return fmt.Errorf("authentication required: run 'pinner auth' or set --auth-token")
				}
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[NoInput]()
			},
		},
		{
			Name:  StateDomainWebsite,
			Event: EventDomainWebsite,
			Handler: func(ctx context.Context, sess *Session, input json.RawMessage) error {
				w := sess.State().(DomainWizardState)
				var in WebsiteInput
				if err := json.Unmarshal(input, &in); err != nil {
					return fmt.Errorf("invalid input: %w", err)
				}
				if in.WebsiteID == "" {
					return fmt.Errorf("website_id cannot be empty")
				}

				websites, err := deps.WebsitesService.List(ctx)
				if err != nil {
					return fmt.Errorf("failed to load websites: %w", err)
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
					return fmt.Errorf("website with ID %q not found; list available websites with 'pinner websites list'", in.WebsiteID)
				}
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[WebsiteInput]()
			},
		},
		{
			Name:  StateDomainName,
			Event: EventDomainName,
			Handler: func(_ context.Context, sess *Session, input json.RawMessage) error {
				w := sess.State().(DomainWizardState)
				var in DomainNameInput
				if err := json.Unmarshal(input, &in); err != nil {
					return fmt.Errorf("invalid input: %w", err)
				}
				if in.Domain == "" {
					return fmt.Errorf("domain cannot be empty")
				}
				w.SetDomain(in.Domain)
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[DomainNameInput]()
			},
		},
		{
			Name:  StateDomainNamespace,
			Event: EventDomainNamespace,
			Handler: func(_ context.Context, sess *Session, input json.RawMessage) error {
				w := sess.State().(DomainWizardState)
				var in NamespaceInput
				if err := json.Unmarshal(input, &in); err != nil {
					return fmt.Errorf("invalid input: %w", err)
				}
				if !in.Namespace.Valid() {
					return fmt.Errorf("invalid namespace: %s (expected \"icann\" or \"hns\")", in.Namespace)
				}
				w.SetNamespace(string(in.Namespace))
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[NamespaceInput]()
			},
		},
		{
			Name:  StateDomainBind,
			Event: EventDomainBound,
			Handler: func(ctx context.Context, sess *Session, input json.RawMessage) error {
				w := sess.State().(DomainWizardState)
				var in BindInput
				if err := json.Unmarshal(input, &in); err != nil {
					return fmt.Errorf("invalid input: %w", err)
				}
				if !in.Confirm {
					return fmt.Errorf("confirmation required: set confirm=true to bind the domain")
				}
				if w.Domain() == "" {
					return fmt.Errorf("domain name not set")
				}
				if w.Namespace() == "" {
					return fmt.Errorf("namespace not set")
				}

				req := ipfs.DomainRequest{
					Domain:    w.Domain(),
					Namespace: w.Namespace(),
				}
				domainResp, err := deps.WebsitesService.BindDomain(ctx, w.WebsiteID(), req)
				if err != nil {
					return fmt.Errorf("domain binding failed: %w", err)
				}
				w.SetResult(domainResp)
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[BindInput]()
			},
		},
		{
			Name:  StateDomainDelegationSetup,
			Event: EventDomainDelegation,
			Handler: func(ctx context.Context, sess *Session, _ json.RawMessage) error {
				w := sess.State().(DomainWizardState)
				if w.Result() == nil {
					return fmt.Errorf("domain not bound yet")
				}
				// Fetch delegation requirements so the service confirms the binding is
				// resolvable; rendering is informational and omitted from the MCP flow.
				domainID := strconv.Itoa(int(w.Result().Id))
				if _, err := deps.WebsitesService.GetDomainDNSRequirements(ctx, w.WebsiteID(), domainID); err != nil {
					return fmt.Errorf("failed to fetch delegation requirements: %w", err)
				}
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[NoInput]()
			},
		},
		{
			Name:  StateDomainVerify,
			Event: EventDomainVerified,
			Handler: func(ctx context.Context, sess *Session, input json.RawMessage) error {
				w := sess.State().(DomainWizardState)
				var in DomainVerifyInput
				if len(input) > 0 && string(input) != "null" {
					if err := json.Unmarshal(input, &in); err != nil {
						return fmt.Errorf("invalid input: %w", err)
					}
				}
				_ = in // The retry flag is informational; verification always runs.

				if w.Result() == nil {
					return fmt.Errorf("domain not bound yet")
				}
				domainID := strconv.Itoa(int(w.Result().Id))
				verified, err := deps.WebsitesService.VerifyDomain(ctx, w.WebsiteID(), domainID)
				if err != nil {
					return fmt.Errorf("domain verification failed: %w", err)
				}
				w.SetResult(verified)
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[DomainVerifyInput]()
			},
		},
	}
}

// --- Setup wizard step definitions ---

// SetupWizardDeps holds the dependencies needed to build and run the
// setup wizard session steps.
type SetupWizardDeps struct {
	CfgMgr       config.Manager
	AuthService  AuthService
	SetupFactory SetupWizardFactory
}

// buildSetupSteps returns the StepDef slice for the setup wizard.
func buildSetupSteps(deps SetupWizardDeps) []StepDef {
	return []StepDef{
		{
			Name:  StateSetupAuth,
			Event: EventSetupAuthDone,
			Handler: func(ctx context.Context, sess *Session, input json.RawMessage) error {
				var in SetupAuthInput
				if err := json.Unmarshal(input, &in); err != nil {
					return fmt.Errorf("invalid input: %w", err)
				}
				if !in.Choice.Valid() {
					return fmt.Errorf("invalid auth choice: %s", in.Choice)
				}
				if in.Choice == AuthChoiceSkip {
					return nil
				}
				if in.Choice == AuthChoiceCreate {
					return fmt.Errorf("account creation must be done at https://pinner.xyz/register, then sign in")
				}
				// Sign in
				if in.Email == "" || in.Password == "" {
					return fmt.Errorf("email and password are required for sign_in")
				}
				loginResult, err := deps.AuthService.LoginCheck(ctx, in.Email, in.Password)
				if err != nil {
					return fmt.Errorf("authentication failed: %w", err)
				}
				if loginResult.OTPRequired {
					if in.OTPCode == "" {
						return fmt.Errorf("OTP code required: provide otp_code in the input")
					}
					err = deps.AuthService.LoginWithOTP(ctx, loginResult.IntermediateJWT, in.OTPCode, "mcp-generated", false)
					if err != nil {
						return fmt.Errorf("OTP authentication failed: %w", err)
					}
				} else {
					err = deps.AuthService.CompleteLogin(ctx, loginResult.Token, "mcp-generated", false)
					if err != nil {
						return fmt.Errorf("login completion failed: %w", err)
					}
				}
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[SetupAuthInput]()
			},
		},
		{
			Name:  StateSetupConfig,
			Event: EventSetupConfigDone,
			Handler: func(ctx context.Context, sess *Session, input json.RawMessage) error {
				var in SetupConfigInput
				if err := json.Unmarshal(input, &in); err != nil {
					return fmt.Errorf("invalid input: %w", err)
				}
				if !in.Choice.Valid() {
					return fmt.Errorf("invalid config choice: %s", in.Choice)
				}
				switch in.Choice {
				case ConfigChoiceDefaults:
					if err := deps.CfgMgr.SetBaseEndpoint(""); err != nil {
						return fmt.Errorf("failed to reset endpoint: %w", err)
					}
					if err := deps.CfgMgr.SetSecure(true); err != nil {
						return fmt.Errorf("failed to set secure: %w", err)
					}
				case ConfigChoiceSkip:
					// Skip — preserve existing configuration.
				case ConfigChoiceCustom:
					if in.Endpoint == "" {
						return fmt.Errorf("endpoint is required for custom_endpoint choice")
					}
					if err := deps.CfgMgr.SetBaseEndpoint(in.Endpoint); err != nil {
						return fmt.Errorf("failed to set endpoint: %w", err)
					}
					if err := deps.CfgMgr.SetSecure(in.Secure); err != nil {
						return fmt.Errorf("failed to set secure: %w", err)
					}
				}
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[SetupConfigInput]()
			},
		},
		{
			Name:  StateSetupCompletion,
			Event: EventSetupCompDone,
			Handler: func(_ context.Context, _ *Session, input json.RawMessage) error {
				var in SetupCompletionInput
				if len(input) > 0 && string(input) != "null" {
					if err := json.Unmarshal(input, &in); err != nil {
						return fmt.Errorf("invalid input: %w", err)
					}
				}
				// Shell completion is informational for MCP agents.
				_ = in
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[SetupCompletionInput]()
			},
		},
		{
			Name:  StateSetupTutorial,
			Event: EventSetupTutDone,
			Handler: func(_ context.Context, _ *Session, _ json.RawMessage) error {
				return nil
			},
			Schema: func(_ *Session) *jsonschema.Schema {
				return schemaFor[NoInput]()
			},
		},
	}
}

// --- Session creation helpers ---

// NewWebsitesSession creates a new wizard session for the websites wizard,
// with the FSM and step definitions wired up. The returned session is
// stored in the given SessionStore.
func NewWebsitesSession(store *SessionStore, deps WebsitesWizardDeps) (*Session, error) {
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
func NewSetupSession(store *SessionStore, deps SetupWizardDeps) (*Session, error) {
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
func NewDomainSession(store *SessionStore, deps DomainWizardDeps) (*Session, error) {
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

// buildStepResponse constructs a StepResponse from the current session state.
func buildStepResponse(sess *Session) StepResponse {
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
		// No step def for the current state — likely complete.
		resp.Complete = true
	}

	return resp
}

// --- MCP tool registration ---

// RegisterWizardTools registers the websites_wizard_start, websites_wizard_step,
// setup_wizard_start, and setup_wizard_step MCP tools on the given server.
// The session store is shared between start and step tools.
// toolToEntry converts an mcp.Tool and its handler into a ToolEntry suitable
// for adding to the catalog. The tool's input schema is marshaled to JSON
// for storage.
func toolToEntry(name, description string, category ToolCategory, tool mcp.Tool, handler ToolHandler) (*ToolEntry, error) {
	schemaBytes, err := json.Marshal(tool.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input schema for %s: %w", name, err)
	}
	return &ToolEntry{
		Name:        name,
		Description: description,
		Category:    category,
		InputSchema: schemaBytes,
		Handler:     handler,
	}, nil
}

func RegisterWizardTools(catalog *ToolCatalog, store *SessionStore, wDeps WebsitesWizardDeps, sDeps SetupWizardDeps, dDeps DomainWizardDeps) error {
	if err := registerWebsitesWizardTools(catalog, store, wDeps); err != nil {
		return fmt.Errorf("failed to register websites wizard tools: %w", err)
	}
	if err := registerSetupWizardTools(catalog, store, sDeps); err != nil {
		return fmt.Errorf("failed to register setup wizard tools: %w", err)
	}
	if err := registerDomainWizardTools(catalog, store, dDeps); err != nil {
		return fmt.Errorf("failed to register domain wizard tools: %w", err)
	}
	return nil
}

// registerDomainWizardTools registers the domain wizard start and step tools.
func registerDomainWizardTools(catalog *ToolCatalog, store *SessionStore, deps DomainWizardDeps) error {
	startTool := mcp.NewTool("domains_wizard_start",
		mcp.WithDescription("Start a new domain addition wizard session. Returns a session_id "+
			"and the first step to complete (auth_check). No arguments required."),
	)
	startHandler := func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess, err := NewDomainSession(store, deps)
		if err != nil {
			return errorStepResult("", fmt.Sprintf("failed to create session: %v", err)), nil
		}
		resp := buildStepResponse(sess)
		return marshalStepResult(resp)
	}
	entry, err := toolToEntry("domains_wizard_start",
		"Start a new domain addition wizard session. Returns a session_id "+
			"and the first step to complete (auth_check). No arguments required.",
		CategoryWizard, startTool, startHandler)
	if err != nil {
		return fmt.Errorf("failed to create catalog entry for domains_wizard_start: %w", err)
	}
	catalog.Add(entry)

	stepTool := mcp.NewTool("domains_wizard_step",
		mcp.WithDescription("Advance a domain addition wizard session by one step. Provide the session_id "+
			"from domains_wizard_start and the input matching the next_step_schema returned by "+
			"the previous step."),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description("Wizard session ID returned by domains_wizard_start"),
		),
		mcp.WithObject("input",
			mcp.Description("Step input matching the next_step_schema from the previous response"),
		),
	)
	stepHandler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return errorStepResult("", "session_id is required"), nil
		}

		sess, err := store.Get(sessionID)
		if err != nil {
			return errorStepResult(sessionID, err.Error()), nil
		}

		var input json.RawMessage
		if raw, ok := args["input"]; ok && raw != nil {
			input, err = json.Marshal(raw)
			if err != nil {
				return errorStepResult(sessionID, fmt.Sprintf("failed to encode input: %v", err)), nil
			}
		}
		if input == nil {
			input = json.RawMessage(`{}`)
		}

		if err := AdvanceSession(ctx, sess, input); err != nil {
			resp := buildStepResponse(sess)
			resp.Error = err.Error()
			resp.Message = fmt.Sprintf("step '%s' failed the session remains in state '%s', you may retry",
				resp.CurrentStep, resp.CurrentStep)
			return marshalStepResult(resp)
		}

		resp := buildStepResponse(sess)
		if resp.Complete {
			resp.Message = "Domains wizard completed successfully."
		}
		return marshalStepResult(resp)
	}
	entry, err = toolToEntry("domains_wizard_step",
		"Advance a domain addition wizard session by one step. Provide the session_id "+
			"from domains_wizard_start and the input matching the next_step_schema returned by "+
			"the previous step.",
		CategoryWizard, stepTool, stepHandler)
	if err != nil {
		return fmt.Errorf("failed to create catalog entry for domains_wizard_step: %w", err)
	}
	catalog.Add(entry)

	return nil
}

// registerWebsitesWizardTools registers the websites wizard start and step tools.
func registerWebsitesWizardTools(catalog *ToolCatalog, store *SessionStore, deps WebsitesWizardDeps) error {
	startTool := mcp.NewTool("websites_wizard_start",
		mcp.WithDescription("Start a new websites creation wizard session. Returns a session_id "+
			"and the first step to complete (auth_check). No arguments required."),
	)
	startHandler := func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess, err := NewWebsitesSession(store, deps)
		if err != nil {
			return errorStepResult("", fmt.Sprintf("failed to create session: %v", err)), nil
		}
		resp := buildStepResponse(sess)
		return marshalStepResult(resp)
	}
	entry, err := toolToEntry("websites_wizard_start",
		"Start a new websites creation wizard session. Returns a session_id "+
			"and the first step to complete (auth_check). No arguments required.",
		CategoryWizard, startTool, startHandler)
	if err != nil {
		return fmt.Errorf("failed to create catalog entry for websites_wizard_start: %w", err)
	}
	catalog.Add(entry)

	stepTool := mcp.NewTool("websites_wizard_step",
		mcp.WithDescription("Advance a websites wizard session by one step. Provide the session_id "+
			"from websites_wizard_start and the input matching the next_step_schema returned by "+
			"the previous step."),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description("Wizard session ID returned by websites_wizard_start"),
		),
		mcp.WithObject("input",
			mcp.Description("Step input matching the next_step_schema from the previous response"),
		),
	)
	stepHandler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return errorStepResult("", "session_id is required"), nil
		}

		sess, err := store.Get(sessionID)
		if err != nil {
			return errorStepResult(sessionID, err.Error()), nil
		}

		// Extract the input as raw JSON.
		var input json.RawMessage
		if raw, ok := args["input"]; ok && raw != nil {
			input, err = json.Marshal(raw)
			if err != nil {
				return errorStepResult(sessionID, fmt.Sprintf("failed to encode input: %v", err)), nil
			}
		}
		if input == nil {
			input = json.RawMessage(`{}`)
		}

		// Advance the session: handler validates input, then FSM transitions.
		if err := AdvanceSession(ctx, sess, input); err != nil {
			// Return the error but include the current state for retry.
			resp := buildStepResponse(sess)
			resp.Error = err.Error()
			resp.Message = fmt.Sprintf("step '%s' failed the session remains in state '%s', you may retry",
				resp.CurrentStep, resp.CurrentStep)
			return marshalStepResult(resp)
		}

		resp := buildStepResponse(sess)
		if resp.Complete {
			resp.Message = "Websites wizard completed successfully."
		}
		return marshalStepResult(resp)
	}
	entry, err = toolToEntry("websites_wizard_step",
		"Advance a websites wizard session by one step. Provide the session_id "+
			"from websites_wizard_start and the input matching the next_step_schema returned by "+
			"the previous step.",
		CategoryWizard, stepTool, stepHandler)
	if err != nil {
		return fmt.Errorf("failed to create catalog entry for websites_wizard_step: %w", err)
	}
	catalog.Add(entry)

	return nil
}

// registerSetupWizardTools registers the setup wizard start and step tools.
func registerSetupWizardTools(catalog *ToolCatalog, store *SessionStore, deps SetupWizardDeps) error {
	startTool := mcp.NewTool("setup_wizard_start",
		mcp.WithDescription("Start a new setup wizard session. Returns a session_id "+
			"and the first step to complete (auth). No arguments required."),
	)
	startHandler := func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sess, err := NewSetupSession(store, deps)
		if err != nil {
			return errorStepResult("", fmt.Sprintf("failed to create session: %v", err)), nil
		}
		resp := buildStepResponse(sess)
		return marshalStepResult(resp)
	}
	entry, err := toolToEntry("setup_wizard_start",
		"Start a new setup wizard session. Returns a session_id "+
			"and the first step to complete (auth). No arguments required.",
		CategoryWizard, startTool, startHandler)
	if err != nil {
		return fmt.Errorf("failed to create catalog entry for setup_wizard_start: %w", err)
	}
	catalog.Add(entry)

	stepTool := mcp.NewTool("setup_wizard_step",
		mcp.WithDescription("Advance a setup wizard session by one step. Provide the session_id "+
			"from setup_wizard_start and the input matching the next_step_schema returned by "+
			"the previous step."),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description("Wizard session ID returned by setup_wizard_start"),
		),
		mcp.WithObject("input",
			mcp.Description("Step input matching the next_step_schema from the previous response"),
		),
	)
	stepHandler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return errorStepResult("", "session_id is required"), nil
		}

		sess, err := store.Get(sessionID)
		if err != nil {
			return errorStepResult(sessionID, err.Error()), nil
		}

		var input json.RawMessage
		if raw, ok := args["input"]; ok && raw != nil {
			input, err = json.Marshal(raw)
			if err != nil {
				return errorStepResult(sessionID, fmt.Sprintf("failed to encode input: %v", err)), nil
			}
		}
		if input == nil {
			input = json.RawMessage(`{}`)
		}

		if err := AdvanceSession(ctx, sess, input); err != nil {
			resp := buildStepResponse(sess)
			resp.Error = err.Error()
			resp.Message = fmt.Sprintf("step '%s' failed the session remains in state '%s', you may retry",
				resp.CurrentStep, resp.CurrentStep)
			return marshalStepResult(resp)
		}

		resp := buildStepResponse(sess)
		if resp.Complete {
			resp.Message = "Setup wizard completed successfully."
		}
		return marshalStepResult(resp)
	}
	entry, err = toolToEntry("setup_wizard_step",
		"Advance a setup wizard session by one step. Provide the session_id "+
			"from setup_wizard_start and the input matching the next_step_schema returned by "+
			"the previous step.",
		CategoryWizard, stepTool, stepHandler)
	if err != nil {
		return fmt.Errorf("failed to create catalog entry for setup_wizard_step: %w", err)
	}
	catalog.Add(entry)

	return nil
}

// --- Result helpers ---

// marshalStepResult serializes a StepResponse into an MCP CallToolResult.
func marshalStepResult(resp StepResponse) (*mcp.CallToolResult, error) {
	raw, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal step response: %w", err)
	}
	isError := resp.Error != ""
	return &mcp.CallToolResult{
		IsError: isError,
		Content: []mcp.Content{mcp.NewTextContent(string(raw))},
	}, nil
}

// errorStepResult builds an error CallToolResult for a failed step.
func errorStepResult(sessionID, msg string) *mcp.CallToolResult {
	resp := StepResponse{
		SessionID: sessionID,
		Error:     msg,
	}
	raw, _ := json.MarshalIndent(resp, "", "  ")
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{mcp.NewTextContent(string(raw))},
	}
}

// Compile-time interface checks.
var (
	_ portalsdk.AccountAPI = (portalsdk.AccountAPI)(nil)
)
