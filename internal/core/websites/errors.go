// Package websites provides website and domain-binding operations.
package websites

import (
	"fmt"

	ipfs "go.lumeweb.com/ipfs-sdk"
)

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

	switch ipfs.ErrorReasonOf(err) {
	case ipfs.ErrorCodeCIDNotPinned:
		return fmt.Errorf("target CID is not pinned on the gateway: pin it first (pins add / pins_add), then retry: %w", err)
	case ipfs.ErrorCodeIPNSKeyNotFound:
		return fmt.Errorf("target IPNS key does not exist: create an IPNS key or target a pinned CID instead, then retry: %w", err)
	case ipfs.ErrorCodeDNSValidationFailed:
		return fmt.Errorf("DNS validation failed for the website domain: the validation TXT and _dnslink records are not resolving on the domain; publish them, then validate again: %w", err)
	default:
		return err
	}
}
