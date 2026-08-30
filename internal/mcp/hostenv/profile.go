package hostenv

import (
	"net/http"
	"time"
)

// HostType identifies the connected MCP client platform.
type HostType string

const (
	HostUnknown       HostType = "unknown"
	HostOpenAI        HostType = "openai"
	HostChatGPT       HostType = "chatgpt"
	HostGrok          HostType = "grok"
	HostOpenCode      HostType = "opencode"
	HostKilo          HostType = "kilo"
	HostKiro          HostType = "kiro"
	HostClaude        HostType = "claude"
	HostClaudeDesktop HostType = "claude-desktop"
	HostClaudeCode    HostType = "claude-code"

	// HostStdioApps is a synthetic host representing any co-located stdio
	// client that also renders MCP Apps UI. It is the alias target shared by
	// the concrete stdio hosts that present this surface (Claude Desktop,
	// Goose); it is never detected directly.
	HostStdioApps   HostType = "stdio-apps"
	HostAiderDesk   HostType = "aider-desk"
	HostDevin       HostType = "devin"
	HostCline       HostType = "cline"
	HostCodex       HostType = "codex"
	HostCopilotCLI  HostType = "copilot-cli"
	HostGoose       HostType = "goose"
	HostAntigravity HostType = "antigravity"
	HostKimi        HostType = "kimi"
	HostZed         HostType = "zed"
	HostFX          HostType = "fx"
	HostGeneric     HostType = "generic"
)

// TransportKind is the MCP transport the server runs under. It decides
// which file-input mechanism actually works: only one mechanism is real per
// transport, and the caller never picks it — registration and the resolver do.
// The values are the same string constants used by transfer.TransportKind;
// transfer re-exports these via a type alias so both packages agree.
type TransportKind string

const (
	// TransportStdio is co-located stdio/local mode.
	TransportStdio TransportKind = "stdio"
	// TransportHTTP is remote HTTP or a real tunnel with a reachable HTTP mux.
	TransportHTTP TransportKind = "http"
	// TransportOpenAI is the embedded OpenAI Secure MCP Tunnel: pure MCP
	// RPC with no reachable HTTP mux.
	TransportOpenAI TransportKind = "openai"
)

// AuthMethod describes how the client authenticated.
type AuthMethod string

const (
	AuthNone   AuthMethod = "none"
	AuthBearer AuthMethod = "bearer"
	AuthOAuth  AuthMethod = "oauth"
)

// ClientInfo carries the MCP clientInfoImplementation fields from the
// wire (initialize params or per-request _meta).
type ClientInfo struct {
	Name        string
	Version     string
	Title       string
	Description string
}

// TokenInfo carries the OAuth bearer token information extracted by the
// SDK's auth middleware.
type TokenInfo struct {
	Scopes     []string
	Expiration time.Time
	UserID     string
	Extra      map[string]any
}

// PlatformProfile is the resolved capability set for a specific host on
// a specific transport. It is the "browser profile" in the caniuse
// analogy: a static declaration of which features a HostType + Transport
// combination supports, overlaid with runtime wire signals.
type PlatformProfile struct {
	HostType   HostType
	Transport  TransportKind
	AuthMethod AuthMethod
	Remote     bool
	Features   FeatureSet

	// Surface declares which Pinner operation domains/tool families this
	// server exposes. It is a server-construction-time property (full for the
	// CLI/local MCP server, restricted to the hosted surface on a
	// Portal-embedded server) rather than a wire signal. A zero Surface means
	// the full surface. The whole profile-aware surface — tool registration,
	// Apps, resources, prompts, and the agent_guide flow DSL — gates on it.
	Surface Surface

	// Raw wire signals, populated by the detector for runtime
	// introspection by tools that need them at call time.
	ClientInfo  *ClientInfo
	ProtocolVer string
	UserAgent   string
	Headers     http.Header
	TokenInfo   *TokenInfo
}

// Predicate is a boolean test over a resolved PlatformProfile. Builders use it
// for gates that cannot be expressed as a Feature — e.g. a decision specific to
// one host. The platform DSL (toolforge.DescBuilder and the guide builders)
// accept predicates where a feature is not the right abstraction.
type Predicate func(PlatformProfile) bool

// HostIs matches profiles connected from the given host. It is a convenience
// constructor so call sites read as prose (WhenHost(hostenv.HostGrok, ...))
// rather than spelling out the closure.
func HostIs(h HostType) Predicate {
	return func(p PlatformProfile) bool { return p.HostType == h }
}

// Not negates a predicate. Builders use it for the "unless host" style gates.
func Not(p Predicate) Predicate {
	return func(prof PlatformProfile) bool { return !p(prof) }
}

// TransportIs matches profiles running on the given transport. It is a
// convenience constructor so description DSL call sites read as prose
// (WhenTransport(hostenv.TransportOpenAI, ...)) rather than spelling out a
// closure. It lets a segment gate on the transport alone — e.g. url/data
// source-mode copy for upload_file, which only actually accept those modes on
// the OpenAI tunnel transport even when a host profile also declares
// FeatSourceData/FeatSourceURL to register the separate upload_data/upload_url
// tools.
func TransportIs(t TransportKind) Predicate {
	return func(p PlatformProfile) bool { return p.Transport == t }
}

// Has reports whether the profile supports the given feature.
func (p PlatformProfile) Has(f Feature) bool {
	return p.Features.Has(f)
}

// IsTransport reports whether the profile's transport matches t.
func (p PlatformProfile) IsTransport(t TransportKind) bool {
	return p.Transport == t
}

// IsHost reports whether the profile's host type matches h.
func (p PlatformProfile) IsHost(h HostType) bool {
	return p.HostType == h
}

// CloneFeatures returns a shallow copy of this profile with a cloned
// FeatureSet. Callers that overlay runtime flags (e.g. setting
// FeatFileHostInput based on whether a relay handler is wired) MUST use
// this before mutating Features — the FeatureSet in a static profile
// is a shared map.
func (p PlatformProfile) CloneFeatures() PlatformProfile {
	p.Features = p.Features.Clone()
	return p
}
