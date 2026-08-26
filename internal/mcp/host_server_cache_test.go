package mcp

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
	"go.lumeweb.com/pinner-cli/internal/mcp/vault"
)

// hostRequest builds an HTTP request carrying the given User-Agent. The
// profile-aware cache's detector reads headers to resolve a host profile.
func hostRequest(t *testing.T, userAgent string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "/mcp", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", userAgent)
	return req
}

// TestHostServerUploadVaultPresentation proves the profile-aware resolution
// behind the per-host upload_file / vault_put_file presentation: an
// OpenAI-over-HTTP host (FeatFileHostInput + FeatSourceMint) must see the
// file-handoff descriptions, while Grok / generic HTTP hosts (FeatSourceMint
// only) must see the mint-only descriptions with no file handoff.
func TestHostServerUploadVaultPresentation(t *testing.T) {
	openAIUpload, openAIVault := resolveHostUploadVault(hostenv.ProfileOpenAIHTTP)
	require.Contains(t, openAIUpload, "`file`", "OpenAI-HTTP upload_file must advertise the file handoff")
	require.Contains(t, openAIUpload, "source.mode=mint", "OpenAI-HTTP upload_file must still advertise the HTTP mint source")
	require.Contains(t, openAIVault, "file input", "OpenAI-HTTP vault_put_file must advertise the file handoff")
	require.Contains(t, openAIVault, "source.mode=mint", "OpenAI-HTTP vault_put_file must still advertise the HTTP mint source")

	for _, p := range []hostenv.PlatformProfile{hostenv.ProfileGrokHTTP, hostenv.ProfileHTTPGeneric} {
		upload, vaultDesc := resolveHostUploadVault(p)
		require.Contains(t, upload, "source.mode=mint", "HTTP generic/Grok upload_file must advertise the mint source")
		require.NotContains(t, upload, "`file`", "HTTP generic/Grok upload_file must NOT advertise the file handoff")
		require.Contains(t, vaultDesc, "source.mode=mint", "HTTP generic/Grok vault_put_file must advertise the mint source")
		require.NotContains(t, vaultDesc, "file input", "HTTP generic/Grok vault_put_file must NOT advertise the file handoff")
	}
}

// TestUploadVaultMatchesTransport verifies the "reuse startup server" gate:
// the startup HTTP transport (mint-only) already matches Grok / generic HTTP
// hosts, so only hosts that change the upload/vault presentation (OpenAI-HTTP)
// trigger a dedicated per-host server.
func TestUploadVaultMatchesTransport(t *testing.T) {
	httpT := hostenv.TransportHTTP
	require.False(t, uploadVaultMatchesTransport(hostenv.ProfileOpenAIHTTP, httpT),
		"OpenAI-HTTP changes the upload/vault presentation and needs a dedicated server")
	require.True(t, uploadVaultMatchesTransport(hostenv.ProfileGrokHTTP, httpT),
		"Grok-HTTP matches the startup mint-only presentation; reuse the startup server")
	require.True(t, uploadVaultMatchesTransport(hostenv.ProfileHTTPGeneric, httpT),
		"Generic-HTTP matches the startup mint-only presentation; reuse the startup server")
}

// TestHostServerDescriptorOverride mirrors the per-host description override
// registerCustomTools applies: a transport-built descriptor still carries the
// file-handoff description when resolved against the profile, and keeps its
// MCPTargets so describe_tool / search_tools keep the per-request surface.
func TestHostServerDescriptorOverride(t *testing.T) {
	// Startup HTTP descriptor: mint-only, no file handoff.
	startupUpload := transfer.NewUploadFileDescriptor(false, false, nil, nil, nil, nil, 0)
	require.NotContains(t, startupUpload.Description, "`file`")
	require.Contains(t, startupUpload.Description, "source.mode=mint")

	startupVault := vault.NewVaultPutFileDescriptor(false, false, nil, nil, nil, nil, 0)
	require.NotContains(t, startupVault.Description, "`file`")

	// Re-resolve both against the OpenAI-HTTP profile (as a dedicated per-host
	// server would) and confirm the baked tools/list description and MCPTargets.
	uploadDesc, vaultDesc := resolveHostUploadVault(hostenv.ProfileOpenAIHTTP)
	startupUpload.Description = uploadDesc
	startupVault.Description = vaultDesc

	require.Contains(t, startupUpload.Description, "`file`")
	require.Equal(t, toolforge.UploadFileTargets, startupUpload.MCPTargets)
	require.Contains(t, startupVault.Description, "file input")
	require.Equal(t, toolforge.VaultPutFileTargets, startupVault.MCPTargets)

	// Gateway parity: sdk.Tool bakes desc.Description into the *mcp.Tool,
	// which is exactly what tools/list serves.
	require.Equal(t, startupUpload.Description, sdk.Tool(startupUpload).Description)
	require.Equal(t, startupVault.Description, sdk.Tool(startupVault).Description)
}

// TestHostServerCachePerHost verifies the cache is keyed by HostType, that the
// factory receives the resolved profile, and that a repeated request for the
// same host reuses the cached server while a different host builds its own.
func TestHostServerCachePerHost(t *testing.T) {
	called := map[hostenv.HostType]int{}
	factory := func(profile hostenv.PlatformProfile) *sdk.Server {
		called[profile.HostType]++
		return sdk.NewServer(nil)
	}
	cache := newHostServerCache(factory, nil)

	oa1 := cache.Get(hostRequest(t, "openai-mcp/1.0.0"))
	require.NotNil(t, oa1)
	oa2 := cache.Get(hostRequest(t, "openai-mcp/1.0.0"))
	require.Same(t, oa1, oa2, "same OpenAI host must reuse its cached server")
	require.Equal(t, 1, called[hostenv.HostOpenAI])

	cache.Get(hostRequest(t, "grok-connectors-manager/0.1.0"))
	require.Equal(t, 1, called[hostenv.HostGrok])

	cache.Get(hostRequest(t, "some-generic-client/1.0"))
	require.Equal(t, 1, called[hostenv.HostGeneric])
}
