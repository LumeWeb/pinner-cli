package vault

import (
	"context"
	"fmt"

	"go.sia.tech/core/types"
	"go.sia.tech/siastorage"
)

// RestoreConnection restores a Sia connection from a mnemonic.
// Returns the hex-encoded app key.
func RestoreConnection(ctx context.Context, indexerURL, mnemonic string) (string, error) {
	appID := AppID()
	metadata := siastorage.AppMetadata{
		ID:          appID,
		Name:        "Pinner CLI Vault",
		Description: "Private encrypted file storage via Sia",
		ServiceURL:  indexerURL,
	}

	builder := siastorage.NewBuilder(indexerURL, metadata)
	sdk, err := builder.Register(ctx, mnemonic)
	if err != nil {
		return "", fmt.Errorf("failed to register: %w", err)
	}
	defer sdk.Close()

	appKey := sdk.AppKey()
	return fmt.Sprintf("%x", []byte(appKey)), nil
}

// NewSeedPhrase generates a new random recovery mnemonic without issuing any
// network connection request. Used by agent-mode create, which defers the
// browser approval to restore.
func NewSeedPhrase() string {
	return siastorage.NewSeedPhrase()
}

// RequestNewConnection initiates a new Sia connection.
// Returns the approval URL and mnemonic.
func RequestNewConnection(ctx context.Context, indexerURL string) (string, string, error) {
	appID := AppID()
	mnemonic := siastorage.NewSeedPhrase()
	metadata := siastorage.AppMetadata{
		ID:          appID,
		Name:        "Pinner CLI Vault",
		Description: "Private encrypted file storage via Sia",
		ServiceURL:  indexerURL,
	}

	builder := siastorage.NewBuilder(indexerURL, metadata)
	approvalURL, err := builder.RequestConnection(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to request connection: %w", err)
	}

	return approvalURL, mnemonic, nil
}

// RequestConnectionOnly initiates a connection request without generating a new mnemonic.
// Used by restore — the mnemonic is provided by the user.
func RequestConnectionOnly(ctx context.Context, indexerURL string) (string, error) {
	appID := AppID()
	metadata := siastorage.AppMetadata{
		ID:          appID,
		Name:        "Pinner CLI Vault",
		Description: "Private encrypted file storage via Sia",
		ServiceURL:  indexerURL,
	}

	builder := siastorage.NewBuilder(indexerURL, metadata)
	approvalURL, err := builder.RequestConnection(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to request connection: %w", err)
	}

	return approvalURL, nil
}

// WaitForApprovalAndRegister waits for browser approval, then registers.
// Returns the hex-encoded app key.
func WaitForApprovalAndRegister(ctx context.Context, indexerURL, mnemonic string) (string, error) {
	appID := AppID()
	metadata := siastorage.AppMetadata{
		ID:          appID,
		Name:        "Pinner CLI Vault",
		Description: "Private encrypted file storage via Sia",
		ServiceURL:  indexerURL,
	}

	builder := siastorage.NewBuilder(indexerURL, metadata)
	if err := builder.WaitForApproval(ctx); err != nil {
		return "", fmt.Errorf("approval wait failed: %w", err)
	}

	sdk, err := builder.Register(ctx, mnemonic)
	if err != nil {
		return "", fmt.Errorf("failed to register: %w", err)
	}
	defer sdk.Close()

	appKey := sdk.AppKey()
	return fmt.Sprintf("%x", []byte(appKey)), nil
}

// noopSDK returns a minimal SDK for type compatibility.
var _ types.PrivateKey = types.PrivateKey{}
