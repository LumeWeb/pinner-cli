package vault

import (
	"context"
	"fmt"

	"go.sia.tech/siastorage"
)

// Connection drives a single Sia connection-approval flow. It wraps one
// *siastorage.Builder for the entire Request -> WaitForApproval -> Register
// sequence. The SDK stores the pending request, ephemeral key, and approval
// status on the Builder instance itself, so Request and Wait/Register MUST run
// on the same builder; spawning a fresh builder for the wait (as the previous
// split helpers did) left registerResp nil and WaitForApproval failed with
// "no connection request", orphaning the browser approval.
type Connection struct {
	builder  *siastorage.Builder
	mnemonic string
}

// NewConnection creates a connection flow for the given indexer and mnemonic.
// The mnemonic is used to derive the app key during WaitAndRegister.
func NewConnection(indexerURL, mnemonic string) *Connection {
	return &Connection{
		builder:  siastorage.NewBuilder(indexerURL, appMetadata(indexerURL)),
		mnemonic: mnemonic,
	}
}

func appMetadata(indexerURL string) siastorage.AppMetadata {
	return siastorage.AppMetadata{
		ID:          AppID(),
		Name:        "Pinner CLI Vault",
		Description: "Private encrypted file storage via Sia",
		ServiceURL:  indexerURL,
	}
}

// Request issues the connection request on the shared builder and returns the
// URL the user must visit to approve. Call WaitAndRegister (on the same
// Connection) afterward to complete the flow.
func (c *Connection) Request(ctx context.Context) (string, error) {
	approvalURL, err := c.builder.RequestConnection(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to request connection: %w", err)
	}
	return approvalURL, nil
}

// WaitAndRegister waits for browser approval of the connection request and then
// registers the app key derived from the mnemonic. Returns the hex-encoded app
// key. It must be called on the same Connection that issued Request.
func (c *Connection) WaitAndRegister(ctx context.Context) (string, error) {
	if err := c.builder.WaitForApproval(ctx); err != nil {
		return "", fmt.Errorf("approval wait failed: %w", err)
	}
	sdk, err := c.builder.Register(ctx, c.mnemonic)
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
