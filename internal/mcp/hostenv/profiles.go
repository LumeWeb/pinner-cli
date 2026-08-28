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
// caps. Genuine per-host capability features (FeatFileHostInput, FeatXMcpFile,
// FeatMCPApps, FeatElicitation) belong in caps. FeatSourceData/FeatSourceURL
// may also be declared (e.g. ProfileGrokHTTP) but there they gate the SEPARATE
// relay tools (upload_data / upload_url) for registration and positive copy —
// they do NOT widen upload_file/vault_put_file's source.mode enum, which
// travels only with the transport (via TransportKindFromFeatures). Declaring a
// source mechanism feature never implies the corresponding upload_file source
// mode.
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
// FeatFileHostInput). MCP Apps is NOT declared here: Grok's connector has not
// advertised Apps support on the wire, so it is set only by the live overlay
// (requestCaps / UI.SupportsApps()) when the initialize client capabilities
// actually negotiate it.
//
// FeatSourceData and FeatSourceURL ARE declared: Grok supports the separate
// upload_data (RFC 2397 data: URI) and upload_url (server-fetch URL) relay
// tools. Declaring them gates those tools' registration (see custom_tools.go)
// and drives their positive description copy. It does NOT flip upload_file's
// source.mode enum: that enum stays bound to the HTTP transport (mint only)
// via TransportKindFromFeatures in UploadSourceSchemaTransform, because
// upload_file's own handler on HTTP rejects url/data modes — the separate
// tools are the data/url byte path.
var ProfileGrokHTTP = newProfile(
	FeatureSet{
		FeatSourceData: true,
		FeatSourceURL:  true,
	},
	HostGrok, TransportHTTP, AuthOAuth, true,
)

// ProfileClaudeHTTP is Anthropic's Claude Web client (clientInfo name
// "Anthropic/ClaudeAI", User-Agent "Claude-User") connecting over HTTP +
// OAuth. Claude Web grants the agent no network egress (no curl) and no
// OpenAI-style file references, so its only real byte path is the base64
// upload_data relay — declared via FeatSourceData (which registers the
// upload_data tool; it does NOT flip upload_file's transport-bound mint
// source). It supports MCP Apps UI. Downloads are effectively not
// deliverable to the user: mint is unusable without curl, sink=drop needs an
// out-of-band fetch, and sink=local writes to the server's unreachable disk.
var ProfileClaudeHTTP = newProfile(
	FeatureSet{
		FeatSourceData: true,
		FeatMCPApps:    true,
	},
	HostClaude, TransportHTTP, AuthOAuth, true,
)

// ProfileStdioMCPApps is the generic profile for a co-located stdio client
// that also renders MCP Apps UI. It is the shared declaration (alias target)
// for every concrete stdio host that presents exactly this surface — Claude
// Desktop and Goose — so the capability set lives in one place and both hosts
// inherit it through resolveProfile (see profileAliasTargets). Such a host
// shares the local filesystem (source-path uploads, sink-local downloads) plus
// MCP Apps, with no network-restriction notice. HostStdioApps is the synthetic
// host backing the declaration; it is never detected directly.
var ProfileStdioMCPApps = newProfile(
	FeatureSet{
		FeatMCPApps: true,
	},
	HostStdioApps, TransportStdio, AuthNone, false,
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

// profileAliasTargets declares hosts whose static capability surface is
// identical to another host's, so they can reuse the target's declaration
// instead of maintaining a duplicate profile. An alias means "X is exactly
// Y": X presents the same profile as Y but keeps its own HostType, so
// callers can still distinguish it via HostIs(HostX) without duplicating a
// feature set that is guaranteed to match Y's.
//
// Adding support for a new host that is indistinguishable from an existing
// one is a one-line alias here plus a detector — no new profile is needed,
// and the alias cannot drift from its target because it is resolved through
// resolveProfile (see resolveProfile).
var profileAliasTargets = map[HostType]HostType{
	// aider-desk is a co-located stdio client whose capability surface is
	// exactly the generic stdio profile (co-located, sink-local,
	// source-path). It is its own HostType so host-specific gating can use
	// HostIs(HostAiderDesk), but inherits HostGeneric's declaration.
	HostAiderDesk: HostGeneric,

	// devin (the Cognition agent harness) is a co-located stdio client whose
	// capability surface is exactly the generic stdio profile as well. It is
	// its own HostType so host-specific gating can use HostIs(HostDevin),
	// but inherits HostGeneric's declaration.
	HostDevin: HostGeneric,

	// cline (VS Code extension / CLI, clientInfo name "@cline/core") is a
	// co-located stdio client whose capability surface is exactly the generic
	// stdio profile.
	HostCline: HostGeneric,

	// Codex is a co-located stdio client whose capability surface is exactly
	// the generic stdio profile (co-located, sink-local, source-path). It is
	// its own HostType so host-specific gating can use HostIs(HostCodex),
	// but inherits HostGeneric's declaration.
	HostCodex: HostGeneric,

	// Kilo Code is a co-located stdio client (clientInfo name "kilo") whose
	// capability surface is exactly the generic stdio profile (co-located,
	// sink-local, source-path). It is its own HostType so host-specific gating
	// can use HostIs(HostKilo), but inherits HostGeneric's declaration.
	HostKilo: HostGeneric,

	// OpenCode is a co-located stdio client (clientInfo name "opencode") whose
	// capability surface is exactly the generic stdio profile (co-located,
	// sink-local, source-path). It is its own HostType so host-specific gating
	// can use HostIs(HostOpenCode), but inherits HostGeneric's declaration.
	HostOpenCode: HostGeneric,

	// Claude Desktop (clientInfo name "claude-ai") is a co-located stdio client
	// that renders MCP Apps UI — exactly the generic stdio+MCP-Apps surface. It
	// is its own HostType so host-specific gating can use HostIs(HostClaudeDesktop),
	// but inherits the shared ProfileStdioMCPApps declaration.
	HostClaudeDesktop: HostStdioApps,

	// Goose (clientInfo name "goose-app") is a co-located stdio client that
	// renders MCP Apps UI — exactly the generic stdio+MCP-Apps surface shared
	// with Claude Desktop. It is its own HostType so host-specific gating can
	// use HostIs(HostGoose), but inherits the shared ProfileStdioMCPApps
	// declaration.
	HostGoose: HostStdioApps,

	// Antigravity (Google IDE) is a co-located stdio client (clientInfo name
	// "antigravity-client") whose capability surface is exactly the generic
	// stdio profile (co-located, sink-local, source-path). It is its own
	// HostType so host-specific gating can use HostIs(HostAntigravity), but
	// inherits HostGeneric's declaration.
	HostAntigravity: HostGeneric,
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
	// An aliased host reuses its target's static declaration but keeps its own
	// HostType. resolveProfile returns a value copy (never the shared static
	// profile), so overriding HostType here cannot corrupt the target profile.
	if base, ok := profileAliasTargets[host]; ok {
		p := resolveProfile(base, transport, auth)
		p.HostType = host
		return p
	}

	switch {
	case (host == HostOpenAI || host == HostChatGPT) && transport == TransportOpenAI:
		return ProfileOpenAITunnel
	case (host == HostOpenAI || host == HostChatGPT) && transport == TransportHTTP:
		return ProfileOpenAIHTTP
	case host == HostGrok && transport == TransportHTTP:
		return ProfileGrokHTTP
	case host == HostClaude && transport == TransportHTTP:
		return ProfileClaudeHTTP
	case host == HostStdioApps && transport == TransportStdio:
		return ProfileStdioMCPApps
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
