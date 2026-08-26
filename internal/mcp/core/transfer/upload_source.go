package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/ieo"
)

// TransportKind describes which MCP transport the server runs under. It decides
// which file-input mechanism actually works: only one mechanism is real per
// transport, and the caller never picks it — registration and the resolver do.
type TransportKind string

const (
	// TransportStdio is co-located stdio/local mode. The caller shares the host
	// filesystem, so a host path can be read directly.
	TransportStdio TransportKind = "stdio"
	// TransportHTTP is remote HTTP or a real tunnel (ngrok/cloudflared) with a
	// reachable HTTP mux. A presigned HTTP PUT can be minted and the caller
	// curls bytes out of band.
	TransportHTTP TransportKind = "http"
	// TransportOpenAI is the embedded OpenAI Secure MCP Tunnel: pure MCP RPC
	// with no reachable HTTP mux. Bytes must travel through MCP RPC (a
	// server-fetchable URL or a data: URI).
	TransportOpenAI TransportKind = "openai"
)

// FileSourceMode enumerates the uniform source dialects the upload tools
// accept. Only the modes valid for the running transport are advertised, and
// the resolver rejects a mode the transport cannot support.
type FileSourceMode string

const (
	// SourcePath is a host-side file/directory/archive path on the MCP server
	// host. Valid in stdio (co-located) mode only, where the server reads it
	// directly.
	SourcePath FileSourceMode = "path"
	// SourceMint mints a one-time presigned HTTP PUT endpoint and returns its
	// URL; the caller streams bytes to it out of band (the curl path, and the
	// same mechanism the GUI app views use). Valid when a reachable HTTP mux
	// exists (HTTP / real tunnel).
	SourceMint FileSourceMode = "mint"
	// SourceURL is a server-fetchable HTTPS URL the server relays through its
	// SSRF-guarded transport. Valid on the OpenAI tunnel.
	SourceURL FileSourceMode = "url"
	// SourceData is an RFC 2397 data: URI (SEP-2356 wire form) the server
	// decodes locally. Valid on the OpenAI tunnel alongside SourceURL.
	SourceData FileSourceMode = "data"
)

// UploadSource is the single uniform file input shared by upload_file and
// vault_put_file. Exactly one mode is set; the other payload fields are
// ignored. The server routes to the real mechanism by transport; the caller
// never picks a mechanism, only a source voice.
type UploadSource struct {
	Mode FileSourceMode `json:"mode" jsonschema:"enum=path,enum=mint,enum=url,enum=data,description=Fallback transport only. Do not use if the file already exists as a host file accepted by the file parameter. The schema enum advertises which modes are valid on this server: path=host-side file path; mint=presigned curl PUT URL; url=server-fetchable HTTPS URL; data=RFC 2397 data URI."`
	Path string         `json:"path,omitempty" jsonschema:"description=Host-side path. Only for mode=path."`
	URL  string         `json:"url,omitempty" jsonschema:"description=Server-fetchable HTTPS URL. Only for mode=url (OpenAI tunnel)."`
	Data string         `json:"data,omitempty" jsonschema:"description=RFC 2397 data: URI. Only for mode=data (OpenAI tunnel)."`
}

// SourceModeEnumValues returns the UploadSource.mode string values that are
// valid for the given transport, in stable order. It is the single source of
// truth for both the published tool schema (via RewriteSourceModeEnum) and the
// advertised capabilities, so a supported mode is never absent from the schema
// and an unsupported mode is never surfaced to a model.
func SourceModeEnumValues(t TransportKind) []string {
	switch t {
	case TransportStdio:
		return []string{string(SourcePath)}
	case TransportHTTP:
		return []string{string(SourceMint)}
	case TransportOpenAI:
		return []string{string(SourceURL), string(SourceData)}
	default:
		// Unknown transport: advertise nothing. Production always passes one of
		// the three known kinds, so this only guards callers that misuse the
		// function (and matches capabilities' historic nil-for-unknown).
		return nil
	}
}

// RewriteSourceModeEnum rewrites the nested source.mode enum inside a tool
// schema that embeds an UploadSource so it advertises only the modes valid for
// transport t. The toolargs schema reflector (DoNotReference) inlines the
// UploadSource object as properties.source.properties.mode, which is exactly
// the shape this walks.
//
// Previously the schema was built statically and its enum carried only "path"
// on every transport (invopop/jsonschema parses jsonschema:"enum=path,mint,..."
// as just enum=path), so on an HTTP connection capabilities() reported
// ["mint"] while the published inputSchema still said enum ["path"] — a client
// could not legally pass mint. Rewriting the enum from the same source of
// truth as capabilities removes that drift. If the expected shape is not found
// the schema is returned unchanged so a structural change degrades to
// "static", never a panic.
func RewriteSourceModeEnum(schema json.RawMessage, t TransportKind) json.RawMessage {
	var root map[string]any
	if err := json.Unmarshal(schema, &root); err != nil {
		return schema
	}
	source, ok := root["properties"].(map[string]any)["source"].(map[string]any)
	if !ok {
		return schema
	}
	mode, ok := source["properties"].(map[string]any)["mode"].(map[string]any)
	if !ok {
		return schema
	}
	mode["enum"] = SourceModeEnumValues(t)
	out, err := json.Marshal(root)
	if err != nil {
		return schema
	}
	return out
}

// Available reports whether the source mode is usable on the given transport.
func (s UploadSource) Available(t TransportKind) bool {
	switch s.Mode {
	case SourcePath:
		return t == TransportStdio
	case SourceMint:
		return t == TransportHTTP
	case SourceURL, SourceData:
		return t == TransportOpenAI
	default:
		return false
	}
}

// Validate checks the source is coherent for the transport and that its
// payload field is populated for the declared mode.
func (s UploadSource) Validate(t TransportKind) error {
	switch s.Mode {
	case SourcePath:
		if s.Path == "" {
			return fmt.Errorf("source mode %q requires path", SourcePath)
		}
	case SourceMint:
		// Mint has no caller payload; the URL is derived server-side.
	case SourceURL:
		if s.URL == "" {
			return fmt.Errorf("source mode %q requires url", SourceURL)
		}
	case SourceData:
		if s.Data == "" {
			return fmt.Errorf("source mode %q requires data", SourceData)
		}
	default:
		return fmt.Errorf("unknown source mode %q", s.Mode)
	}
	if !s.Available(t) {
		return fmt.Errorf("source mode %q is not available on the %s transport", s.Mode, t)
	}
	return nil
}

// SourceResolver holds the byte-acquisition backends for the running transport
// and resolves a validated UploadSource into the real mechanism. Only the
// branches for the server's transport are non-nil.
type SourceResolver struct {
	// Transport is the transport this server runs under.
	Transport TransportKind
	// HTTPUpload mints presigned PUT URLs (SourceMint, HTTP transport).
	HTTPUpload *Upload
	// RelayAllowedHosts and RelayMaxBytes bound the SourceURL SSRF guard and
	// the SourceData size cap.
	RelayAllowedHosts []string
	RelayMaxBytes     int64
	// RelayHTTPClient, when set, is the deliberate-trust HTTP client used for
	// SourceURL fetches (mirrors FileRelayOptions.HTTPClient). When nil, the
	// SSRF-guarded default transport is used.
	RelayHTTPClient *http.Client
}

// MintURL returns the presigned PUT URL for SourceMint. Only valid on the HTTP
// transport.
func (r *SourceResolver) MintURL(s UploadSource, name string, ttl time.Duration) (string, error) {
	if err := s.Validate(r.Transport); err != nil {
		return "", err
	}
	if s.Mode != SourceMint {
		return "", fmt.Errorf("MintURL requires mode %q, got %q", SourceMint, s.Mode)
	}
	if r.HTTPUpload == nil {
		return "", fmt.Errorf("presigned upload endpoint is not configured for HTTP mode")
	}
	url := r.HTTPUpload.Mint(name, ttl)
	if url == "" {
		return "", errors.New("failed to mint one-time upload endpoint")
	}
	return url, nil
}

// OpenBytes opens a server-fetchable URL (SourceURL) or decodes a data: URI
// (SourceData) into an owned reader the caller must close. Valid on the OpenAI
// tunnel transport only. Returns the resolved base name and byte size.
func (r *SourceResolver) OpenBytes(ctx context.Context, s UploadSource) (body io.ReadCloser, size int64, name string, err error) {
	if err := s.Validate(r.Transport); err != nil {
		return nil, 0, "", err
	}
	maxBytes := r.RelayMaxBytes
	if maxBytes == 0 {
		maxBytes = ieo.EffectiveRelayMaxBytes(0)
	}
	switch s.Mode {
	case SourceURL:
		body, size, err = ieo.OpenFileURL(ctx, s.URL, ieo.FileRelayOptions{
			HTTPClient:     r.RelayHTTPClient,
			AllowedHosts:   r.RelayAllowedHosts,
			MaxBytes:       maxBytes,
			RequestTimeout: 2 * time.Minute,
		})
		if err != nil {
			return nil, 0, "", err
		}
		return body, size, RelayURLName(s.URL), nil
	case SourceData:
		reader, opt, derr := ieo.ParseFileDataURI(s.Data, maxBytes)
		if derr != nil {
			return nil, 0, "", derr
		}
		return io.NopCloser(reader), opt.Size, opt.Name, nil
	default:
		return nil, 0, "", fmt.Errorf("OpenBytes requires mode url or data, got %q", s.Mode)
	}
}

// RelayURLName derives a best-effort upload name from a fetchable URL path.
// Returns "" when the URL is not absolute or has no path component, so callers
// fall back to the default upload name.
func RelayURLName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || u.Scheme == "" {
		return ""
	}
	base := path.Base(u.Path)
	if base == "" || base == "." || base == "/" {
		return ""
	}
	return base
}
