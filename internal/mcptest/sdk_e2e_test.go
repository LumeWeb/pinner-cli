package mcptest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	ipfs "go.lumeweb.com/ipfs-sdk"
	portalsdk "go.lumeweb.com/portal-sdk"
)

// The point of these tests: a server generated from the SAME swagger the SDKs
// consume must be interoperable with the SDKs' real generated clients. If the
// fake server is faithful to the contract, the official clients work against
// it unchanged — which is what makes it a usable upstream double for the
// CLI/MCP end-to-end tests.

func TestPortalSDKAccountClientAgainstFake(t *testing.T) {
	ts := New().Start()
	defer ts.Close()

	api := portalsdk.NewClient(
		portalsdk.WithEndpoint(ts.URL),
	)

	err := api.Register(context.Background(), "e2e@example.com", "E2E", "Test", "hunter2")
	require.NoError(t, err, "register via portal-sdk against swagger-generated fake")

	// Login with the same creds returns a token — proving auth round-trips.
	res, err := api.Login(context.Background(), "e2e@example.com", "hunter2")
	require.NoError(t, err, "login via portal-sdk against swagger-generated fake")
	require.NotEmpty(t, res.Token, "expected a returned token")
}

func TestIPFSSDKClientAgainstFake(t *testing.T) {
	ts := New().Start()
	defer ts.Close()

	client, err := ipfs.NewClient(ts.URL, "token-e2e@example.com")
	require.NoError(t, err, "construct ipfs-sdk client")

	pins, err := client.Pinning().ListPins(context.Background())
	require.NoError(t, err, "list pins via ipfs-sdk against swagger-generated fake")
	require.Len(t, pins, 0, "expected empty pin list")
}
