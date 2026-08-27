package hostenv

// Pre-declared platform profiles. Each is a static declaration of the
// capability features a HostType supports; the transport-mechanism features
// (source/sink/reachability) are merged in by newProfile from the profile's
// Transport (see transportMechanismFeatures). The detector resolves a wire
// request to one of these and overlays runtime signals.

// newProfile assembles a static PlatformProfile by merging a host's declared
// capability features with the transport-mechanism features derived from t.
// Mechanism features are transport-derived by construction (see
// transportMechanismFeatures), so they can never drift from the transport — an
// HTTP host always presents mint, never url/data, regardless of what is put in
// caps. Only genuine per-host capability features (FeatFileHostInput,
// FeatXMcpFile, FeatMCPApps, FeatElicitation) should be declared in caps.
func newProfile(caps FeatureSet, host HostType, t TransportKind, auth AuthMethod, remote bool) PlatformProfile {
	features := caps.Clone()
	mechanism := transportMechanismFeatures(t)
	for k, v := range mechanism {
		features[k] = v
	}
	return PlatformProfile{
		HostType:   host,
		Transport:  t,
		AuthMethod: auth,
		Remote:     remote,
		Features:   features,
	}
}

// ProfileOpenAITunnel is the OpenAI/ChatGPT embedded tunnel: pure MCP
// RPC with no reachable HTTP mux. The ChatGPT runtime provides file
// references ({download_url, file_id}) and supports MCP Apps.
var ProfileOpenAITunnel = newProfile(
	FeatureSet{
		FeatFileHostInput: true,
		FeatXMcpFile:      true,
		FeatMCPApps:       true,
		FeatElicitation:   true,
	},
	HostChatGPT, TransportOpenAI, AuthNone, true,
)

// ProfileOpenAIHTTP is OpenAI's openai-mcp client connecting over HTTP
// (e.g. ChatGPT via remote MCP). It advertises OpenAI-specific headers
// and supports MCP Apps UI, but uses HTTP transport sources.
var ProfileOpenAIHTTP = newProfile(
	FeatureSet{
		FeatFileHostInput: true,
		FeatXMcpFile:      true,
		FeatMCPApps:       true,
		FeatElicitation:   true,
	},
	HostOpenAI, TransportHTTP, AuthOAuth, true,
)

// ProfileGrokHTTP is xAI Grok connectors over HTTP + OAuth. Grok sends a
// distinctive User-Agent (grok-connectors-manager) but no clientInfo. It
// cannot hand Pinner an OpenAI {download_url, file_id} file object (no
// FeatFileHostInput), but it CAN render MCP Apps. Its data/url uploads go
// through the separately-wired upload_data / upload_url tools, not through
// upload_file's transport-bound mint source.
var ProfileGrokHTTP = newProfile(
	FeatureSet{
		FeatMCPApps: true,
	},
	HostGrok, TransportHTTP, AuthOAuth, true,
)

// ProfileStdioGeneric is the fallback for any unidentified client over
// stdio. It assumes co-located filesystem access but no host-specific
// capability features.
var ProfileStdioGeneric = newProfile(
	nil,
	HostGeneric, TransportStdio, AuthNone, false,
)

// ProfileHTTPGeneric is the fallback for any unidentified client over
// HTTP. It supports HTTP-reachable sources/sinks but no host-specific
// capability features.
var ProfileHTTPGeneric = newProfile(
	nil,
	HostGeneric, TransportHTTP, AuthBearer, true,
)

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
