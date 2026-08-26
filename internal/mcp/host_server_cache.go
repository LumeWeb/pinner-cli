package mcp

import (
	"net/http"
	"sync"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"

	"go.uber.org/zap"
)

// hostServerCache lazily creates and caches MCP servers per detected
// HostType. It enables per-host tool presentation: each host can receive
// different tool descriptions, schemas, and visibility in tools/list,
// resolved by the forge from the host's PlatformProfile.
//
// For stdio transport, the transport is known at startup and only one
// profile applies. For HTTP transport, different hosts (OpenAI, Grok,
// Claude, generic) may connect to the same endpoint; the cache ensures
// each distinct host gets its own server with the right tool surface.
//
// The cache is keyed by HostType (not the full PlatformProfile) because
// tool presentation varies by host family, not by per-request details
// like token info or headers. Transport is fixed at startup.
type hostServerCache struct {
	mu       sync.RWMutex
	servers  map[hostenv.HostType]*sdk.Server
	factory  func(hostenv.PlatformProfile) *sdk.Server
	resolver *hostenv.DetectorRegistry
	log      *zap.Logger
}

// newHostServerCache creates a cache that resolves servers per detected
// host. factory builds a server for a given resolved host profile; it is
// called at most once per distinct HostType.
func newHostServerCache(factory func(hostenv.PlatformProfile) *sdk.Server, log *zap.Logger) *hostServerCache {
	return &hostServerCache{
		servers:  make(map[hostenv.HostType]*sdk.Server),
		factory:  factory,
		resolver: defaultDetectorRegistry,
		log:      log,
	}
}

// Get returns the server for the detected host, creating it via the
// factory if this is the first request from that host type. For stdio
// transport (no HTTP headers to inspect), the default server is returned
// directly.
func (c *hostServerCache) Get(r *http.Request) *sdk.Server {
	profile := c.detectProfile(r)
	c.mu.RLock()
	srv, ok := c.servers[profile.HostType]
	c.mu.RUnlock()
	if ok {
		return srv
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check after acquiring write lock.
	if srv, ok := c.servers[profile.HostType]; ok {
		return srv
	}
	srv = c.factory(profile)
	if srv == nil {
		// Factory returned nil — fall back to HostUnknown's server or
		// the first available.
		srv = c.fallback()
	}
	if srv != nil {
		c.servers[profile.HostType] = srv
	}
	if c.log != nil {
		c.log.Debug("resolved MCP server for host",
			zap.String("host", string(profile.HostType)),
			zap.String("transport", string(profile.Transport)),
			zap.String("user_agent", profile.UserAgent),
		)
	}
	return srv
}

// detectProfile resolves the PlatformProfile from the HTTP request's
// headers and the server's transport flags.
func (c *hostServerCache) detectProfile(r *http.Request) hostenv.PlatformProfile {
	return c.resolver.DetectFromHTTPRequest(
		r.Header,
		transportFlagsVar.coLocated,
		transportFlagsVar.tunnelOpenAI,
		nil,
	)
}

// fallback returns any cached server when the factory produces nil.
func (c *hostServerCache) fallback() *sdk.Server {
	for _, srv := range c.servers {
		return srv
	}
	return nil
}

// ServerGetter returns a function suitable for sdk.StreamableHTTPHandler's
// getServer parameter.
func (c *hostServerCache) ServerGetter() func(*http.Request) *sdk.Server {
	return c.Get
}
