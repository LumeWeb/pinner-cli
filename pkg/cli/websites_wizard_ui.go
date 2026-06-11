package cli

import (
	"context"

	"go.lumeweb.com/pinner-cli/pkg/cli/wizard"
)

// WebsitesUI defines the interface for websites wizard UI interactions.
type WebsitesUI interface {
	wizard.UI
	SelectPrompter
	ContinuePrompter
	Spinner

	// Step execution
	ExecuteAuthCheckStep(ctx context.Context, w *WebsitesWizard) error
	ExecuteContentSourceStep(ctx context.Context, w *WebsitesWizard) error
	ExecuteTargetTypeStep(ctx context.Context, w *WebsitesWizard) error
	ExecuteDomainStep(ctx context.Context, w *WebsitesWizard) error
	ExecuteDNSModeStep(ctx context.Context, w *WebsitesWizard) error
	ExecuteCreateWebsiteStep(ctx context.Context, w *WebsitesWizard) error
	ExecuteValidateStep(ctx context.Context, w *WebsitesWizard) error
}

// ContentSourceChoice represents user's choice for the content source step.
type ContentSourceChoice int

const (
	ContentChoiceCID  ContentSourceChoice = iota // User has a CID
	ContentChoiceExit                             // User needs to upload first
)

// DNSModeChoice represents user's choice for the DNS mode step.
type DNSModeChoice int

const (
	DNSModeSelfManaged  DNSModeChoice = iota // User manages DNS themselves
	DNSModePinnerManaged                     // Pinner manages DNS
)

// TargetTypeChoice represents user's choice for the target type step.
type TargetTypeChoice int

const (
	TargetTypeIPFS TargetTypeChoice = iota // IPFS content addressing (default)
	TargetTypeIPNS                          // IPNS mutable name
)
