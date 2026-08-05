package cli

import (
	"context"

	"go.lumeweb.com/pinner-cli/pkg/cli/wizard"
)

// DomainsUI defines the interface for domain wizard UI interactions.
type DomainsUI interface {
	wizard.UI
	SelectPrompter
	ContinuePrompter
	Spinner

	// Step execution
	ExecuteAuthCheckStep(ctx context.Context, w *DomainAddWizard) error
	ExecuteWebsiteStep(ctx context.Context, w *DomainAddWizard) error
	ExecuteDomainStep(ctx context.Context, w *DomainAddWizard) error
	ExecuteNamespaceStep(ctx context.Context, w *DomainAddWizard) error
	ExecuteBindDomainStep(ctx context.Context, w *DomainAddWizard) error
	ExecuteDelegationSetupStep(ctx context.Context, w *DomainAddWizard) error
	ExecuteVerifyStep(ctx context.Context, w *DomainAddWizard) error
}

// DomainNamespaceChoice represents user's choice for the namespace step.
type DomainNamespaceChoice int

const (
	DomainNamespaceICANNChoice DomainNamespaceChoice = iota // ICANN-managed domain
	DomainNamespaceHNSChoice                                // Handshake naming system domain
)

// DomainVerifyChoice represents the user's choice after a validation attempt.
type DomainVerifyChoice int

const (
	DomainVerifyDone  DomainVerifyChoice = iota // Validation successful (or give up)
	DomainVerifyRetry                           // Ask the wizard to retry validation
)
