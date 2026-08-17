package config

import (
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	z "github.com/Oudwins/zog"
	"github.com/docker/go-units"
	"go.lumeweb.com/configmanager"
	"go.lumeweb.com/configmanager/source"
)

// Config holds the runtime configuration for the pinner CLI.
type Config struct {
	// AuthToken is the JWT token for API authentication.
	AuthToken string `config:"auth_token" desc:"JWT authentication token for API access"`

	// BaseEndpoint is the base URL for the Pinner service (e.g., https://pinner.xyz).
	// Subdomains will be derived from this base endpoint (e.g., account.pinner.xyz, ipfs.pinner.xyz).
	// If empty, defaults to the production endpoint.
	BaseEndpoint string `config:"base_endpoint" desc:"Base URL for Pinner service (e.g., pinner.xyz)"`

	// MaxRetries is the maximum number of retry attempts for failed operations.
	MaxRetries int `config:"max_retries" desc:"Maximum retry attempts for failed operations (0-10)"`

	// MemoryLimit is the memory limit in megabytes for CAR generation.
	// This limits the amount of memory used during IPFS DAG construction.
	MemoryLimit uint64 `config:"memory_limit" desc:"Memory limit in MB for CAR generation (1-10240)"`

	// Secure indicates whether endpoints should use HTTPS. Defaults to true.
	// When false, HTTP will be used instead of HTTPS.
	Secure bool `config:"secure" desc:"Use HTTPS for endpoints (true/false)"`

	// GatewayEndpoint is the URL for the IPFS gateway (e.g., https://dweb.link).
	// If empty, defaults to the ipfs subdomain of the base endpoint.
	GatewayEndpoint string `config:"gateway_endpoint" desc:"IPFS gateway URL (e.g., https://dweb.link)"`

	DefaultTimeout time.Duration `config:"default_timeout" desc:"Default timeout for API operations (e.g., 30s, 1m)"`

	UploadTimeout time.Duration `config:"upload_timeout" desc:"Timeout for upload/download/benchmark operations (e.g., 5m, 10m)"`

	SyncTimeout time.Duration `config:"sync_timeout" desc:"Timeout for reconciliation/cleanup/sync operations (e.g., 1m, 2m)"`

	// MaxMCPUploadSize is the maximum allowed size in bytes for MCP file uploads.
	// Defaults to 1 GiB when unset.
	MaxMCPUploadSize uint64 `config:"max_mcp_upload_size" desc:"Max MCP upload size in bytes (default 1 GiB)"`

	// DownloadRoot confines MCP download_file / vault_get_file local-sink writes
	// to a single host directory. A caller-supplied output_path is resolved
	// relative to this root and rejected (with a traversal check) if it escapes
	// it, so a compromised MCP agent cannot overwrite arbitrary server files or
	// redirect decrypted vault/IPFS content elsewhere on the host. Defaults to
	// <config-dir>/downloads when unset.
	DownloadRoot string `config:"download_root" desc:"Host directory that confines MCP download local-sink writes (default <config-dir>/downloads)"`

	// Tunnels holds last-resort tunnel provider credentials. These are only used
	// when no external source (flag, provider env var, or provider config file)
	// supplies them. The config file is written 0600, so secrets stored here are
	// not world-readable. See TunnelCredential/SetTunnelCredential on Manager.
	Tunnels TunnelConfig `config:"tunnels" desc:"Tunnel provider credentials (last-resort store)"`
}

// TunnelConfig is the last-resort credential store for MCP tunnel providers.
// Each field maps to a stable config key under the "tunnels" namespace so the
// ResolveCredential chain can fall back to the config manager after flags, env,
// and provider config files.
type TunnelConfig struct {
	// NgrokToken is a last-resort ngrok authtoken.
	NgrokToken string `config:"ngrok_token" desc:"Last-resort ngrok authtoken"`
	// OpenAITunnelID is a last-resort OpenAI Secure MCP Tunnel ID.
	OpenAITunnelID string `config:"openai_tunnel_id" desc:"Last-resort OpenAI Secure MCP Tunnel ID"`
	// OpenAIAPIKey is a last-resort OpenAI runtime (control-plane) API key.
	OpenAIAPIKey string `config:"openai_api_key" desc:"Last-resort OpenAI runtime (control-plane) API key"`
}

// Tunnel config keys (namespace "tunnels").
const (
	ConfigKeyTunnelsNgrokToken     = "tunnels.ngrok_token"
	ConfigKeyTunnelsOpenAITunnelID = "tunnels.openai_tunnel_id"
	ConfigKeyTunnelsOpenAIAPIKey   = "tunnels.openai_api_key"
)

// TunnelCredentialKey maps a provider + logical key to its config-manager key.
// Supported (provider, key) pairs:
//
//	("ngrok", "token")  -> tunnels.ngrok_token
//	("openai", "tunnel_id") -> tunnels.openai_tunnel_id
//	("openai", "api_key")   -> tunnels.openai_api_key
//
// Returns "" for unknown pairs so callers fail fast rather than silently
// persisting to a misspelled key.
func TunnelCredentialKey(provider, key string) string {
	switch provider + "." + key {
	case "ngrok.token":
		return ConfigKeyTunnelsNgrokToken
	case "openai.tunnel_id":
		return ConfigKeyTunnelsOpenAITunnelID
	case "openai.api_key":
		return ConfigKeyTunnelsOpenAIAPIKey
	default:
		return ""
	}
}

// Config keys used throughout the package.
const (
	ConfigKeyAuthToken       = "auth_token"
	ConfigKeyBaseEndpoint    = "base_endpoint"
	ConfigKeyMaxRetries      = "max_retries"
	ConfigKeySecure          = "secure"
	ConfigKeyGatewayEndpoint = "gateway_endpoint"
)

const (
	ConfigKeyDefaultTimeout = "default_timeout"
	ConfigKeyUploadTimeout  = "upload_timeout"
	ConfigKeySyncTimeout    = "sync_timeout"
)

const (
	DefaultTimeoutSeconds       = 30
	DefaultUploadTimeoutSeconds = 300
	DefaultSyncTimeoutSeconds   = 60
)

// Subdomain constants for API endpoints.
const (
	SubdomainAccount = "account"
	SubdomainIPFS    = "ipfs"
	SubdomainAdmin   = "admin"
	SubdomainMeta    = "meta"
	SubdomainSia     = "sia"
)

// Default endpoint constants.
const (
	DefaultBaseDomain = "pinner.xyz"
	DefaultProtocol   = "https"
)

// Upload endpoint path constants.
const (
	UploadPath = "/api/upload"
	TUSPath    = "/api/upload/tus"
)

// DefaultMemoryLimitMB is the default memory limit in megabytes for CAR generation.
const DefaultMemoryLimitMB = 100

// DefaultMaxMCPUploadSize is the default maximum MCP upload size in bytes (1 GiB).
const DefaultMaxMCPUploadSize uint64 = 1 << 30

var _ configmanager.ConfigSchemaProvider = (*Config)(nil)
var _ source.ConfigDefaults = (*Config)(nil)

// NewConfig creates a new Config instance with default values.
func NewConfig() *Config {
	return &Config{
		MaxRetries: 3,
	}
}

func (c *Config) Schema() z.ZogSchema {
	return z.Struct(z.Shape{
		"AuthToken": z.String().Optional(),
		"BaseEndpoint": z.String().
			Min(1).
			Max(2048).
			Optional(),
		"MaxRetries": z.Int().
			GTE(0).
			LTE(10).
			Optional(),
		"MemoryLimit": z.UintLike[uint64]().
			GTE(1).
			LTE(10240).
			Optional(),
		"Secure": z.Bool().
			Optional(),
		"GatewayEndpoint": z.String().
			Min(1).
			Max(2048).
			Optional(),
		"DefaultTimeout": z.Int().GTE(1).LTE(3600).Optional(),
		"UploadTimeout":  z.Int().GTE(1).LTE(3600).Optional(),
		"SyncTimeout":    z.Int().GTE(1).LTE(3600).Optional(),
		"MaxMCPUploadSize": z.UintLike[uint64]().
			GTE(1).
			Optional(),
		"DownloadRoot": z.String().
			Max(4096).
			Optional(),
		"Tunnels": z.Struct(z.Shape{
			"NgrokToken":     z.String().Optional(),
			"OpenAITunnelID": z.String().Optional(),
			"OpenAIAPIKey":   z.String().Optional(),
		}).Optional(),
	})
}

func (c *Config) Defaults() map[string]any {
	return map[string]any{
		"MaxRetries":       3,
		"MemoryLimit":      DefaultMemoryLimitMB,
		"Secure":           true,
		"DefaultTimeout":   time.Duration(DefaultTimeoutSeconds) * time.Second,
		"UploadTimeout":    time.Duration(DefaultUploadTimeoutSeconds) * time.Second,
		"SyncTimeout":      time.Duration(DefaultSyncTimeoutSeconds) * time.Second,
		"MaxMCPUploadSize": DefaultMaxMCPUploadSize,
	}
}

func (c *Config) GetDefaultTimeout() time.Duration {
	if c.DefaultTimeout > 0 {
		return c.DefaultTimeout
	}
	return time.Duration(DefaultTimeoutSeconds) * time.Second
}

func (c *Config) GetUploadTimeout() time.Duration {
	if c.UploadTimeout > 0 {
		return c.UploadTimeout
	}
	return time.Duration(DefaultUploadTimeoutSeconds) * time.Second
}

func (c *Config) GetSyncTimeout() time.Duration {
	if c.SyncTimeout > 0 {
		return c.SyncTimeout
	}
	return time.Duration(DefaultSyncTimeoutSeconds) * time.Second
}

// GetMaxMCPUploadSize returns the maximum MCP upload size in bytes.
// If MaxMCPUploadSize is not set, returns the default (1 GiB).
func (c *Config) GetMaxMCPUploadSize() uint64 {
	if c.MaxMCPUploadSize == 0 {
		return DefaultMaxMCPUploadSize
	}
	return c.MaxMCPUploadSize
}

// DefaultDownloadRoot returns the host directory that confines MCP download
// local-sink writes. It is a "downloads" directory under the Pinner config
// root, so it honors PINNER_HOME and the OS-native config dir and is stable
// across the purely-Go and Docker/PaaS (PINNER_HOME=/data) deployments.
func DefaultDownloadRoot() string {
	return filepath.Join(PinnerConfigDir(), "downloads")
}

// GetDownloadRoot returns the directory that confines MCP download local-sink
// writes. When DownloadRoot is unset, returns the package default
// (<config-dir>/downloads). The value is returned verbatim (Clean is applied by
// the resolver); it is the operator's responsibility to set an absolute path.
func (c *Config) GetDownloadRoot() string {
	if c.DownloadRoot == "" {
		return DefaultDownloadRoot()
	}
	return c.DownloadRoot
}

// IsAuthenticated checks if the client has valid authentication credentials.
func (c *Config) IsAuthenticated() bool {
	return c.AuthToken != ""
}

// GetBaseEndpoint returns the configured base endpoint, falling back to the default.
func (c *Config) GetBaseEndpoint() string {
	if c.BaseEndpoint != "" {
		return ensureScheme(c.BaseEndpoint, c.Secure)
	}
	return buildEndpoint(DefaultProtocol, DefaultBaseDomain)
}

// GetBaseEndpointSecure returns the configured base endpoint using the secure flag.
func (c *Config) GetBaseEndpointSecure() string {
	if c.BaseEndpoint != "" {
		// If BaseEndpoint is set, parse it and rebuild with the secure flag
		_, domain := parseEndpoint(c.BaseEndpoint)
		return buildEndpointWithSecure(domain, c.Secure)
	}
	return buildEndpointWithSecure(DefaultBaseDomain, c.Secure)
}

// GetAccountEndpoint returns the account API endpoint (account subdomain).
func (c *Config) GetAccountEndpoint() string {
	return getSubdomainEndpoint(c.GetBaseEndpoint(), SubdomainAccount)
}

// GetAccountEndpointSecure returns the account API endpoint using the secure flag.
func (c *Config) GetAccountEndpointSecure() string {
	return getSubdomainEndpointWithProtocol(c.GetBaseEndpoint(), SubdomainAccount, c.Secure)
}

// GetIPFSEndpoint returns the IPFS pinning API endpoint (ipfs subdomain).
func (c *Config) GetIPFSEndpoint() string {
	return getSubdomainEndpoint(c.GetBaseEndpoint(), SubdomainIPFS)
}

// GetIPFSEndpointSecure returns the IPFS pinning API endpoint using the secure flag.
func (c *Config) GetIPFSEndpointSecure() string {
	return getSubdomainEndpointWithProtocol(c.GetBaseEndpoint(), SubdomainIPFS, c.Secure)
}

// GetUploadEndpoint returns the upload API endpoint.
func (c *Config) GetUploadEndpoint() string {
	return getSubdomainEndpoint(c.GetBaseEndpoint(), SubdomainIPFS) + UploadPath
}

// GetUploadEndpointSecure returns the upload API endpoint using the secure flag.
func (c *Config) GetUploadEndpointSecure() string {
	return getSubdomainEndpointWithProtocol(c.GetBaseEndpoint(), SubdomainIPFS, c.Secure) + UploadPath
}

// GetTUSEndpoint returns the TUS upload API endpoint.
func (c *Config) GetTUSEndpoint() string {
	return getSubdomainEndpoint(c.GetBaseEndpoint(), SubdomainIPFS) + TUSPath
}

// GetTUSEndpointSecure returns the TUS upload API endpoint using the secure flag.
func (c *Config) GetTUSEndpointSecure() string {
	return getSubdomainEndpointWithProtocol(c.GetBaseEndpoint(), SubdomainIPFS, c.Secure) + TUSPath
}

// GetAccountEndpointWithSecure returns the account API endpoint with a custom secure flag.
func (c *Config) GetAccountEndpointWithSecure(secure bool) string {
	return getSubdomainEndpointWithProtocol(c.GetBaseEndpoint(), SubdomainAccount, secure)
}

// GetIPFSEndpointWithSecure returns the IPFS pinning API endpoint with a custom secure flag.
func (c *Config) GetIPFSEndpointWithSecure(secure bool) string {
	return getSubdomainEndpointWithProtocol(c.GetBaseEndpoint(), SubdomainIPFS, secure)
}

// GetMetaEndpointWithSecure returns the meta API endpoint (meta subdomain) with a custom secure flag.
func (c *Config) GetMetaEndpointWithSecure(secure bool) string {
	return getSubdomainEndpointWithProtocol(c.GetBaseEndpoint(), SubdomainMeta, secure)
}

// GetGatewayEndpointWithSecure returns the IPFS gateway endpoint with a custom secure flag.
func (c *Config) GetGatewayEndpointWithSecure(secure bool) string {
	if c.GatewayEndpoint != "" {
		return ensureGatewayPath(c.GatewayEndpoint)
	}
	return getSubdomainEndpointWithProtocol(c.GetBaseEndpoint(), SubdomainIPFS, secure) + "/ipfs/"
}

// GetUploadEndpointWithSecure returns the upload API endpoint with a custom secure flag.
func (c *Config) GetUploadEndpointWithSecure(secure bool) string {
	return getSubdomainEndpointWithProtocol(c.GetBaseEndpoint(), SubdomainIPFS, secure) + UploadPath
}

// GetTUSEndpointWithSecure returns the TUS upload API endpoint with a custom secure flag.
func (c *Config) GetTUSEndpointWithSecure(secure bool) string {
	return getSubdomainEndpointWithProtocol(c.GetBaseEndpoint(), SubdomainIPFS, secure) + TUSPath
}

// GetAdminEndpoint returns the admin API endpoint (admin subdomain).
func (c *Config) GetAdminEndpoint() string {
	return getSubdomainEndpoint(c.GetBaseEndpoint(), SubdomainAdmin)
}

// GetAdminEndpointSecure returns the admin API endpoint using the secure flag.
func (c *Config) GetAdminEndpointSecure() string {
	return getSubdomainEndpointWithProtocol(c.GetBaseEndpoint(), SubdomainAdmin, c.Secure)
}

// GetAdminEndpointWithSecure returns the admin API endpoint with a custom secure flag.
func (c *Config) GetAdminEndpointWithSecure(secure bool) string {
	return getSubdomainEndpointWithProtocol(c.GetBaseEndpoint(), SubdomainAdmin, secure)
}

// GetSiaIndexerURL returns the Sia indexer URL derived from the portal domain.
// Convention: sia.{portal_domain} (e.g., https://sia.pinner.xyz). The scheme
// honors Config.Secure so a Secure:false (e.g. local http) indexer resolves to
// http:// rather than being hardcoded to https.
func (c *Config) GetSiaIndexerURL() string {
	return getSubdomainEndpointWithProtocol(c.GetBaseEndpoint(), SubdomainSia, c.Secure)
}

// GetGatewayEndpoint returns the IPFS gateway endpoint.
// If GatewayEndpoint is configured, it is used; otherwise, it falls back to the ipfs subdomain of the base endpoint.
func (c *Config) GetGatewayEndpoint() string {
	if c.GatewayEndpoint != "" {
		return ensureGatewayPath(c.GatewayEndpoint)
	}
	return getSubdomainEndpoint(c.GetBaseEndpoint(), SubdomainIPFS) + "/ipfs/"
}

// GetGatewayEndpointSecure returns the IPFS gateway endpoint using the secure flag.
// If GatewayEndpoint is configured, it is used; otherwise, it falls back to the ipfs subdomain of the base endpoint with the secure flag.
func (c *Config) GetGatewayEndpointSecure() string {
	if c.GatewayEndpoint != "" {
		return ensureGatewayPath(c.GatewayEndpoint)
	}
	return getSubdomainEndpointWithProtocol(c.GetBaseEndpoint(), SubdomainIPFS, c.Secure) + "/ipfs/"
}

// ensureGatewayPath ensures the gateway URL ends with "/ipfs/".
func ensureGatewayPath(gateway string) string {
	if strings.HasSuffix(gateway, "/ipfs/") {
		return gateway
	}
	gateway = strings.TrimSuffix(gateway, "/")
	return gateway + "/ipfs/"
}

// GetMemoryLimitBytes returns the memory limit in bytes for CAR generation.
// If MemoryLimit is not set, returns the default (100MB).
func (c *Config) GetMemoryLimitBytes() uint64 {
	if c.MemoryLimit == 0 {
		return uint64(DefaultMemoryLimitMB) * units.MiB
	}
	return c.MemoryLimit * units.MiB
}

// GetAPIEndpoint returns the configured API endpoint (deprecated: use GetAccountEndpoint).
// Maintained for backward compatibility.
func (c *Config) GetAPIEndpoint() string {
	return c.GetAccountEndpoint()
}

// ensureScheme prepends a scheme to an endpoint if it doesn't have one.
// url.Parse treats "localhost:8080" as scheme=localhost with empty host,
// so we detect that case by checking Opaque and host validity.
func ensureScheme(endpoint string, secure bool) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	if u.Scheme != "" && u.Host != "" {
		return endpoint
	}
	protocol := "http"
	if secure {
		protocol = "https"
	}
	return protocol + "://" + endpoint
}

// parseEndpoint extracts protocol and domain from an endpoint URL.
// Returns protocol (e.g., "https") and domain (e.g., "pinner.xyz").
func parseEndpoint(endpoint string) (protocol, domain string) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return DefaultProtocol, endpoint
	}
	if u.Scheme == "" {
		return DefaultProtocol, endpoint
	}
	if u.Host == "" {
		return DefaultProtocol, endpoint
	}
	return u.Scheme, u.Host
}

// buildEndpoint constructs a full URL from protocol and domain.
func buildEndpoint(protocol, domain string) string {
	if protocol == "" {
		protocol = DefaultProtocol
	}
	return protocol + "://" + domain
}

// buildEndpointWithSecure constructs a full URL from domain, using secure or insecure protocol.
func buildEndpointWithSecure(domain string, secure bool) string {
	protocol := "https"
	if !secure {
		protocol = "http"
	}
	return buildEndpoint(protocol, domain)
}

// getSubdomainEndpoint constructs a subdomain URL from the base endpoint.
func getSubdomainEndpoint(base, subdomain string) string {
	protocol, _ := parseEndpoint(base)
	secure := protocol == "https"
	return getSubdomainEndpointWithProtocol(base, subdomain, secure)
}

// getSubdomainEndpointWithProtocol constructs a subdomain URL from the base endpoint, using http or https based on the secure flag.
func getSubdomainEndpointWithProtocol(base, subdomain string, secure bool) string {
	_, domain := parseEndpoint(base)

	// Strip port from domain for subdomain construction
	host := domain
	port := ""
	if u, err := url.Parse("http://" + domain); err == nil {
		host = u.Hostname()
		if u.Port() != "" {
			port = ":" + u.Port()
		}
	}

	// For bare IPs (e.g. 127.0.0.1), subdomains are not resolvable; use the IP directly.
	// For localhost, subdomains are valid (e.g. account.localhost, ipfs.localhost).
	if net.ParseIP(host) != nil {
		return buildEndpointWithSecure(host+port, secure)
	}

	return buildEndpointWithSecure(subdomain+"."+host+port, secure)
}
