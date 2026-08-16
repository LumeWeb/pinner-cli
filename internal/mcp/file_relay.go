package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultRelayMaxBytes = 512 << 20 // 512 MiB per-tool relay cap
	maxRelayRedirects    = 3
)

// effectiveRelayMaxBytes returns maxBytes if positive, else the package
// default relay cap. A maxBytes of 0 means "not configured", so callers that
// thread a config-driven cap through can keep the established 512 MiB default
// behavior when the option is unset.
func effectiveRelayMaxBytes(maxBytes int64) int64 {
	if maxBytes > 0 {
		return maxBytes
	}
	return int64(defaultRelayMaxBytes)
}

var (
	ErrInvalidFileReference = errors.New("invalid file reference")
	ErrFileTooLarge         = errors.New("file exceeds relay limit")
)

// ChatGPTFileReference is the file object ChatGPT supplies for an OpenAI file
// parameter. The temporary URL is fetched by the local MCP process; it is not
// passed to TUS or exposed to the Pinner API.
type ChatGPTFileReference struct {
	DownloadURL string `json:"download_url"`
	FileID      string `json:"file_id"`
	MIMEType    string `json:"mime_type,omitempty"`
	FileName    string `json:"file_name,omitempty"`
}

// FileRelayOptions constrains remote file retrieval.
type FileRelayOptions struct {
	HTTPClient     *http.Client
	AllowedHosts   []string
	MaxBytes       int64
	RequestTimeout time.Duration
}

// ValidateChatGPTFileReference validates the vendor OpenAI file object before
// any network request. Field-specific HTTPS/size constraints are delegated to
// the generic URL validator.
func ValidateChatGPTFileReference(ref ChatGPTFileReference, maxBytes int64) error {
	if strings.TrimSpace(ref.FileID) == "" {
		return fmt.Errorf("%w: file_id is required", ErrInvalidFileReference)
	}
	if strings.TrimSpace(ref.DownloadURL) == "" {
		return fmt.Errorf("%w: download_url is required", ErrInvalidFileReference)
	}
	if err := validateHTTPFileURL(ref.DownloadURL, maxBytes); err != nil {
		return fmt.Errorf("%w: download_url is invalid: %v", ErrInvalidFileReference, err)
	}
	if ref.FileName != "" {
		name := filepath.Base(ref.FileName)
		if name != ref.FileName || name == "." || name == ".." {
			return fmt.Errorf("%w: invalid file_name", ErrInvalidFileReference)
		}
	}
	return nil
}

// validateHTTPFileURL enforces the generic HTTPS-without-userinfo and size
// constraints shared by every relay URL (OpenAI files, agnostic URL upload).
// Returns nil when url is empty so vendor-specific "required" checks remain
// distinct.
func validateHTTPFileURL(rawURL string, maxBytes int64) error {
	if strings.TrimSpace(rawURL) == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("%w: must be an HTTPS URL without user info", ErrInvalidFileReference)
	}
	if maxBytes < 0 {
		return fmt.Errorf("%w: maxBytes must not be negative", ErrInvalidFileReference)
	}
	return nil
}

// OpenFileURL is the vendor-agnostic relay primitive. It fetches a public
// HTTPS URL with SSRF hardening (non-private IPs, redirect validation), a
// host allowlist, and a hard byte bound, returning a reader the caller owns.
// This is the reusable baseline for any HTTP-mode file input (OpenAI files,
// agnostic pinner_upload_url, or future draft MCP file staging).
func OpenFileURL(ctx context.Context, rawURL string, opts FileRelayOptions) (io.ReadCloser, int64, error) {
	maxBytes := opts.MaxBytes
	if maxBytes == 0 {
		maxBytes = defaultRelayMaxBytes
	}
	if err := validateHTTPFileURL(rawURL, maxBytes); err != nil {
		return nil, 0, err
	}
	u, _ := url.Parse(rawURL)
	if len(opts.AllowedHosts) > 0 && !hostAllowed(u.Hostname(), opts.AllowedHosts) {
		return nil, 0, fmt.Errorf("%w: download host %q is not on the allowed list; supply a URL whose host is allow-listed for this client (this download mechanism may not be supported for your client)", ErrInvalidFileReference, u.Hostname())
	}

	// SSRF defense-in-depth: never dial private / link-local IPs, even when the
	// hostname passes the string allowlist (mitigates DNS rebinding). The guard
	// is installed on Pinner-owned transports, which is the only path a remote
	// MCP caller can reach (callers supply only the URL, never an http.Client).
	// An explicitly injected HTTPClient is a deliberate trust decision by the
	// embedding Go code (tests, internal-service fetches), so its transport is
	// preserved; the redirect-hop SSRF check below still applies to it.
	client := opts.HTTPClient
	if client == nil {
		// The Client.Timeout bounds the whole exchange including reading the
		// response body — unlike the RequestTimeout context (which only spans
		// client.Do and returns after headers). Reuse the caller's budget so a
		// legitimately slow-but-in-budget download isn't cut off at a hardcoded
		// 30s.
		t := 30 * time.Second
		if opts.RequestTimeout > 0 {
			t = opts.RequestTimeout
		}
		client = &http.Client{Timeout: t}
		client.Transport = ssrfGuardedTransport()
	}
	client = cloneRelayClient(client, opts.AllowedHosts)

	reqCtx := ctx
	if opts.RequestTimeout > 0 {
		var cancel context.CancelFunc
		reqCtx, cancel = context.WithTimeout(ctx, opts.RequestTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create file download request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch file reference: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("fetch file reference: unexpected HTTP status %s", resp.Status)
	}
	if resp.ContentLength < 0 {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("%w: download size is unknown (%q did not advertise a Content-Length; only URLs with a known fixed size qualify, streaming/chunked responses are not supported)", ErrInvalidFileReference, rawURL)
	}
	if resp.ContentLength > maxBytes {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("%w: declared size %d exceeds %d", ErrFileTooLarge, resp.ContentLength, maxBytes)
	}
	return &limitedReadCloser{ReadCloser: resp.Body, remaining: maxBytes}, resp.ContentLength, nil
}

// OpenChatGPTFile is a thin vendor adapter: validate the OpenAI download_url
// shape, then delegate to the generic relay primitive.
func OpenChatGPTFile(ctx context.Context, ref ChatGPTFileReference, opts FileRelayOptions) (io.ReadCloser, int64, error) {
	maxBytes := opts.MaxBytes
	if maxBytes == 0 {
		maxBytes = defaultRelayMaxBytes
	}
	if err := ValidateChatGPTFileReference(ref, maxBytes); err != nil {
		return nil, 0, err
	}
	return OpenFileURL(ctx, ref.DownloadURL, opts)
}

type limitedReadCloser struct {
	io.ReadCloser
	remaining int64
}

func (r *limitedReadCloser) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.ReadCloser.Read(probe[:])
		if n > 0 {
			return 0, ErrFileTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.ReadCloser.Read(p)
	r.remaining -= int64(n)
	return n, err
}

func isPrivateIP(ip netip.Addr) bool {
	// Unmap IPv4-in-IPv6 addresses so the IPv4 range checks cover them. netip
	// handles the modern IPv4-mapped form (::ffff:a.b.c.d) internally, but the
	// deprecated IPv4-compatible form (::a.b.c.d, e.g. ::7f00:1 for 127.0.0.1)
	// is neither unmapped nor classed private/4, so it must be converted here or
	// a literal https://[::127.0.0.1]/ URL would slip through as public.
	if ip.Is4In6() {
		ip = netip.AddrFrom4(ip.As4())
	} else if isIPv4Compatible(ip) {
		// As4() panics on the IPv4-compatible form (Is4/Is4In6 are both false),
		// so extract the low 32 bits from the 16-byte form directly.
		b := ip.As16()
		var v4 [4]byte
		copy(v4[:], b[12:])
		ip = netip.AddrFrom4(v4)
	}
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if !ip.Is4() {
		return false
	}
	// Special-use / non-global ranges not classed private by netip. Denied so
	// a remote MCP caller cannot SSRF Pinner into fetching them.
	return inCIDR(ip, "100.64.0.0/10") || // RFC 6598 CGNAT
		inCIDR(ip, "192.0.0.0/24") || // IETF protocol assignments
		inCIDR(ip, "198.18.0.0/15") || // benchmark (RFC 2544)
		inCIDR(ip, "192.88.99.0/24") // IPv6-to-IPv4 relay (deprecated)
}

// isIPv4Compatible reports whether ip is the deprecated IPv4-compatible IPv6
// form (::a.b.c.d, the ::/96 prefix when the address is not ::/::1). Go's netip
// does not expose this via Is4In6 (which matches only the IPv4-mapped
// ::ffff:a.b.c.d form), so it is detected by a zero top-96-bits check.
func isIPv4Compatible(ip netip.Addr) bool {
	if !ip.Is6() {
		return false
	}
	b := ip.As16()
	for i := 0; i < 12; i++ {
		if b[i] != 0 {
			return false
		}
	}
	// Exclude the zero and loopback singlets (:: and ::1), which are classed by
	// netip directly but would otherwise match this zero-prefix test.
	return !ip.IsUnspecified() && !ip.IsLoopback()
}

func inCIDR(ip netip.Addr, cidr string) bool {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return false
	}
	return prefix.Contains(ip)
}

// resolvePublicIP returns a single usable public (non-private) address for a
// literal IP or hostname, preferring IPv4. It uses the stdlib resolver's
// LookupNetIP, which yields netip.Addr values directly, so the only local
// logic is the private-address classification and the v4-over-v6 preference.
func resolvePublicIP(ctx context.Context, host string) (netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		if isPrivateIP(ip) {
			return netip.Addr{}, fmt.Errorf("refusing to dial non-public address %s", ip)
		}
		return ip, nil
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("resolve %q: %w", host, err)
	}
	var ipv4, ipv6 netip.Addr
	for _, ip := range addrs {
		ip = ip.Unmap()
		if ip.Is4() && ipv4 == (netip.Addr{}) {
			ipv4 = ip
		}
		if ip.Is6() && ipv6 == (netip.Addr{}) {
			ipv6 = ip
		}
	}
	ip := ipv4
	if ip == (netip.Addr{}) {
		ip = ipv6
	}
	if ip == (netip.Addr{}) {
		return netip.Addr{}, fmt.Errorf("no usable address for %q", host)
	}
	if isPrivateIP(ip) {
		return netip.Addr{}, fmt.Errorf("refusing to dial non-public address %s", ip)
	}
	return ip, nil
}

func hostAllowed(host string, allowed []string) bool {
	for _, candidate := range allowed {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		host = strings.ToLower(host)
		if host == candidate || strings.HasSuffix(host, "."+candidate) {
			return true
		}
	}
	return false
}

// ssrfGuardedTransport returns a clone of http.DefaultTransport whose dial
// path rejects private / link-local addresses by resolving the checked public
// IP itself. It is installed on Pinner-owned clients so the relay's SSRF
// boundary is enforced for the default transport that every remote caller
// routes through.
func ssrfGuardedTransport() *http.Transport {
	base := http.DefaultTransport.(*http.Transport).Clone()
	// Never route through an HTTP proxy: when a proxy is set in the
	// environment, Go dials only the proxy address via CONNECT and the target
	// IP is never passed to DialContext, which would silently bypass the
	// private-IP guard below. The relay must connect directly so the checked
	// public IP is the one actually dialed.
	base.Proxy = nil
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid dial address %q: %w", addr, err)
		}
		ip, err := resolvePublicIP(ctx, host)
		if err != nil {
			return nil, err
		}
		// Dial the validated IP directly (not addr, the hostname) so the
		// address that was security-checked is the one connected to, closing
		// the DNS-rebinding window between check and dial.
		return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	return base
}

func cloneRelayClient(base *http.Client, allowed []string) *http.Client {
	clone := *base

	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRelayRedirects {
			return fmt.Errorf("too many file download redirects")
		}
		if req.URL.Scheme != "https" || (len(allowed) > 0 && !hostAllowed(req.URL.Hostname(), allowed)) {
			return fmt.Errorf("file download redirect is not allowed")
		}
		// Resolved-IP check on redirect hops closes the SSRF gap for transports
		// the caller injected (which may not carry the DialContext guard).
		if err := rejectPrivateHost(req.URL.Hostname()); err != nil {
			return err
		}
		return nil
	}
	return &clone
}

// rejectPrivateHost resolves a host and rejects it if it maps only to
// non-public addresses. It accepts a literal IP without a resolver call.
func rejectPrivateHost(host string) error {
	host = strings.TrimSuffix(host, ".")
	if ip, err := netip.ParseAddr(host); err == nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("refusing to reach non-public address %s", ip)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", host, err)
	}
	for _, addr := range addrs {
		if !isPrivateIP(addr.Unmap()) {
			return nil
		}
	}
	return fmt.Errorf("refusing to reach non-public address for %q", host)
}

var _ io.ReadCloser = (*limitedReadCloser)(nil)
