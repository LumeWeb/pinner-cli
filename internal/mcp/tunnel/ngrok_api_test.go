//go:build !no_tunnel

package tunnel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubNgrokAPI returns an *http.Client that routes api.ngrok.com requests to a
// handler scripted by handler, letting tests exercise the reserved_domains
// client through the shim's ResolveNgrokPublicURL without network access.
func stubNgrokAPI(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	orig := NgrokAPIHTTPClient
	NgrokAPIHTTPClient = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
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
	t.Cleanup(func() { NgrokAPIHTTPClient = orig })
	return NgrokAPIHTTPClient
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestNgrokReservedDomainsParsesList guards the reserved_domains client path
// (exercised through the shim's delegated ResolveNgrokPublicURL): a well-formed
// list response must pass the API key through as a Bearer token and yield the
// free dev domain of the account.
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
	url, typ, err := ResolveNgrokPublicURL(context.Background(), "ngrok_api_key_123", "")
	require.NoError(t, err)
	require.Equal(t, "Bearer ngrok_api_key_123", gotAuth)
	// The custom hostname marks the account paid; the free dev domain is still
	// preferred as the stable public host.
	require.Equal(t, NgrokAccountPaid, typ)
	require.Equal(t, "https://you.ngrok-free.dev", url)
}

// TestNgrokReservedDomainsSurfacesAPIError guards that a rejected/invalid API
// key produces a readable error rather than a silent empty result.
func TestNgrokReservedDomainsSurfacesAPIError(t *testing.T) {
	stubNgrokAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error_code":"ERR_NGROK_200","msg":"requires authorization"}`))
	})
	_, _, err := ResolveNgrokPublicURL(context.Background(), "bad_key", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ERR_NGROK_200")
}

// TestClassifyNgrokAccountAndResolve guards account identification + URL
// derivation through the delegated resolver: a free account's single
// *.ngrok-free.* dev domain yields the free type and that dev URL; a
// named/custom domain marks a paid account; an empty set is unknown with no
// URL.
func TestClassifyNgrokAccountAndResolve(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantT   NgrokAccountType
		wantURL string
	}{
		{
			name:    "free dev domain",
			body:    `{"reserved_domains":[{"id":"rd_1","domain":"you.ngrok-free.dev","cname_target":null}],"next_page_uri":null}`,
			wantT:   NgrokAccountFree,
			wantURL: "https://you.ngrok-free.dev",
		},
		{
			name:    "named ngrok domain is paid",
			body:    `{"reserved_domains":[{"id":"rd_1","domain":"my-app.ngrok.app","cname_target":"tunnel.ngrok.io"}],"next_page_uri":null}`,
			wantT:   NgrokAccountPaid,
			wantURL: "https://my-app.ngrok.app",
		},
		{
			name:    "custom hostname is paid",
			body:    `{"reserved_domains":[{"id":"rd_1","domain":"app.example.com","cname_target":"cname.example.com"}],"next_page_uri":null}`,
			wantT:   NgrokAccountPaid,
			wantURL: "https://app.example.com",
		},
		{
			name:    "free dev preferred over named when both present",
			body:    `{"reserved_domains":[{"id":"rd_1","domain":"custom.ngrok.app","cname_target":"x"},{"id":"rd_2","domain":"you.ngrok-free.dev","cname_target":null}],"next_page_uri":null}`,
			wantT:   NgrokAccountPaid, // presence of a named domain -> paid account
			wantURL: "https://you.ngrok-free.dev",
		},
		{
			name:    "empty set",
			body:    `{"reserved_domains":[],"next_page_uri":null}`,
			wantT:   NgrokAccountUnknown,
			wantURL: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stubNgrokAPI(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(c.body))
			})
			url, typ, err := ResolveNgrokPublicURL(context.Background(), "ngrok_key", "")
			require.NoError(t, err)
			require.Equal(t, c.wantT, typ, "account classification")
			require.Equal(t, c.wantURL, url, "resolved public URL")
		})
	}
}

// TestResolveNgrokPublicURLNoKeyIsUnambiguous guards that an empty API key is
// treated as "nothing to query" (no error, unknown account), so callers fall
// back to prompting instead of failing the install.
func TestResolveNgrokPublicURLNoKeyIsUnambiguous(t *testing.T) {
	url, typ, err := ResolveNgrokPublicURL(context.Background(), "   ", "")
	require.NoError(t, err)
	require.Equal(t, "", url)
	require.Equal(t, NgrokAccountUnknown, typ)
}

// TestResolveNgrokPublicURLPreferDomain guards Kody finding (paid user's custom
// --domain must win): when the operator requested a specific domain (MCP_DOMAIN
// / --domain) and it exists in the reserved-domain set — even alongside the
// account's free dev domain — the custom hostname is honored as the public URL
// instead of the free dev domain. Previously ResolveNgrokPublicURL hardcoded
// prefer="", so the operator's explicit choice was silently dropped.
func TestResolveNgrokPublicURLPreferDomain(t *testing.T) {
	stubNgrokAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"reserved_domains":[
			{"id":"rd_1","domain":"my-app.ngrok.app","cname_target":"tunnel.ngrok.io"},
			{"id":"rd_2","domain":"you.ngrok-free.dev","cname_target":null}
		],"next_page_uri":null}`))
	})
	url, typ, err := ResolveNgrokPublicURL(context.Background(), "ngrok_key", "my-app.ngrok.app")
	require.NoError(t, err)
	require.Equal(t, "https://my-app.ngrok.app", url,
		"operator's chosen --domain must be preferred over the free dev domain")
	require.Equal(t, NgrokAccountPaid, typ)

	// A free account holding only its dev domain, with no preferred domain set,
	// still resolves to the dev domain.
	url, _, err = ResolveNgrokPublicURL(context.Background(), "ngrok_key", "")
	require.NoError(t, err)
	require.Equal(t, "https://you.ngrok-free.dev", url)
}
