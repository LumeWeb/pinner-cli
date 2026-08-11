package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
)

// TestApplyDefaultTimeout verifies the shared legacy per-call deadline helper
// that every catalog wiring adapter now uses to bound catalog operations.
func TestApplyDefaultTimeout(t *testing.T) {
	t.Run("applies deadline from configured default timeout", func(t *testing.T) {
		cfg := &config.Config{DefaultTimeout: 2 * time.Second}
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(cfg)

		prev := configManagerFactory
		configManagerFactory = func() (config.Manager, error) { return cfgMgr, nil }
		defer func() { configManagerFactory = prev }()

		dctx, cancel := applyDefaultTimeout(context.Background())
		defer cancel()

		deadline, ok := dctx.Deadline()
		require.True(t, ok, "expected a deadline on the derived context")
		remaining := time.Until(deadline)
		assert.Greater(t, remaining, time.Duration(0), "deadline should be in the future")
		assert.LessOrEqual(t, remaining, 2*time.Second, "deadline should honor the configured timeout")
	})

	t.Run("no-op when factory errors", func(t *testing.T) {
		prev := configManagerFactory
		configManagerFactory = func() (config.Manager, error) { return nil, errors.New("boom") }
		defer func() { configManagerFactory = prev }()

		dctx, cancel := applyDefaultTimeout(context.Background())
		defer cancel()

		_, ok := dctx.Deadline()
		assert.False(t, ok, "no deadline should be applied when the config factory fails")
	})

	t.Run("no-op when factory returns nil manager", func(t *testing.T) {
		prev := configManagerFactory
		configManagerFactory = func() (config.Manager, error) { return nil, nil }
		defer func() { configManagerFactory = prev }()

		dctx, cancel := applyDefaultTimeout(context.Background())
		defer cancel()

		_, ok := dctx.Deadline()
		assert.False(t, ok, "no deadline should be applied for a nil manager")
	})
}
