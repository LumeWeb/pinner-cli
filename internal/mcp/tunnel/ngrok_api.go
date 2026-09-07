//go:build !no_tunnel

package tunnel

import (
	"context"
	"net/http"
	"time"

	ngrok "go.lumeweb.com/tunneler/ngrok"
)

// NgrokAccountType classifies an ngrok account by what its reserved-domain set
// actually contains (free vs paid), driving how the install resolves a public
// URL. It aliases the type from the extracted tunneler ngrok package.
type NgrokAccountType = ngrok.NgrokAccountType

const (
	// NgrokAccountUnknown means the account type could not be determined.
	NgrokAccountUnknown = ngrok.NgrokAccountUnknown
	// NgrokAccountFree is a free account: it has exactly one auto-assigned dev
	// domain on an *.ngrok-free.* suffix and no named/custom domains.
	NgrokAccountFree = ngrok.NgrokAccountFree
	// NgrokAccountPaid is a paid account: it may hold named *.ngrok.* domains
	// and/or user-owned (custom) hostnames.
	NgrokAccountPaid = ngrok.NgrokAccountPaid
)

// NgrokAPIHTTPClient is the HTTP client used for ngrok REST API calls by this
// package's ResolveNgrokPublicURL. It is a package variable so tests can
// substitute a stub transport without touching the network. The API requires
// the `ngrok-version: 2` header and a bearer API key (distinct from the
// authtoken). ResolveNgrokPublicURL installs this client into the tunneler
// library for the duration of each call, so stubbing it keeps affecting the
// delegated API client.
var NgrokAPIHTTPClient = &http.Client{Timeout: 15 * time.Second}

// ResolveNgrokPublicURL queries the ngrok REST API for the account's reserved
// domains and derives the public base URL for the MCP endpoint, along with the
// account type. prefer is the operator's requested domain (MCP_DOMAIN /
// --domain): when it matches a reserved domain it is honored first. It returns
// ("", NgrokAccountUnknown, nil) when the API key is empty (nothing to query)
// rather than an error, so callers fall back to a prompt; a non-empty key that
// the API rejects returns an error.
//
// It delegates to the extracted tunneler ngrok package, temporarily installing
// this package's NgrokAPIHTTPClient so stubs of the pinner var keep working.
func ResolveNgrokPublicURL(ctx context.Context, apiKey string, prefer string) (string, NgrokAccountType, error) {
	prev := ngrok.NgrokAPIHTTPClient
	ngrok.NgrokAPIHTTPClient = NgrokAPIHTTPClient
	defer func() { ngrok.NgrokAPIHTTPClient = prev }()
	url, accountType, err := ngrok.ResolveNgrokPublicURL(ctx, apiKey, prefer)
	return url, accountType, err
}
