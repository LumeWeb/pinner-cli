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
