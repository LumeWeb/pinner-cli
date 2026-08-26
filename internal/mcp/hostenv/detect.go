package hostenv

import (
	"net/http"
)

// DetectRequest carries the raw wire signals extracted from an MCP
// request. The detector uses these to identify the connected platform.
// All fields are optional — a stdio request has no headers or tokenInfo.
type DetectRequest struct {
	// ClientInfo is the MCP clientInfo from initialize params or
	// per-request _meta. May be nil (e.g. Grok does not send clientInfo).
	ClientInfo *ClientInfo

	// ProtocolVersion is the MCP protocol version the client sent.
	ProtocolVersion string

	// UserAgent is the HTTP User-Agent header value, when available.
	UserAgent string

	// Headers are the raw HTTP headers from the request. Nil on stdio.
	Headers http.Header

	// TokenInfo is the OAuth bearer token info when the client
	// authenticated via OAuth. Nil otherwise.
	TokenInfo *TokenInfo

	// CoLocated is true when the server was started in stdio mode
	// (the client shares the host filesystem).
	CoLocated bool

	// TunnelOpenAI is true when the server uses the embedded OpenAI
	// tunnel transport (no reachable HTTP mux).
	TunnelOpenAI bool
}
