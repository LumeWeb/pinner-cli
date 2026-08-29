package vault

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"go.sia.tech/core/types"
)

// AppID returns the deterministic app ID for pinner-cli.
func AppID() types.Hash256 {
	return types.HashBytes([]byte("pinner-cli"))
}

// DecodeAppKey parses a hex-encoded app key into a types.PrivateKey.
func DecodeAppKey(hexKey string) (types.PrivateKey, error) {
	keyBytes, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("invalid app key: %w", err)
	}
	return types.PrivateKey(keyBytes), nil
}

// resolveShareURL guards ShareAccept against SSRF by rewriting the
// agent-supplied share URL's scheme and host to the profile's configured
// indexer origin. The path, query, and fragment (which carries the encryption
// key) are preserved unchanged. This eliminates the need to validate-then-pass:
// the HTTP GET issued by app.Client.SharedObject always targets the trusted
// indexer, regardless of what host the agent supplied. When indexerURL carries
// no scheme, https is assumed (the default indexer transport).
func resolveShareURL(shareURL, indexerURL string) (string, error) {
	su, err := url.Parse(shareURL)
	if err != nil {
		return "", fmt.Errorf("invalid share URL: %w", err)
	}
	// When indexerURL carries no scheme, url.Parse treats the whole string as
	// a path (empty Host). Prepending "//" forces path-scheme parsing so the
	// hostname lands in Host where it belongs.
	if !strings.Contains(indexerURL, "://") {
		indexerURL = "//" + indexerURL
	}
	iu, err := url.Parse(indexerURL)
	if err != nil {
		return "", fmt.Errorf("invalid indexer URL: %w", err)
	}
	scheme := iu.Scheme
	if scheme == "" {
		scheme = "https"
	}
	su.Scheme = scheme
	su.Host = iu.Host
	return su.String(), nil
}
