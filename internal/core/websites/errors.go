// Package websites provides website and domain-binding operations.
package websites

import (
	"fmt"
	"strings"
	"unicode"

	ipfs "go.lumeweb.com/ipfs-sdk"
)

// normalizeReason canonicalizes a backend reason code for comparison. The
// gateway emits Go/JSON-style enum values (e.g. "CidNotPinned") while the
// ipfs-sdk constants are SCREAMING_SNAKE ("CID_NOT_PINNED"); collapsing both to
// lowercase alphanumerics makes the translation robust to either wire format.
func normalizeReason(reason string) string {
	var b strings.Builder
	for _, r := range reason {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// TranslateError maps the ipfs-sdk backend reason codes that surface around
// website creation/update to clear, actionable messages. It is the single
// translation seam shared by every consumer (the CLI and the MCP tool-call
// path both invoke the same catalog website operations), so a readable message
// surfaces on both surfaces without duplicating the mapping.
//
// The original error is preserved with %w so errors.Is/errors.As still match
// the SDK sentinels (ErrNotFound, ErrGone, ...). Errors without a recognized
// reason code are returned unchanged so their original wording is preserved.
func TranslateError(err error) error {
	if err == nil {
		return nil
	}

	switch normalizeReason(ipfs.ErrorReasonOf(err)) {
	case normalizeReason(ipfs.ErrorCodeCIDNotPinned):
		return fmt.Errorf("target CID is not pinned on the gateway: pin it first (pins add / pins_add), then retry: %w", err)
	case normalizeReason(ipfs.ErrorCodeIPNSKeyNotFound):
		return fmt.Errorf("target IPNS key does not exist: create an IPNS key or target a pinned CID instead, then retry: %w", err)
	case normalizeReason(ipfs.ErrorCodeDNSValidationFailed):
		return fmt.Errorf("DNS validation failed for the website domain: the validation TXT and _dnslink records are not resolving on the domain; publish them, then validate again: %w", err)
	default:
		return err
	}
}

// TranslateErrorWithCID translates backend website errors like TranslateError
// but, for the CID_NOT_PINNED case, names the exact target CID and the precise
// recovery tool call so a caller (agent or user) pins the right content before
// retrying. Without the CID, an agent that already uploaded a few blobs can
// mistakenly pin the wrong one. Handlers that know the requested cid should
// pass it here instead of calling TranslateError directly. A non-empty cid
// only changes the CID_NOT_PINNED message; all other reason codes (and an
// empty cid) fall back to TranslateError so the shared mapping is preserved.
func TranslateErrorWithCID(err error, cid string) error {
	if err == nil {
		return nil
	}
	if cid != "" && normalizeReason(ipfs.ErrorReasonOf(err)) == normalizeReason(ipfs.ErrorCodeCIDNotPinned) {
		return fmt.Errorf("target CID %s is not pinned on the gateway: pin it first with pins_add(cids=[%q], wait=true), then retry the website operation: %w", cid, cid, err)
	}
	return TranslateError(err)
}
