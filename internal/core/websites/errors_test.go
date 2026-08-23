package websites

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// apiErr builds a minimal *ipfs.APIError wrapping base with the given reason,
// mirroring what ipfs-sdk v0.1.83 produces via withReason.
func apiErr(reason string, base error) error {
	return &ipfs.APIError{Reason: reason, Err: base}
}

func TestTranslateError(t *testing.T) {
	t.Run("nil passes through", func(t *testing.T) {
		assert.Nil(t, TranslateError(nil))
	})

	t.Run("unknown error passes through", func(t *testing.T) {
		base := errors.New("some generic failure")
		assert.Equal(t, base, TranslateError(base))
	})

	t.Run("CID_NOT_PINNED", func(t *testing.T) {
		base := errors.New("invalid website data")
		err := TranslateError(apiErr(ipfs.ErrorCodeCIDNotPinned, base))

		require.Error(t, err)
		msg := err.Error()
		assert.Contains(t, msg, "not pinned on the gateway")
		assert.Contains(t, msg, "pin it first")
		// Guidance distinguishes the post-upload eventual-consistency case from
		// a CID that was never uploaded through Pinner.
		assert.Contains(t, msg, "propagating")
		assert.Contains(t, msg, "retry")
		// The original chain is preserved for errors.Is.
		assert.ErrorIs(t, err, base)
	})

	t.Run("IPNS_KEY_NOT_FOUND", func(t *testing.T) {
		base := errors.New("invalid website data")
		err := TranslateError(apiErr(ipfs.ErrorCodeIPNSKeyNotFound, base))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "IPNS key does not exist")
		assert.ErrorIs(t, err, base)
	})

	t.Run("DNS_VALIDATION_FAILED", func(t *testing.T) {
		base := errors.New("validation failed")
		err := TranslateError(apiErr(ipfs.ErrorCodeDNSValidationFailed, base))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "DNS validation failed")
		assert.ErrorIs(t, err, base)
	})

	t.Run("PascalCase wire format (CidNotPinned)", func(t *testing.T) {
		// The gateway emits Go/JSON-style enum values; the translation must not
		// depend on the SDK's SCREAMING_SNAKE constants matching byte-for-byte.
		base := errors.New("invalid website data")
		err := TranslateError(apiErr("CidNotPinned", base))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not pinned on the gateway")
		assert.Contains(t, err.Error(), "pin it first")
		assert.ErrorIs(t, err, base)
	})

	t.Run("reason preserved even when wrapped by retry", func(t *testing.T) {
		// The sdk wraps APIError in retry.Unrecoverable; ErrorReasonOf still
		// resolves through errors.As, so the translation still applies.
		base := errors.New("invalid website data")
		wrapped := apiErr(ipfs.ErrorCodeCIDNotPinned, base)
		err := TranslateError(wrapped)
		assert.True(t, strings.Contains(err.Error(), "pin it first"))
	})
}

func TestTranslateErrorWithCID(t *testing.T) {
	const cid = "bafybeifsbbigcrgw4oiglcsy3xed4kockd464oufwybpa7wyuv7a4vhv5y"

	t.Run("nil passes through", func(t *testing.T) {
		assert.Nil(t, TranslateErrorWithCID(nil, cid))
	})

	t.Run("unknown error passes through", func(t *testing.T) {
		base := errors.New("some generic failure")
		assert.Equal(t, base, TranslateErrorWithCID(base, cid))
	})

	t.Run("CID_NOT_PINNED names the target CID and gives the exact pin call", func(t *testing.T) {
		base := errors.New("invalid website data")
		err := TranslateErrorWithCID(apiErr(ipfs.ErrorCodeCIDNotPinned, base), cid)

		require.Error(t, err)
		msg := err.Error()
		// The actionable guidance is unchanged, so shared surface tests still
		// match on these substrings.
		assert.Contains(t, msg, "not pinned on the gateway")
		assert.Contains(t, msg, "pin it first")
		// Post-upload propagation-lag guidance is present: retry rather than re-pin.
		assert.Contains(t, msg, "propagating")
		assert.Contains(t, msg, "retry the website operation instead of re-pinning")
		// The critical addition: the exact CID so the caller pins the right blob.
		assert.Contains(t, msg, cid)
		assert.Contains(t, msg, `pins_add(cids=[`+`"`+cid+`"`+`]`)
		// The original chain is preserved for errors.Is.
		assert.ErrorIs(t, err, base)
	})

	t.Run("PascalCase wire format (CidNotPinned) also names the CID", func(t *testing.T) {
		base := errors.New("invalid website data")
		err := TranslateErrorWithCID(apiErr("CidNotPinned", base), cid)
		require.Error(t, err)
		assert.Contains(t, err.Error(), cid)
		assert.ErrorIs(t, err, base)
	})

	t.Run("empty cid falls back to the generic translation", func(t *testing.T) {
		base := errors.New("invalid website data")
		err := TranslateErrorWithCID(apiErr(ipfs.ErrorCodeCIDNotPinned, base), "")
		require.Error(t, err)
		// The generic message does not name a CID.
		assert.NotContains(t, err.Error(), cid)
		assert.Contains(t, err.Error(), "not pinned on the gateway")
		assert.ErrorIs(t, err, base)
	})

	t.Run("non-CID reason still uses the shared map", func(t *testing.T) {
		base := errors.New("invalid website data")
		err := TranslateErrorWithCID(apiErr(ipfs.ErrorCodeIPNSKeyNotFound, base), cid)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "IPNS key does not exist")
		assert.ErrorIs(t, err, base)
	})
}
