package hostenv

// Pre-declared platform profiles. Each is a static declaration of the
// features a HostType + Transport combination supports. The detector
// resolves a wire request to one of these and overlays runtime signals.

// ProfileOpenAITunnel is the OpenAI/ChatGPT embedded tunnel: pure MCP
// RPC with no reachable HTTP mux. The ChatGPT runtime provides file
// references ({download_url, file_id}) and supports MCP Apps.
var ProfileOpenAITunnel = PlatformProfile{
	HostType:   HostChatGPT,
	Transport:  TransportOpenAI,
	AuthMethod: AuthNone,
	Remote:     true,
	Features: FeatureSet{
		FeatFileHostInput: true,
		FeatSourceURL:      true,
		FeatSourceData:     true,
		FeatXMcpFile:       true,
		FeatMCPApps:        true,
		FeatElicitation:   true,
		FeatSinkLocal:     true,
	},
}

// ProfileOpenAIHTTP is OpenAI's openai-mcp client connecting over HTTP
// (e.g. ChatGPT via remote MCP). It advertises OpenAI-specific headers
// and supports MCP Apps UI, but uses HTTP transport sources.
var ProfileOpenAIHTTP = PlatformProfile{
	HostType:   HostOpenAI,
	Transport:  TransportHTTP,
	AuthMethod: AuthOAuth,
	Remote:     true,
	Features: FeatureSet{
		FeatFileHostInput: true,
		FeatSourceMint:    true,
		FeatSinkLocal:     true,
		FeatSinkDrop:      true,
		FeatXMcpFile:      true,
		FeatMCPApps:       true,
		FeatElicitation:  true,
		FeatRemoteAccess:  true,
	},
}

// ProfileGrokHTTP is xAI Grok connectors over HTTP + OAuth. Grok sends a
// distinctive User-Agent (grok-connectors-manager) but no clientInfo.
// It does not support file references or MCP Apps.
var ProfileGrokHTTP = PlatformProfile{
	HostType:   HostGrok,
	Transport:  TransportHTTP,
	AuthMethod: AuthOAuth,
	Remote:     true,
	Features: FeatureSet{
		FeatSourceMint:    true,
		FeatSinkLocal:     true,
		FeatSinkDrop:      true,
		FeatRemoteAccess:  true,
	},
}

// ProfileStdioGeneric is the fallback for any unidentified client over
// stdio. It assumes co-located filesystem access but no host-specific
// features.
var ProfileStdioGeneric = PlatformProfile{
	HostType:   HostGeneric,
	Transport:  TransportStdio,
	AuthMethod: AuthNone,
	Remote:     false,
	Features: FeatureSet{
		FeatSourcePath:    true,
		FeatSinkLocal:     true,
		FeatCoLocated:     true,
	},
}

// ProfileHTTPGeneric is the fallback for any unidentified client over
// HTTP. It supports HTTP-reachable sources/sinks but no host-specific
// file input or MCP Apps.
var ProfileHTTPGeneric = PlatformProfile{
	HostType:   HostGeneric,
	Transport:  TransportHTTP,
	AuthMethod: AuthBearer,
	Remote:     true,
	Features: FeatureSet{
		FeatSourceMint:    true,
		FeatSinkLocal:     true,
		FeatSinkDrop:      true,
		FeatRemoteAccess:  true,
	},
}

// ProfileForTransport returns the generic profile for a transport kind.
// It is used at server startup when only the transport is known (no
// per-request detection has run yet). For OpenAI tunnel it returns
// ProfileOpenAITunnel (which implies the ChatGPT host) because the
// tunnel is OpenAI-specific.
func ProfileForTransport(t TransportKind) PlatformProfile {
	switch t {
	case TransportStdio:
		return ProfileStdioGeneric
	case TransportHTTP:
		return ProfileHTTPGeneric
	case TransportOpenAI:
		return ProfileOpenAITunnel
	default:
		return ProfileStdioGeneric
	}
}

// resolveProfile looks up the pre-declared profile for a HostType +
// Transport pair. It returns the best matching static profile; the
// caller overlays runtime signals (headers, tokenInfo, etc.) afterward.
func resolveProfile(host HostType, transport TransportKind, auth AuthMethod) PlatformProfile {
	switch {
	case (host == HostOpenAI || host == HostChatGPT) && transport == TransportOpenAI:
		return ProfileOpenAITunnel
	case (host == HostOpenAI || host == HostChatGPT) && transport == TransportHTTP:
		return ProfileOpenAIHTTP
	case host == HostGrok && transport == TransportHTTP:
		return ProfileGrokHTTP
	case transport == TransportStdio:
		return ProfileStdioGeneric
	case transport == TransportHTTP:
		return ProfileHTTPGeneric
	case transport == TransportOpenAI:
		return ProfileOpenAITunnel
	default:
		// Unknown transport: degrade to stdio generic. This is safer
		// than HTTP generic because stdio touches no network.
		return ProfileStdioGeneric
	}
}
