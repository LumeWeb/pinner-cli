package hostenv

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helper constructors
// ---------------------------------------------------------------------------

func newTokenInfo() *TokenInfo {
	return &TokenInfo{
		Scopes:     []string{"read", "write"},
		Expiration: time.Date(2025, time.December, 31, 23, 59, 59, 0, time.UTC),
		UserID:     "user-123",
		Extra: map[string]any{
			"iss": "test-issuer",
		},
	}
}

// ---------------------------------------------------------------------------
// Detector tests
// ---------------------------------------------------------------------------

func TestOpenAIDetector_MatchByUserAgent(t *testing.T) {
	d := openAIDetector{}

	// Positive: UA contains "openai", no token → bearer auth
	host, auth := d.Match(DetectRequest{
		UserAgent: "openai-mcp/1.0.0",
	})
	require.Equal(t, HostOpenAI, host)
	require.Equal(t, AuthBearer, auth)
}

func TestOpenAIDetector_MatchByUserAgentWithToken(t *testing.T) {
	d := openAIDetector{}

	// Positive: UA contains "openai", token present → oauth auth
	host, auth := d.Match(DetectRequest{
		UserAgent: "openai-mcp/1.0.0",
		TokenInfo: newTokenInfo(),
	})
	require.Equal(t, HostOpenAI, host)
	require.Equal(t, AuthOAuth, auth)
}

func TestOpenAIDetector_MatchByClientInfo(t *testing.T) {
	d := openAIDetector{}

	// Positive: ClientInfo name contains "openai"
	host, auth := d.Match(DetectRequest{
		ClientInfo: &ClientInfo{Name: "openai-mcp", Version: "1.0.0"},
	})
	require.Equal(t, HostOpenAI, host)
	require.Equal(t, AuthBearer, auth)

	// Positive: ClientInfo name contains "chatgpt"
	host, auth = d.Match(DetectRequest{
		ClientInfo: &ClientInfo{Name: "chatgpt-runtime"},
	})
	require.Equal(t, HostOpenAI, host)
	require.Equal(t, AuthBearer, auth)
}

func TestOpenAIDetector_MatchByHeaders(t *testing.T) {
	d := openAIDetector{}

	// Positive: X-Openai-Session header present
	h := http.Header{}
	h.Set("X-Openai-Session", "sess-123")
	host, auth := d.Match(DetectRequest{
		Headers: h,
	})
	require.Equal(t, HostOpenAI, host)
	require.Equal(t, AuthOAuth, auth)

	// Positive: X-Openai-Subject header present
	h2 := http.Header{}
	h2.Set("X-Openai-Subject", "user-abc")
	host, auth = d.Match(DetectRequest{
		Headers: h2,
	})
	require.Equal(t, HostOpenAI, host)
	require.Equal(t, AuthOAuth, auth)
}

func TestOpenAIDetector_NoMatch(t *testing.T) {
	d := openAIDetector{}

	// Negative: unrelated UA, no ClientInfo, no headers
	host, auth := d.Match(DetectRequest{
		UserAgent: "some-random-client/2.0.0",
	})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)

	// Negative: ClientInfo but no matching name
	host, auth = d.Match(DetectRequest{
		ClientInfo: &ClientInfo{Name: "some-client", Version: "1.0.0"},
	})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)

	// Negative: headers but no OpenAI-specific ones
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	host, auth = d.Match(DetectRequest{
		Headers: h,
	})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)

	// Negative: OpenAI UA but co-located (stdio) — OpenAI is always remote
	host, auth = d.Match(DetectRequest{
		UserAgent: "openai-mcp/1.0.0",
		CoLocated: true,
	})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)
}

func TestGrokDetector_MatchByUserAgent(t *testing.T) {
	d := grokDetector{}

	// Positive: UA contains "grok"
	host, auth := d.Match(DetectRequest{
		UserAgent: "grok-connectors-manager/0.1.0",
	})
	require.Equal(t, HostGrok, host)
	require.Equal(t, AuthBearer, auth)

	// Positive: with token → OAuth
	host, auth = d.Match(DetectRequest{
		UserAgent: "grok-connectors-manager/0.1.0",
		TokenInfo: newTokenInfo(),
	})
	require.Equal(t, HostGrok, host)
	require.Equal(t, AuthOAuth, auth)
}

func TestGrokDetector_NoMatch(t *testing.T) {
	d := grokDetector{}

	// Negative: grokDetector does NOT check ClientInfo, only User-Agent
	host, auth := d.Match(DetectRequest{
		ClientInfo: &ClientInfo{Name: "grok-something"},
	})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)

	// Negative: unrelated UA
	host, auth = d.Match(DetectRequest{
		UserAgent: "openai-mcp/1.0.0",
	})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)

	// Negative: Grok UA but co-located (stdio) — Grok is always remote
	host, auth = d.Match(DetectRequest{
		UserAgent: "grok-connectors-manager/0.1.0",
		CoLocated: true,
	})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)

	// Negative: Grok UA but OpenAI tunnel — Grok cannot use OpenAI's tunnel
	host, auth = d.Match(DetectRequest{
		UserAgent:    "grok-connectors-manager/0.1.0",
		TunnelOpenAI: true,
	})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)
}

func TestClaudeDetector_MatchByUserAgent(t *testing.T) {
	d := claudeDetector{}

	// Positive: UA contains "claude-user", no token → bearer auth
	host, auth := d.Match(DetectRequest{UserAgent: "Claude-User"})
	require.Equal(t, HostClaude, host)
	require.Equal(t, AuthBearer, auth)
}

func TestClaudeDetector_MatchByUserAgentWithToken(t *testing.T) {
	d := claudeDetector{}

	// Positive: UA contains "claude-user", token present → oauth auth
	host, auth := d.Match(DetectRequest{UserAgent: "Claude-User", TokenInfo: newTokenInfo()})
	require.Equal(t, HostClaude, host)
	require.Equal(t, AuthOAuth, auth)
}

func TestClaudeDetector_MatchByClientInfo(t *testing.T) {
	d := claudeDetector{}

	// Positive: clientInfo vendor token "anthropic" → Claude Web
	host, auth := d.Match(DetectRequest{
		ClientInfo: &ClientInfo{Name: "Anthropic/ClaudeAI", Version: "1.0.0"},
	})
	require.Equal(t, HostClaude, host)
	require.Equal(t, AuthBearer, auth)

	// Negative (disambiguation): Desktop's "claude-ai" must NOT match Web —
	// Web requires the "anthropic" vendor token.
	host, auth = d.Match(DetectRequest{
		ClientInfo: &ClientInfo{Name: "claude-ai", Version: "0.1.0"},
	})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)
}

func TestClaudeDetector_MatchByHeader(t *testing.T) {
	d := claudeDetector{}

	h := http.Header{}
	h.Set("X-Anthropic-Client", "ClaudeAI")
	host, auth := d.Match(DetectRequest{Headers: h})
	require.Equal(t, HostClaude, host)
	require.Equal(t, AuthBearer, auth)

	h2 := http.Header{}
	h2.Set("X-Anthropic-Version", "2026-07-28")
	host, auth = d.Match(DetectRequest{Headers: h2})
	require.Equal(t, HostClaude, host)
	require.Equal(t, AuthBearer, auth)
}

// TestClaudeDetector_NarrowUA guards the intentionally-narrow Web UA match: a
// remote UA that merely contains "claude" (but not "claude-user") must not
// classify the client as Claude Web.
func TestClaudeDetector_NarrowUA(t *testing.T) {
	d := claudeDetector{}

	host, auth := d.Match(DetectRequest{UserAgent: "claude-connector/1.0"})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)
}

func TestClaudeDetector_NoMatch(t *testing.T) {
	d := claudeDetector{}

	// Unrelated signals
	host, auth := d.Match(DetectRequest{UserAgent: "some-random-client/2.0.0"})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)

	// Claude Web is always remote — co-located is handled by claudeDesktopDetector.
	host, auth = d.Match(DetectRequest{UserAgent: "Claude-User", CoLocated: true})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)

	// Claude Web never uses the OpenAI tunnel.
	host, auth = d.Match(DetectRequest{UserAgent: "Claude-User", TunnelOpenAI: true})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)
}

func TestClaudeDesktopDetector_MatchByClientInfo(t *testing.T) {
	d := claudeDesktopDetector{}

	// Positive: co-located with the exact "claude-ai" name → host + AuthNone
	host, auth := d.Match(DetectRequest{
		ClientInfo: &ClientInfo{Name: "claude-ai", Version: "0.1.0"},
		CoLocated:  true,
	})
	require.Equal(t, HostClaudeDesktop, host)
	require.Equal(t, AuthNone, auth)

	// Case/whitespace-insensitive exact match
	host, auth = d.Match(DetectRequest{
		ClientInfo: &ClientInfo{Name: "  Claude-AI  "},
		CoLocated:  true,
	})
	require.Equal(t, HostClaudeDesktop, host)
	require.Equal(t, AuthNone, auth)
}

func TestClaudeDesktopDetector_NoMatch(t *testing.T) {
	d := claudeDesktopDetector{}

	// Not co-located (remote HTTP) → that is Claude Web's detector.
	host, auth := d.Match(DetectRequest{ClientInfo: &ClientInfo{Name: "claude-ai"}})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)

	// Co-located but the exact name is required — "Claude Desktop" is not "claude-ai".
	host, auth = d.Match(DetectRequest{
		ClientInfo: &ClientInfo{Name: "Claude Desktop"},
		CoLocated:  true,
	})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)

	// Co-located but OpenAI tunnel → Desktop never uses the tunnel.
	host, auth = d.Match(DetectRequest{
		ClientInfo:   &ClientInfo{Name: "claude-ai"},
		CoLocated:    true,
		TunnelOpenAI: true,
	})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)

	// Co-located, unrelated clientInfo
	host, auth = d.Match(DetectRequest{
		ClientInfo: &ClientInfo{Name: "some-other-tool"},
		CoLocated:  true,
	})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)

	// Co-located, no clientInfo at all
	host, auth = d.Match(DetectRequest{CoLocated: true})
	require.Equal(t, HostUnknown, host)
	require.Equal(t, AuthMethod(""), auth)
}

// ---------------------------------------------------------------------------
// Registry Detect tests
// ---------------------------------------------------------------------------

func TestRegistry_Detect_OpenAIOverHTTP(t *testing.T) {
	r := NewRegistry()

	req := DetectRequest{
		UserAgent:    "openai-mcp/1.0.0",
		TokenInfo:    newTokenInfo(),
		CoLocated:    false,
		TunnelOpenAI: false,
	}
	prof := r.Detect(req)

	require.Equal(t, ProfileOpenAIHTTP.HostType, prof.HostType)
	require.Equal(t, ProfileOpenAIHTTP.Transport, prof.Transport)
	require.Equal(t, AuthOAuth, prof.AuthMethod)
	// Runtime overlay
	require.Equal(t, "openai-mcp/1.0.0", prof.UserAgent)
	require.NotNil(t, prof.TokenInfo)
	// Should match the declared profile's features
	require.True(t, prof.Has(FeatFileHostInput))
	require.True(t, prof.Has(FeatMCPApps))
	require.True(t, prof.Has(FeatRemoteAccess))
}

func TestRegistry_Detect_OpenAIOverHTTP_NoToken(t *testing.T) {
	r := NewRegistry()

	req := DetectRequest{
		UserAgent:    "openai-mcp/1.0.0",
		CoLocated:    false,
		TunnelOpenAI: false,
	}
	prof := r.Detect(req)

	require.Equal(t, HostOpenAI, prof.HostType)
	require.Equal(t, TransportHTTP, prof.Transport)
	// No wire token → the detected per-request auth is bearer, not the static
	// ProfileOpenAIHTTP default (oauth).
	require.Equal(t, AuthBearer, prof.AuthMethod)
}

func TestRegistry_Detect_GrokOverHTTP(t *testing.T) {
	r := NewRegistry()

	req := DetectRequest{
		UserAgent:    "grok-connectors-manager/0.1.0",
		CoLocated:    false,
		TunnelOpenAI: false,
	}
	prof := r.Detect(req)

	require.Equal(t, ProfileGrokHTTP.HostType, prof.HostType)
	require.Equal(t, ProfileGrokHTTP.Transport, prof.Transport)
	// No wire token → the detected per-request auth is bearer, not the static
	// ProfileGrokHTTP default (oauth).
	require.Equal(t, AuthBearer, prof.AuthMethod)
	// Runtime overlay
	require.Equal(t, "grok-connectors-manager/0.1.0", prof.UserAgent)
	// Grok cannot hand Pinner an OpenAI {download_url, file_id} file object,
	// and it does NOT statically claim MCP Apps: Apps is only set by the live
	// wire overlay (UI.SupportsApps()) once the client advertises it.
	require.False(t, prof.Has(FeatFileHostInput))
	require.False(t, prof.Has(FeatMCPApps))
	// Mechanism features are transport-derived: HTTP -> mint + remote.
	// Grok additionally declares FeatSourceData and FeatSourceURL so the
	// separate upload_data / upload_url relay tools register and carry their
	// positive description copy. These capability features do NOT flip
	// upload_file's source.mode enum, which stays bound to the HTTP transport
	// (mint only) via TransportKindFromFeatures in UploadSourceSchemaTransform.
	require.True(t, prof.Has(FeatSourceMint))
	require.True(t, prof.Has(FeatRemoteAccess))
	require.True(t, prof.Has(FeatSourceData))
	require.True(t, prof.Has(FeatSourceURL))
}

func TestRegistry_Detect_ClaudeWebOverHTTP(t *testing.T) {
	r := NewRegistry()

	req := DetectRequest{
		ClientInfo:   &ClientInfo{Name: "Anthropic/ClaudeAI", Version: "1.0.0"},
		UserAgent:    "Claude-User",
		TokenInfo:    newTokenInfo(),
		CoLocated:    false,
		TunnelOpenAI: false,
	}
	prof := r.Detect(req)

	require.Equal(t, ProfileClaudeHTTP.HostType, prof.HostType)
	require.Equal(t, ProfileClaudeHTTP.Transport, prof.Transport)
	// Wire token present → detected auth is oauth.
	require.Equal(t, AuthOAuth, prof.AuthMethod)
	require.True(t, prof.Remote)
	require.Equal(t, "Claude-User", prof.UserAgent)
	require.Equal(t, "Anthropic/ClaudeAI", prof.ClientInfo.Name)
	// Claude Web: MCP Apps + the base64 upload_data relay (FeatSourceData).
	require.True(t, prof.Has(FeatMCPApps))
	require.True(t, prof.Has(FeatSourceData))
	// It does NOT get the OpenAI file-host / url relay, nor a co-located path.
	require.False(t, prof.Has(FeatFileHostInput))
	require.False(t, prof.Has(FeatSourceURL))
	require.False(t, prof.Has(FeatSourcePath))
	require.False(t, prof.Has(FeatCoLocated))
}

func TestRegistry_Detect_ClaudeDesktopOverStdio(t *testing.T) {
	r := NewRegistry()

	req := DetectRequest{
		ClientInfo: &ClientInfo{Name: "claude-ai", Version: "0.1.0"},
		CoLocated:  true,
	}
	prof := r.Detect(req)

	require.Equal(t, ProfileClaudeDesktopStdio.HostType, prof.HostType)
	require.Equal(t, ProfileClaudeDesktopStdio.Transport, prof.Transport)
	require.Equal(t, AuthNone, prof.AuthMethod)
	require.False(t, prof.Remote)
	require.Equal(t, "claude-ai", prof.ClientInfo.Name)
	// Desktop is co-located with full local file access + MCP Apps.
	require.True(t, prof.Has(FeatSourcePath))
	require.True(t, prof.Has(FeatSinkLocal))
	require.True(t, prof.Has(FeatCoLocated))
	require.True(t, prof.Has(FeatMCPApps))
	// Desktop is NOT the network-restricted Web: no data-only gate.
	require.False(t, prof.Has(FeatSourceData))
	require.False(t, prof.Has(FeatRemoteAccess))
}

func TestRegistry_Detect_UnknownOverStdio(t *testing.T) {
	r := NewRegistry()

	req := DetectRequest{
		CoLocated: true,
	}
	prof := r.Detect(req)

	require.Equal(t, ProfileStdioGeneric.HostType, prof.HostType)
	require.Equal(t, ProfileStdioGeneric.Transport, prof.Transport)
	require.Equal(t, ProfileStdioGeneric.AuthMethod, prof.AuthMethod)
	require.False(t, prof.Remote)
	// Features
	require.True(t, prof.Has(FeatSourcePath))
	require.True(t, prof.Has(FeatCoLocated))
	require.False(t, prof.Has(FeatMCPApps))
}

func TestRegistry_Detect_UnknownOverHTTP(t *testing.T) {
	r := NewRegistry()

	req := DetectRequest{
		CoLocated:    false,
		TunnelOpenAI: false,
	}
	prof := r.Detect(req)

	require.Equal(t, ProfileHTTPGeneric.HostType, prof.HostType)
	require.Equal(t, ProfileHTTPGeneric.Transport, prof.Transport)
	require.Equal(t, ProfileHTTPGeneric.AuthMethod, prof.AuthMethod)
	require.True(t, prof.Remote)
	// Features
	require.True(t, prof.Has(FeatRemoteAccess))
	require.False(t, prof.Has(FeatFileHostInput))
	require.False(t, prof.Has(FeatMCPApps))
}

func TestRegistry_Detect_OpenAITunnel(t *testing.T) {
	r := NewRegistry()

	req := DetectRequest{
		CoLocated:    false,
		TunnelOpenAI: true,
	}
	prof := r.Detect(req)

	require.Equal(t, ProfileOpenAITunnel.HostType, prof.HostType)
	require.Equal(t, ProfileOpenAITunnel.Transport, prof.Transport)
	require.Equal(t, ProfileOpenAITunnel.AuthMethod, prof.AuthMethod)
	require.True(t, prof.Remote)
	// Features
	require.True(t, prof.Has(FeatFileHostInput))
	require.True(t, prof.Has(FeatSourceURL))
	require.True(t, prof.Has(FeatSourceData))
	require.True(t, prof.Has(FeatMCPApps))
}

// TestRegistry_Detect_OpenAITunnelKeepsAuthNoneWithToken guards the secure
// tunnel's AuthNone design: even a detector-matched tunnel request carrying a
// token (which the detector would report as OAuth) must not flip the tunnel
// profile's AuthMethod away from AuthNone. The wire-auth overlay only applies
// to the response-auth TransportHTTP profiles.
func TestRegistry_Detect_OpenAITunnelKeepsAuthNoneWithToken(t *testing.T) {
	r := NewRegistry()

	req := DetectRequest{
		UserAgent:    "openai-mcp/1.0.0", // matches the openai detector
		TokenInfo:    newTokenInfo(),
		CoLocated:    false,
		TunnelOpenAI: true,
	}
	prof := r.Detect(req)

	require.Equal(t, ProfileOpenAITunnel.HostType, prof.HostType)
	require.Equal(t, TransportOpenAI, prof.Transport)
	require.Equal(t, AuthNone, prof.AuthMethod, "secure tunnel must keep AuthNone regardless of a wire token")
	// The token itself is still surfaced on the profile.
	require.NotNil(t, prof.TokenInfo)
}

func TestRegistry_Detect_OverlaysRuntimeSignals(t *testing.T) {
	r := NewRegistry()

	token := newTokenInfo()
	h := http.Header{}
	h.Set("X-Custom", "value")

	req := DetectRequest{
		ClientInfo:      &ClientInfo{Name: "openai-mcp", Version: "1.0"},
		ProtocolVersion: "2025-11-25",
		UserAgent:       "openai-mcp/1.0.0",
		Headers:         h,
		TokenInfo:       token,
		CoLocated:       false,
	}
	prof := r.Detect(req)

	require.Equal(t, "openai-mcp", prof.ClientInfo.Name)
	require.Equal(t, "1.0", prof.ClientInfo.Version)
	require.Equal(t, "2025-11-25", prof.ProtocolVer)
	require.Equal(t, "openai-mcp/1.0.0", prof.UserAgent)
	require.Equal(t, "value", prof.Headers.Get("X-Custom"))
	require.NotNil(t, prof.TokenInfo)
	require.Equal(t, "user-123", prof.TokenInfo.UserID)
}

func TestRegistry_Detect_EmptyRequest(t *testing.T) {
	r := NewRegistry()

	// Empty request: no signals, no CoLocated, no Tunnel → HTTP generic
	prof := r.Detect(DetectRequest{})

	require.Equal(t, HostGeneric, prof.HostType)
	require.Equal(t, TransportHTTP, prof.Transport)
}

// ---------------------------------------------------------------------------
// resolveProfile tests
// ---------------------------------------------------------------------------

func TestResolveProfile(t *testing.T) {
	tests := []struct {
		name      string
		host      HostType
		transport TransportKind
		auth      AuthMethod
		want      PlatformProfile
	}{
		{
			name:      "openai over openai tunnel",
			host:      HostOpenAI,
			transport: TransportOpenAI,
			auth:      AuthNone,
			want:      ProfileOpenAITunnel,
		},
		{
			name:      "chatgpt over openai tunnel",
			host:      HostChatGPT,
			transport: TransportOpenAI,
			auth:      AuthNone,
			want:      ProfileOpenAITunnel,
		},
		{
			name:      "openai over http",
			host:      HostOpenAI,
			transport: TransportHTTP,
			auth:      AuthOAuth,
			want:      ProfileOpenAIHTTP,
		},
		{
			name:      "chatgpt over http",
			host:      HostChatGPT,
			transport: TransportHTTP,
			auth:      AuthOAuth,
			want:      ProfileOpenAIHTTP,
		},
		{
			name:      "grok over http",
			host:      HostGrok,
			transport: TransportHTTP,
			auth:      AuthOAuth,
			want:      ProfileGrokHTTP,
		},
		{
			name:      "claude over http",
			host:      HostClaude,
			transport: TransportHTTP,
			auth:      AuthOAuth,
			want:      ProfileClaudeHTTP,
		},
		{
			name:      "claude desktop over stdio",
			host:      HostClaudeDesktop,
			transport: TransportStdio,
			auth:      AuthNone,
			want:      ProfileClaudeDesktopStdio,
		},
		{
			name:      "generic over stdio",
			host:      HostGeneric,
			transport: TransportStdio,
			auth:      AuthNone,
			want:      ProfileStdioGeneric,
		},
		{
			name:      "generic over http",
			host:      HostGeneric,
			transport: TransportHTTP,
			auth:      AuthBearer,
			want:      ProfileHTTPGeneric,
		},
		{
			name:      "unknown over stdio falls to stdio generic",
			host:      HostUnknown,
			transport: TransportStdio,
			auth:      AuthNone,
			want:      ProfileStdioGeneric,
		},
		{
			name:      "unknown over http falls to http generic",
			host:      HostUnknown,
			transport: TransportHTTP,
			auth:      AuthBearer,
			want:      ProfileHTTPGeneric,
		},
		{
			name:      "unknown combo falls back to stdio generic",
			host:      HostUnknown,
			transport: TransportKind("bogus"),
			auth:      AuthNone,
			want:      ProfileStdioGeneric,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveProfile(tt.host, tt.transport, tt.auth)
			require.Equal(t, tt.want.HostType, got.HostType)
			require.Equal(t, tt.want.Transport, got.Transport)
			require.Equal(t, tt.want.AuthMethod, got.AuthMethod)
			require.Equal(t, tt.want.Remote, got.Remote)
			require.Equal(t, tt.want.Features, got.Features)
		})
	}
}

// ---------------------------------------------------------------------------
// ProfileForTransport tests
// ---------------------------------------------------------------------------

func TestProfileForTransport(t *testing.T) {
	tests := []struct {
		name string
		t    TransportKind
		want PlatformProfile
	}{
		{name: "stdio", t: TransportStdio, want: ProfileStdioGeneric},
		{name: "http", t: TransportHTTP, want: ProfileHTTPGeneric},
		{name: "openai", t: TransportOpenAI, want: ProfileOpenAITunnel},
		{name: "unknown defaults to stdio", t: TransportKind("bogus"), want: ProfileStdioGeneric},
		{name: "empty defaults to stdio", t: TransportKind(""), want: ProfileStdioGeneric},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProfileForTransport(tt.t)
			require.Equal(t, tt.want.HostType, got.HostType)
			require.Equal(t, tt.want.Transport, got.Transport)
		})
	}
}

// ---------------------------------------------------------------------------
// FeatureSet tests
// ---------------------------------------------------------------------------

func TestFeatureSet_Has(t *testing.T) {
	fs := FeatureSet{
		FeatFileHostInput: true,
		FeatSourcePath:    true,
	}

	require.True(t, fs.Has(FeatFileHostInput))
	require.True(t, fs.Has(FeatSourcePath))
	require.False(t, fs.Has(FeatSourceMint))
	require.False(t, fs.Has(FeatMCPApps))
}

func TestFeatureSet_HasAll(t *testing.T) {
	fs := FeatureSet{
		FeatFileHostInput: true,
		FeatSourcePath:    true,
		FeatSourceMint:    true,
	}

	// Subset — should return true
	require.True(t, fs.HasAll(FeatureSet{
		FeatFileHostInput: true,
		FeatSourcePath:    true,
	}))

	// Full set — should return true
	require.True(t, fs.HasAll(FeatureSet{
		FeatFileHostInput: true,
		FeatSourcePath:    true,
		FeatSourceMint:    true,
	}))

	// Not subset — should return false
	require.False(t, fs.HasAll(FeatureSet{
		FeatFileHostInput: true,
		FeatMCPApps:       true, // not in fs
	}))

	// Empty requirement set — always true
	require.True(t, fs.HasAll(FeatureSet{}))
}

func TestFeatureSet_EmptySet(t *testing.T) {
	fs := FeatureSet{}

	require.False(t, fs.Has(FeatFileHostInput))
	require.True(t, fs.HasAll(FeatureSet{}))
	require.False(t, fs.HasAll(FeatureSet{FeatFileHostInput: true}))
}

func TestFeatureSet_NilSet(t *testing.T) {
	var fs FeatureSet // nil map

	require.False(t, fs.Has(FeatFileHostInput))
	require.True(t, fs.HasAll(FeatureSet{}))
	require.False(t, fs.HasAll(FeatureSet{FeatFileHostInput: true}))
}

// ---------------------------------------------------------------------------
// PlatformProfile method tests
// ---------------------------------------------------------------------------

func TestPlatformProfile_Has(t *testing.T) {
	p := ProfileStdioGeneric

	require.True(t, p.Has(FeatSourcePath))
	require.True(t, p.Has(FeatCoLocated))
	require.True(t, p.Has(FeatSinkLocal))
	require.False(t, p.Has(FeatRemoteAccess))
	require.False(t, p.Has(FeatFileHostInput))
}

func TestPlatformProfile_IsTransport(t *testing.T) {
	require.True(t, ProfileStdioGeneric.IsTransport(TransportStdio))
	require.False(t, ProfileStdioGeneric.IsTransport(TransportHTTP))
	require.False(t, ProfileStdioGeneric.IsTransport(TransportOpenAI))

	require.True(t, ProfileOpenAIHTTP.IsTransport(TransportHTTP))
	require.True(t, ProfileOpenAITunnel.IsTransport(TransportOpenAI))
}

func TestPlatformProfile_IsHost(t *testing.T) {
	require.True(t, ProfileStdioGeneric.IsHost(HostGeneric))
	require.False(t, ProfileStdioGeneric.IsHost(HostOpenAI))

	require.True(t, ProfileOpenAITunnel.IsHost(HostChatGPT))
	require.True(t, ProfileOpenAIHTTP.IsHost(HostOpenAI))
	require.True(t, ProfileClaudeHTTP.IsHost(HostClaude))
	require.False(t, ProfileClaudeHTTP.IsHost(HostClaudeDesktop))
	require.True(t, ProfileClaudeDesktopStdio.IsHost(HostClaudeDesktop))
	require.False(t, ProfileClaudeDesktopStdio.IsHost(HostClaude))

	require.True(t, ProfileHTTPGeneric.IsHost(HostGeneric))
}

// ---------------------------------------------------------------------------
// detectTransport tests
// ---------------------------------------------------------------------------

func TestDetectTransport(t *testing.T) {
	tests := []struct {
		name string
		req  DetectRequest
		want TransportKind
	}{
		{
			name: "co-located → stdio",
			req:  DetectRequest{CoLocated: true},
			want: TransportStdio,
		},
		{
			name: "co-located takes priority over tunnel → stdio",
			req:  DetectRequest{CoLocated: true, TunnelOpenAI: true},
			want: TransportStdio,
		},
		{
			name: "tunnel openai → openai",
			req:  DetectRequest{TunnelOpenAI: true},
			want: TransportOpenAI,
		},
		{
			name: "neither → http",
			req:  DetectRequest{},
			want: TransportHTTP,
		},
		{
			name: "explicitly false for both → http",
			req:  DetectRequest{CoLocated: false, TunnelOpenAI: false},
			want: TransportHTTP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectTransport(tt.req)
			require.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// DetectFromHTTPRequest tests
// ---------------------------------------------------------------------------

func TestDetectFromHTTPRequest_OpenAI(t *testing.T) {
	r := NewRegistry()

	h := http.Header{}
	h.Set("User-Agent", "openai-mcp/1.0.0")

	prof := r.DetectFromHTTPRequest(h, false, false, newTokenInfo())

	require.Equal(t, HostOpenAI, prof.HostType)
	require.Equal(t, TransportHTTP, prof.Transport)
	require.Equal(t, AuthOAuth, prof.AuthMethod)
	require.Equal(t, "openai-mcp/1.0.0", prof.UserAgent)
	require.NotNil(t, prof.TokenInfo)
	// Headers should be the same underlying map instance
	require.Equal(t, h, prof.Headers)
}

func TestDetectFromHTTPRequest_Grok(t *testing.T) {
	r := NewRegistry()

	h := http.Header{}
	h.Set("User-Agent", "grok-connectors-manager/0.1.0")

	prof := r.DetectFromHTTPRequest(h, false, false, nil)

	require.Equal(t, HostGrok, prof.HostType)
	require.Equal(t, TransportHTTP, prof.Transport)
	// No wire token → detected per-request auth is bearer, not the static
	// ProfileGrokHTTP default (oauth).
	require.Equal(t, AuthBearer, prof.AuthMethod)
	require.Equal(t, "grok-connectors-manager/0.1.0", prof.UserAgent)
}

func TestDetectFromHTTPRequest_UnknownHTTP(t *testing.T) {
	r := NewRegistry()

	h := http.Header{}
	h.Set("User-Agent", "random-tool/1.0")

	prof := r.DetectFromHTTPRequest(h, false, false, nil)

	require.Equal(t, HostGeneric, prof.HostType)
	require.Equal(t, TransportHTTP, prof.Transport)
}

func TestDetectFromHTTPRequest_CoLocated(t *testing.T) {
	r := NewRegistry()

	// Co-located=true overrides to stdio transport even with HTTP headers
	h := http.Header{}
	h.Set("User-Agent", "openai-mcp/1.0.0")

	prof := r.DetectFromHTTPRequest(h, true, false, nil)

	// Transport is stdio due to CoLocated
	require.Equal(t, TransportStdio, prof.Transport)
	// Although the openai-mcp UA is detected (HostOpenAI from the
	// detector), there is no OpenAI-over-stdio profile, so
	// resolveProfile falls through to ProfileStdioGeneric.
	require.Equal(t, ProfileStdioGeneric.HostType, prof.HostType)
}

func TestDetectFromHTTPRequest_TunnelOpenAI(t *testing.T) {
	r := NewRegistry()

	h := http.Header{}

	prof := r.DetectFromHTTPRequest(h, false, true, nil)

	require.Equal(t, TransportOpenAI, prof.Transport)
	// No matching detector signals, so tunnelOpenAI fallback → HostChatGPT
	require.Equal(t, HostChatGPT, prof.HostType)
}

func TestDetectFromHTTPRequest_PassesHeaders(t *testing.T) {
	r := NewRegistry()

	h := http.Header{}
	h.Set("User-Agent", "openai-mcp/1.0.0")
	h.Set("X-Openai-Session", "sess-abc")
	h.Set("X-Custom-Header", "custom-value")

	prof := r.DetectFromHTTPRequest(h, false, false, nil)

	require.Equal(t, "openai-mcp/1.0.0", prof.UserAgent)
	require.Equal(t, "sess-abc", prof.Headers.Get("X-Openai-Session"))
	require.Equal(t, "custom-value", prof.Headers.Get("X-Custom-Header"))
}

// ---------------------------------------------------------------------------
// NewRegistry tests
// ---------------------------------------------------------------------------

func TestNewRegistry_DetectorsRegistered(t *testing.T) {
	r := NewRegistry()

	require.NotNil(t, r)
	require.Len(t, r.detectors, 4)
	// Priority order: openai, grok, claude (web), claude-desktop
	require.IsType(t, openAIDetector{}, r.detectors[0])
	require.IsType(t, grokDetector{}, r.detectors[1])
	require.IsType(t, claudeDetector{}, r.detectors[2])
	require.IsType(t, claudeDesktopDetector{}, r.detectors[3])
}

func TestNewRegistry_PriorityOrder(t *testing.T) {
	r := NewRegistry()

	// OpenAI detector fires first for "openai" UA.
	req := DetectRequest{
		UserAgent: "openai-mcp/1.0.0",
	}
	prof := r.Detect(req)
	require.Equal(t, HostOpenAI, prof.HostType)
}

// ---------------------------------------------------------------------------
// Detection priority: first match wins
// ---------------------------------------------------------------------------

func TestRegistry_Priority_FirstMatchWins(t *testing.T) {
	r := NewRegistry()

	// OpenAI detector is tried first; grok UA doesn't contain "openai".
	req := DetectRequest{
		UserAgent: "grok-connectors-manager/0.1.0",
	}
	prof := r.Detect(req)
	require.Equal(t, HostGrok, prof.HostType)
}

// ---------------------------------------------------------------------------
// Transport-mechanism vs host-capability separation
// ---------------------------------------------------------------------------

// TestTransportMechanismFeaturesDerived locks in the invariant that source/
// sink/reachability features are a pure function of the transport, so they can
// never be hand-declared inconsistently per host.
func TestTransportMechanismFeaturesDerived(t *testing.T) {
	stdio := transportMechanismFeatures(TransportStdio)
	require.True(t, stdio[FeatSourcePath])
	require.True(t, stdio[FeatSinkLocal])
	require.True(t, stdio[FeatCoLocated])
	require.False(t, stdio[FeatSourceMint])

	http := transportMechanismFeatures(TransportHTTP)
	require.True(t, http[FeatSourceMint])
	require.True(t, http[FeatSinkLocal])
	require.True(t, http[FeatSinkDrop])
	require.True(t, http[FeatRemoteAccess])
	require.False(t, http[FeatSourceURL])
	require.False(t, http[FeatSourceData])

	openai := transportMechanismFeatures(TransportOpenAI)
	require.True(t, openai[FeatSourceURL])
	require.True(t, openai[FeatSourceData])
	require.True(t, openai[FeatSinkLocal])
	require.False(t, openai[FeatSourceMint])
	require.False(t, openai[FeatRemoteAccess])
}

// TestNewProfileMechanismCannotBeOverridden verifies that a profile's declared
// capability features can never flip a transport-mechanism feature: an HTTP
// host that declares e.g. FeatMCPApps still resolves to mint (not url/data),
// so a host supporting the data/url tools cannot corrupt upload_file's source.
func TestNewProfileMechanismCannotBeOverridden(t *testing.T) {
	// A hypothetical HTTP host that renders MCP Apps (like Grok) must still
	// present the HTTP mechanism: mint, never url/data, despite any capability.
	p := newProfile(FeatureSet{FeatMCPApps: true}, HostGrok, TransportHTTP, AuthOAuth, true)
	require.True(t, p.Has(FeatSourceMint))
	require.False(t, p.Has(FeatSourceURL))
	require.False(t, p.Has(FeatSourceData))
	require.True(t, p.Has(FeatMCPApps))
	require.True(t, p.Has(FeatRemoteAccess))
}
