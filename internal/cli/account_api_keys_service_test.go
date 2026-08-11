package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAPIKeyService(t *testing.T) {
	mockAuth := NewMockAuthService(t)
	svc := NewAPIKeyService(mockAuth, "test-token")
	require.NotNil(t, svc)
}

func TestDefaultAPIKeyServiceFactory(t *testing.T) {
	mockAuth := NewMockAuthService(t)
	svc := defaultAPIKeyServiceFactory(mockAuth, "test-token")
	require.NotNil(t, svc)
}

func TestAPIKeyServiceRequireAuthenticated(t *testing.T) {
	t.Run("authenticated with token", func(t *testing.T) {
		mockAuth := NewMockAuthService(t)
		svc := NewAPIKeyService(mockAuth, "test-token")
		err := svc.RequireAuthenticated()
		assert.NoError(t, err)
	})

	t.Run("not authenticated without token", func(t *testing.T) {
		mockAuth := NewMockAuthService(t)
		svc := NewAPIKeyService(mockAuth, "")
		err := svc.RequireAuthenticated()
		assert.Error(t, err)
		assert.Equal(t, ErrNotAuthenticated, err)
	})
}
