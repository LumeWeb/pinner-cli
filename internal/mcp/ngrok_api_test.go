package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubNgrokAPI returns an *http.Client that routes api.ngrok.com requests to a
// handler scripted by handler, letting tests exercise the reserved_domains
// client without network access.
func stubNgrokAPI(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	orig := ngrokAPIHTTPClient
	ngrokAPIHTTPClient = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		// Rewrite the absolute api.ngrok.com URL to the test server.
		u := *r.URL
		u.Scheme = "http"
		u.Host = srv.Listener.Addr().String()
		r2 := r.Clone(r.Context())
		r2.URL = &u
		rr := httptest.NewRecorder()
		handler(rr, r2)
		return rr.Result(), nil
	})}
	t.Cleanup(func() { ngrokAPIHTTPClient = orig })
	return ngrokAPIHTTPClient
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestNgrokReservedDomainsParsesList guards the reserved_domains client: a
// well-formed list response must decode into domains and pass the API key
// through as a Bearer token.
func TestNgrokReservedDomainsParsesList(t *testing.T) {
	var gotAuth string
	stubNgrokAPI(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		require.Equal(t, "2", r.Header.Get("ngrok-version"), "API calls must set ngrok-version: 2")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"reserved_domains":[
			{"id":"rd_1","domain":"you.ngrok-free.dev","cname_target":null},
			{"id":"rd_2","domain":"app.example.com","cname_target":"cname.example.com"}
		],"next_page_uri":null}`))
	})
	domains, err := ngrokReservedDomains(context.Background(), "ngrok_api_key_123")
	require.NoError(t, err)
	require.Equal(t, "Bearer ngrok_api_key_123", gotAuth)
	require.Len(t, domains, 2)
	require.Equal(t, "you.ngrok-free.dev", domains[0].Domain)
	require.Nil(t, domains[0].CNAMETarget)
	require.NotNil(t, domains[1].CNAMETarget)
}

// TestNgrokReservedDomainsSurfacesAPIError guards that a rejected/invalid API
// key produces a readable error rather than a silent empty result.
func TestNgrokReservedDomainsSurfacesAPIError(t *testing.T) {
	stubNgrokAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error_code":"ERR_NGROK_200","msg":"requires authorization"}`))
	})
	_, err := ngrokReservedDomains(context.Background(), "bad_key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ERR_NGROK_200")
}

// TestClassifyNgrokAccountAndResolve guards account identification + URL
// derivation: a free account's single *.ngrok-free.* dev domain yields the free
// type and that dev URL; a named/custom domain marks a paid account; an empty
// set is unknown with no URL.
func TestClassifyNgrokAccountAndResolve(t *testing.T) {
	cases := []struct {
		name    string
		domains []ngrokReservedDomain
		wantT   ngrokAccountType
		wantURL string
	}{
		{
			name:    "free dev domain",
			domains: []ngrokReservedDomain{{Domain: "you.ngrok-free.dev"}},
			wantT:   ngrokAccountFree,
			wantURL: "https://you.ngrok-free.dev",
		},
		{
			name:    "named ngrok domain is paid",
			domains: []ngrokReservedDomain{{Domain: "my-app.ngrok.app", CNAMETarget: str("tunnel.ngrok.io")}},
			wantT:   ngrokAccountPaid,
			wantURL: "https://my-app.ngrok.app",
		},
		{
			name:    "custom hostname is paid",
			domains: []ngrokReservedDomain{{Domain: "app.example.com", CNAMETarget: str("cname.example.com")}},
			wantT:   ngrokAccountPaid,
			wantURL: "https://app.example.com",
		},
		{
			name:    "free dev preferred over named when both present",
			domains: []ngrokReservedDomain{{Domain: "custom.ngrok.app", CNAMETarget: str("x")}, {Domain: "you.ngrok-free.dev"}},
			wantT:   ngrokAccountPaid, // presence of a named domain -> paid account
			wantURL: "https://you.ngrok-free.dev",
		},
		{
			name:    "empty set",
			domains: []ngrokReservedDomain{},
			wantT:   ngrokAccountUnknown,
			wantURL: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			typ := classifyNgrokAccount(c.domains)
			require.Equal(t, c.wantT, typ, "account classification")
			url, _ := resolveNgrokPublicURLFromDomains(c.domains, "")
			require.Equal(t, c.wantURL, url, "resolved public URL")
		})
	}
}

// TestResolveNgrokPublicURLNoKeyIsUnambiguous guards that an empty API key is
// treated as "nothing to query" (no error, unknown account), so callers fall
// back to prompting instead of failing the install.
func TestResolveNgrokPublicURLNoKeyIsUnambiguous(t *testing.T) {
	url, typ, err := resolveNgrokPublicURL(context.Background(), "   ", "")
	require.NoError(t, err)
	require.Equal(t, "", url)
	require.Equal(t, ngrokAccountUnknown, typ)
}

// TestResolveNgrokPublicURLPreferDomain guards Kody finding (paid user's custom
// --domain must win): when the operator requested a specific domain (MCP_DOMAIN
// / --domain) and it exists in the reserved-domain set — even alongside the
// account's free dev domain — the custom hostname is honored as the public URL
// instead of the free dev domain. Previously resolveNgrokPublicURL hardcoded
// prefer="", so the operator's explicit choice was silently dropped.
func TestResolveNgrokPublicURLPreferDomain(t *testing.T) {
	stubNgrokAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"reserved_domains":[
			{"id":"rd_1","domain":"my-app.ngrok.app","cname_target":"tunnel.ngrok.io"},
			{"id":"rd_2","domain":"you.ngrok-free.dev","cname_target":null}
		],"next_page_uri":null}`))
	})
	url, typ, err := resolveNgrokPublicURL(context.Background(), "ngrok_key", "my-app.ngrok.app")
	require.NoError(t, err)
	require.Equal(t, "https://my-app.ngrok.app", url,
		"operator's chosen --domain must be preferred over the free dev domain")
	require.Equal(t, ngrokAccountPaid, typ)

	// A free account holding only its dev domain, with no preferred domain set,
	// still resolves to the dev domain.
	url, _, err = resolveNgrokPublicURL(context.Background(), "ngrok_key", "")
	require.NoError(t, err)
	require.Equal(t, "https://you.ngrok-free.dev", url)
}

func str(s string) *string { return &s }
