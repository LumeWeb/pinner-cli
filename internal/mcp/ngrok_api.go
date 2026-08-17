package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ngrokAccountType classifies an ngrok account by what its reserved-domain set
// actually contains (free vs paid), driving how the install resolves a public
// URL.
type ngrokAccountType string

const (
	// ngrokAccountUnknown means the account type could not be determined.
	ngrokAccountUnknown ngrokAccountType = ""
	// ngrokAccountFree is a free account: it has exactly one auto-assigned dev
	// domain on an *.ngrok-free.* suffix and no named/custom domains.
	ngrokAccountFree ngrokAccountType = "free"
	// ngrokAccountPaid is a paid account: it may hold named *.ngrok.* domains
	// and/or user-owned (custom) hostnames.
	ngrokAccountPaid ngrokAccountType = "paid"
)

// ngrokReservedDomain mirrors the subset of the ngrok API's ReservedDomain
// object the installer reads.
type ngrokReservedDomain struct {
	// Domain is the reserved hostname (e.g. `you.ngrok-free.dev`,
	// `my-app.ngrok.app`, or a custom hostname).
	Domain string `json:"domain"`
	// CNAMETarget is set for a user-owned custom hostname and null for a
	// subdomain of an ngrok-owned base (e.g. *.ngrok.app, *.ngrok-free.dev).
	CNAMETarget *string `json:"cname_target"`
}

// ngrokReservedDomainList is the ngrok API's list response for
// GET /reserved_domains.
type ngrokReservedDomainList struct {
	ReservedDomains []ngrokReservedDomain `json:"reserved_domains"`
	NextPageURI     *string               `json:"next_page_uri"`
}

// ngrokAPIHTTPClient is the HTTP client used for ngrok REST API calls. It is a
// package variable so tests can substitute a stub transport without touching
// the network. The API requires the `ngrok-version: 2` header and a bearer API
// key (distinct from the authtoken).
var ngrokAPIHTTPClient = &http.Client{Timeout: 15 * time.Second}

// ngrokReservedDomains fetches the account's reserved domains from the ngrok
// REST API (https://api.ngrok.com/reserved_domains) using the given REST API
// key. It follows pagination up to a bounded number of pages. An empty non-nil
// collection is returned when the account has no reserved domains.
func ngrokReservedDomains(ctx context.Context, apiKey string) ([]ngrokReservedDomain, error) {
	var all []ngrokReservedDomain
	next := "/reserved_domains?limit=100"
	for page := 0; page < 10 && next != ""; page++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ngrok.com"+next, nil)
		if err != nil {
			return nil, fmt.Errorf("ngrok API: build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("ngrok-version", "2")

		resp, err := ngrokAPIHTTPClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("ngrok API: %w", err)
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if rerr != nil {
			return nil, fmt.Errorf("ngrok API: read response: %w", rerr)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, ngrokAPIError(resp.StatusCode, body)
		}
		var list ngrokReservedDomainList
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, fmt.Errorf("ngrok API: decode reserved_domains: %w", err)
		}
		if list.NextPageURI != nil {
			next = *list.NextPageURI
		} else {
			next = ""
		}
		all = append(all, list.ReservedDomains...)
	}
	if all == nil {
		all = []ngrokReservedDomain{}
	}
	return all, nil
}

// ngrokAPIError builds a readable error from a non-200 ngrok API response. The
// body is best-effort; a malformed body falls back to the status code.
func ngrokAPIError(status int, body []byte) error {
	var e struct {
		ErrorCode string `json:"error_code"`
		Msg       string `json:"msg"`
	}
	if err := json.Unmarshal(body, &e); err == nil && e.Msg != "" {
		code := e.ErrorCode
		if code == "" {
			code = fmt.Sprintf("HTTP %d", status)
		}
		return fmt.Errorf("ngrok API %s: %s", code, e.Msg)
	}
	return fmt.Errorf("ngrok API returned HTTP %d", status)
}

// isNgrokFreeDomain reports whether a reserved domain is a free-tier dev-domain
// subdomain (an *.ngrok-free.* base) as opposed to a named/custom domain.
func isNgrokFreeDomain(d ngrokReservedDomain) bool {
	return isDevDomainSuffix(d.Domain)
}

// isDevDomainSuffix reports whether host is a subdomain of an ngrok free-tier
// dev-domain base. Free accounts get exactly one such domain and nothing else.
func isDevDomainSuffix(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	for _, suffix := range []string{".ngrok-free.app", ".ngrok-free.dev", ".ngrok-free.pizza", ".ngrok-free.io"} {
		if strings.HasSuffix(h, suffix) {
			return true
		}
	}
	return false
}

// classifyNgrokAccount derives the account type from its reserved-domain set.
// A free account's set is its single *.ngrok-free.* dev domain; the presence of
// any named/custom (non-dev) domain marks a paid account.
func classifyNgrokAccount(domains []ngrokReservedDomain) ngrokAccountType {
	hasDev, hasNamed := false, false
	for _, d := range domains {
		if d.Domain == "" {
			continue
		}
		if isNgrokFreeDomain(d) {
			hasDev = true
		} else {
			hasNamed = true
		}
	}
	switch {
	case hasNamed:
		return ngrokAccountPaid
	case hasDev:
		return ngrokAccountFree
	default:
		return ngrokAccountUnknown
	}
}

// resolveNgrokPublicURL maps an ngrok account's reserved-domain set to the
// public base URL to advertise for the MCP server, with a stable deterministic
// preference:
//
//  1. A custom hostname the user explicitly requested via want (an MCP_DOMAIN
//     the operator chose); if present and in the set, honor it.
//  2. The account's free dev domain (the only public host a free account has);
//     this is the happy path that resolves the "no MCP_PUBLIC_URL" failure.
//  3. The first named domain (paid account with multiple).
//
// prefer, when non-empty, selects a domain of exact match first. It returns the
// chosen base URL ("https://<host>") or "" when the set yields nothing usable.
func resolveNgrokPublicURLFromDomains(domains []ngrokReservedDomain, prefer string) (string, ngrokAccountType) {
	accountType := classifyNgrokAccount(domains)
	if prefer != "" {
		for _, d := range domains {
			if d.Domain != "" && strings.EqualFold(d.Domain, bareHostname(prefer)) {
				return "https://" + d.Domain, accountType
			}
		}
	}
	// Prefer the free dev domain — the stable, account-bound host that works on
	// both free and (as an extra) paid accounts.
	for _, d := range domains {
		if isNgrokFreeDomain(d) && d.Domain != "" {
			return "https://" + d.Domain, accountType
		}
	}
	// Otherwise the first named/custom host.
	for _, d := range domains {
		if d.Domain != "" {
			return "https://" + d.Domain, accountType
		}
	}
	return "", accountType
}

// resolveNgrokPublicURL queries the ngrok REST API for the account's reserved
// domains and derives the public base URL for the MCP endpoint, along with the
// account type. prefer is the operator's requested domain (MCP_DOMAIN /
// --domain): when it matches a reserved domain it is honored first. It returns
// ("", ngrokAccountUnknown, nil) when the API key is empty (nothing to query)
// rather than an error, so callers fall back to a prompt; a non-empty key that
// the API rejects returns an error.
func resolveNgrokPublicURL(ctx context.Context, apiKey string, prefer string) (string, ngrokAccountType, error) {
	if strings.TrimSpace(apiKey) == "" {
		return "", ngrokAccountUnknown, nil
	}
	domains, err := ngrokReservedDomains(ctx, strings.TrimSpace(apiKey))
	if err != nil {
		return "", ngrokAccountUnknown, err
	}
	url, t := resolveNgrokPublicURLFromDomains(domains, prefer)
	return url, t, nil
}
