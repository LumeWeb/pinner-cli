package mcp

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// This file implements the --dev-tools feature: a small set of read-only
// introspection tools that help debug the MCP server and the host connected to
// it. They are registered only when the MCP command is launched with
// --dev-tools, so the production surface is unaffected. Each tool is added to
// the catalog with DirectVisible set, so it is both discoverable through the
// progressive-disclosure meta-tools and listed directly in tools/list when
// dev tools are on.
//
// The tools are SDK-free: they read the enriched model.RequestCaps (which
// carries the resolved hostenv.PlatformProfile and, under dev tools, the raw
// wire snapshot), never the go-sdk request directly. Their StructuredContent
// is a typed struct so the result shape is stable and self-documenting.

// devClientInfo is the structured form of the connecting client's
// clientInfo implementation (name/title/version/description).
type devClientInfo struct {
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// devClientInfoFrom converts a hostenv client-info into the structured form,
// returning nil when there is nothing to report.
func devClientInfoFrom(ci *hostenv.ClientInfo) *devClientInfo {
	if ci == nil {
		return nil
	}
	return &devClientInfo{
		Name:        ci.Name,
		Version:     ci.Version,
		Title:       ci.Title,
		Description: ci.Description,
	}
}

// devUserAgent is the structured form of the connecting client's User-Agent
// wire signal. It carries the resolved value plus the raw header values so a
// host that sends a multi-valued User-Agent header is not collapsed to a
// single string (matching how http_headers keeps every value).
type devUserAgent struct {
	// Raw is the resolved User-Agent value from the profile.
	Raw string `json:"value,omitempty"`
	// Values lists every raw User-Agent header value observed on the wire.
	Values []string `json:"header_values,omitempty"`
}

// devUserAgentFrom returns the structured User-Agent for a profile, or nil when
// neither the resolved value nor any header value is present.
func devUserAgentFrom(profile *hostenv.PlatformProfile) *devUserAgent {
	if profile == nil {
		return nil
	}
	var values []string
	if len(profile.Headers) > 0 {
		values = profile.Headers.Values("User-Agent")
	}
	if profile.UserAgent == "" && len(values) == 0 {
		return nil
	}
	return &devUserAgent{Raw: profile.UserAgent, Values: values}
}

// devTokenInfo is the structured form of the OAuth bearer token info attached
// to the call.
type devTokenInfo struct {
	UserID     string         `json:"user_id,omitempty"`
	Scopes     []string       `json:"scopes,omitempty"`
	Expiration time.Time      `json:"expiration,omitempty"`
	Extra      map[string]any `json:"extra,omitempty"`
}

// devTokenInfoFrom converts hostenv token info into the structured form,
// returning nil when there is none.
func devTokenInfoFrom(ti *hostenv.TokenInfo) *devTokenInfo {
	if ti == nil {
		return nil
	}
	info := &devTokenInfo{UserID: ti.UserID}
	if len(ti.Scopes) > 0 {
		info.Scopes = ti.Scopes
	}
	if !ti.Expiration.IsZero() {
		info.Expiration = ti.Expiration
	}
	if len(ti.Extra) > 0 {
		info.Extra = ti.Extra
	}
	return info
}

// devHostEnvOutput is the StructuredContent of dev_host_env.
type devHostEnvOutput struct {
	ProtocolVersion    string              `json:"protocol_version,omitempty"`
	HostType           string              `json:"host_type"`
	Transport          string              `json:"transport"`
	AuthMethod         string              `json:"auth_method"`
	Remote             bool                `json:"remote"`
	Features           []string            `json:"features"`
	ClientInfo         *devClientInfo      `json:"client_info,omitempty"`
	UserAgent          *devUserAgent       `json:"user_agent,omitempty"`
	ClientCapabilities map[string]any      `json:"client_capabilities,omitempty"`
	InitializeParams   map[string]any      `json:"initialize_params,omitempty"`
	OAuthToken         *devTokenInfo       `json:"oauth_token,omitempty"`
	HTTPHeaders        map[string][]string `json:"http_headers,omitempty"`
}

// devProfileOutput is the StructuredContent of dev_profile.
type devProfileOutput struct {
	HostType        string         `json:"host_type"`
	Transport       string         `json:"transport"`
	AuthMethod      string         `json:"auth_method"`
	Remote          bool           `json:"remote"`
	ProtocolVersion string         `json:"protocol_version,omitempty"`
	Features        []string       `json:"features"`
	ClientInfo      *devClientInfo `json:"client_info,omitempty"`
	UserAgent       *devUserAgent  `json:"user_agent,omitempty"`
	TokenPresent    bool           `json:"token_present,omitempty"`
}

// devRequestOutput is the StructuredContent of dev_request.
type devRequestOutput struct {
	Tool            string         `json:"tool"`
	Arguments       map[string]any `json:"arguments,omitempty"`
	InputResponses  bool           `json:"input_responses"`
	ProtocolVersion string         `json:"protocol_version,omitempty"`
}

// devProfileFromRequest resolves the platform profile for a request, falling
// back to the stdio-generic default when the request carries no caps.
func devProfileFromRequest(request model.ToolRequest) *hostenv.PlatformProfile {
	return profileFromRequest(request)
}

// capsProtoVersion returns the negotiated protocol version, nil-safe.
func capsProtoVersion(request model.ToolRequest) string {
	if request.Caps == nil {
		return ""
	}
	return request.Caps.ProtocolVersion
}

// capsClientCapabilities returns the raw client capabilities snapshot, nil-safe.
func capsClientCapabilities(request model.ToolRequest) map[string]any {
	if request.Caps == nil {
		return nil
	}
	return request.Caps.Capabilities
}

// capsInitializeParams returns the raw initialize params snapshot, nil-safe.
func capsInitializeParams(request model.ToolRequest) map[string]any {
	if request.Caps == nil {
		return nil
	}
	return request.Caps.InitializeParams
}

// registerDevTools adds the dev_* introspection tools to the catalog. It is
// called only when dev tools are enabled; each entry carries DirectVisible so
// the curated loop surfaces it on tools/list.
func registerDevTools(catalog *ToolCatalog) {
	if catalog == nil {
		return
	}
	for _, desc := range devToolDescriptors() {
		catalog.Add(model.ToolEntryFromDescriptor(desc))
	}
}

// devToolDescriptors builds the descriptors for every dev tool. They are all
// read-only and directly visible.
func devToolDescriptors() []model.ToolDescriptor {
	desc := func(name, title, description string, handler model.PinnerToolHandler) model.ToolDescriptor {
		return model.ToolDescriptor{
			Name:          name,
			Title:         title,
			Description:   description,
			Category:      model.CategoryCore,
			ReadOnly:      true,
			DirectVisible: true,
			InputSchema:   json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			MCPTargets:    toolforge.MCPTargets(toolforge.Fallback(description)),
			Handler:       handler,
		}
	}

	return []model.ToolDescriptor{
		desc(
			"dev_host_env",
			"Dev: Host Environment",
			"Dump everything the MCP server observes about the CALLING MODEL HOST, as raw facts with no interpretation: the client implementation (name/title/version/description), the negotiated MCP protocol version, the full raw client capabilities, the raw initialize params, the OAuth token info attached to this call, and the HTTP request headers including User-Agent, plus the hostenv profile resolution (host type, transport, auth method, and enabled features). Over HTTP/OAuth transports the server's own process environment is unrelated to the remote host, so this reports only the signals the host advertises on the wire. Use it (with --dev-tools enabled) to identify which platform/agent is actually connected.",
			devHostEnvHandler,
		),
		desc(
			"dev_profile",
			"Dev: Resolved Host Profile",
			"Dump the resolved hostenv platform profile for THIS request: host type, transport kind, auth method, whether the client is remote or co-located, the client implementation, user-agent, negotiated protocol version, and the full set of enabled features. This is what the host-env detection decided for the current caller — useful to verify detection and feature gates without guessing.",
			devProfileHandler,
		),
		desc(
			"dev_request",
			"Dev: Request Echo",
			"Echo back what the server actually received for THIS invocation: the tool name, the raw arguments, whether the call was a retry carrying form elicitation input, and the negotiated protocol version. Use it to debug sessionless round-trips, argument coercion, or wizard/elicitation continuity.",
			devRequestHandler,
		),
	}
}

// devHostEnvHandler dumps the raw wire signal + resolved profile for the
// calling host. The raw snapshot is populated when dev tools are enabled; the
// profile resolution always applies.
func devHostEnvHandler(_ context.Context, request model.ToolRequest) (model.ToolResult, error) {
	profile := devProfileFromRequest(request)

	out := &devHostEnvOutput{
		ProtocolVersion: capsProtoVersion(request),
		HostType:        string(profile.HostType),
		Transport:       string(profile.Transport),
		AuthMethod:      string(profile.AuthMethod),
		Remote:          profile.Remote,
		Features:        enabledFeatures(profile),
		ClientInfo:      devClientInfoFrom(profile.ClientInfo),
		UserAgent:       devUserAgentFrom(profile),
		OAuthToken:      devTokenInfoFrom(profile.TokenInfo),
	}
	if cc := capsClientCapabilities(request); len(cc) > 0 {
		out.ClientCapabilities = cc
	}
	if ip := capsInitializeParams(request); len(ip) > 0 {
		out.InitializeParams = ip
	}
	if len(profile.Headers) > 0 {
		out.HTTPHeaders = map[string][]string(profile.Headers)
	}

	return devResult(out), nil
}

// devProfileHandler dumps the resolved profile for this request.
func devProfileHandler(_ context.Context, request model.ToolRequest) (model.ToolResult, error) {
	profile := devProfileFromRequest(request)

	out := &devProfileOutput{
		HostType:        string(profile.HostType),
		Transport:       string(profile.Transport),
		AuthMethod:      string(profile.AuthMethod),
		Remote:          profile.Remote,
		ProtocolVersion: profile.ProtocolVer,
		Features:        enabledFeatures(profile),
		ClientInfo:      devClientInfoFrom(profile.ClientInfo),
		UserAgent:       devUserAgentFrom(profile),
		TokenPresent:    profile.TokenInfo != nil,
	}

	return devResult(out), nil
}

// devRequestHandler echoes the invocation details the server received.
func devRequestHandler(_ context.Context, request model.ToolRequest) (model.ToolResult, error) {
	out := &devRequestOutput{
		Tool:            request.Name,
		Arguments:       request.Arguments,
		InputResponses:  request.InputResponses,
		ProtocolVersion: capsProtoVersion(request),
	}
	return devResult(out), nil
}

// devResult wraps a typed introspection payload as a combined text+structured
// tool result so both a text-only host and a structured host see the same data.
func devResult(payload any) model.ToolResult {
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return model.ToolResult{IsError: true, Text: "unable to serialize dev output"}
	}
	return model.ToolResult{Text: string(b), StructuredContent: payload}
}

// enabledFeatures returns the sorted list of feature names a profile enables.
func enabledFeatures(profile *hostenv.PlatformProfile) []string {
	if profile == nil {
		return nil
	}
	var feats []string
	for f, on := range profile.Features {
		if on {
			feats = append(feats, string(f))
		}
	}
	sort.Strings(feats)
	return feats
}
