package cli

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWrapNetworkError(t *testing.T) {
	t.Run("nil error returns nil", func(t *testing.T) {
		result := WrapNetworkError("upload", nil)
		assert.Nil(t, result)
	})

	t.Run("wraps error with operation context", func(t *testing.T) {
		err := errors.New("connection refused")
		result := WrapNetworkError("upload", err)
		assert.Contains(t, result.Error(), "upload failed")
		assert.Contains(t, result.Error(), "connection refused")
		assert.Contains(t, result.Error(), "Check your internet connection")
	})
}

func TestIsBoxoAuthError(t *testing.T) {
	t.Run("nil error returns false", func(t *testing.T) {
		assert.False(t, isBoxoAuthError(nil))
	})

	t.Run("401 error matches", func(t *testing.T) {
		err := errors.New("remote pinning service returned http error 401: unauthorized")
		assert.True(t, isBoxoAuthError(err))
	})

	t.Run("non-auth error does not match", func(t *testing.T) {
		err := errors.New("connection refused")
		assert.False(t, isBoxoAuthError(err))
	})
}

func TestWrapPinningError(t *testing.T) {
	t.Run("nil error returns nil", func(t *testing.T) {
		result := wrapPinningError("pin", nil, ErrNotAuthenticated)
		assert.Nil(t, result)
	})

	t.Run("auth error wraps with auth message", func(t *testing.T) {
		err := errors.New("remote pinning service returned http error 401: unauthorized")
		result := wrapPinningError("pin", err, ErrNotAuthenticated)
		assert.Contains(t, result.Error(), "authentication expired")
	})

	t.Run("non-auth error wraps with operation", func(t *testing.T) {
		err := errors.New("timeout")
		result := wrapPinningError("pin", err, ErrNotAuthenticated)
		assert.Contains(t, result.Error(), "pin failed")
		assert.Contains(t, result.Error(), "timeout")
	})
}

func TestWithAuthToken(t *testing.T) {
	opt := WithAuthToken("my-token")
	s := &PinningServiceDefault{}
	opt(s)
	assert.Equal(t, "my-token", s.authToken)
}
