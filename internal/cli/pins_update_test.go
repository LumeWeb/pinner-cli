package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
)

func TestPinsUpdate(t *testing.T) {
	t.Run("returns error when cid is missing", func(t *testing.T) {
		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(nil)

		cmd := newMockCommand().
			withIsSet(FlagName, true)

		output := newTestOutput()
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			Secure:       true,
			BaseEndpoint: "pinner.xyz",
			AuthToken:    "test-token",
			MaxRetries:   3,
		}).Maybe()
		pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgr, "", true, pinningServiceFactory)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cid is required")
	})

	t.Run("returns error when no update fields provided", func(t *testing.T) {
		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(nil)

		cmd := newMockCommand().
			withCID("QmTest")

		output := newTestOutput()
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			Secure:       true,
			BaseEndpoint: "pinner.xyz",
			AuthToken:    "test-token",
			MaxRetries:   3,
		}).Maybe()
		pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgr, "", true, pinningServiceFactory)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least one field must be provided for update")
	})

	t.Run("returns error for invalid meta pair", func(t *testing.T) {
		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(nil)

		cmd := newMockCommand().
			withCID("QmTest").
			withStringSlice(FlagMeta, []string{"invalid"}).
			withIsSet(FlagMeta, true)

		output := newTestOutput()
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			Secure:       true,
			BaseEndpoint: "pinner.xyz",
			AuthToken:    "test-token",
			MaxRetries:   3,
		}).Maybe()
		pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgr, "", true, pinningServiceFactory)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected key=value format")
	})

	t.Run("returns error when not authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{})

		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)

		cmd := newMockCommand().
			withCID("QmTest").
			withString(FlagName, "renamed").
			withIsSet(FlagName, true)

		output := newTestOutput()
		pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgr, "", true, pinningServiceFactory)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotAuthenticated))
	})

	t.Run("updates name successfully", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: "test-token",
		})

		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(nil)
		service.EXPECT().UpdatePin(mock.Anything, "QmTest", "renamed", []string(nil), false).Return(nil)

		cmd := newMockCommand().
			withCID("QmTest").
			withString(FlagName, "renamed").
			withIsSet(FlagName, true)

		output := newTestOutput()
		pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgr, "", true, pinningServiceFactory)
		assert.NoError(t, err)
	})

	t.Run("updates metadata successfully", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: "test-token",
		})

		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(nil)
		service.EXPECT().UpdatePin(mock.Anything, "QmTest", "", []string{"env", "prod"}, false).Return(nil)

		cmd := newMockCommand().
			withCID("QmTest").
			withStringSlice(FlagMeta, []string{"env=prod"}).
			withIsSet(FlagMeta, true)

		output := newTestOutput()
		pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgr, "", true, pinningServiceFactory)
		assert.NoError(t, err)
	})

	t.Run("clears metadata successfully", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: "test-token",
		})

		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(nil)
		service.EXPECT().UpdatePin(mock.Anything, "QmTest", "", []string(nil), true).Return(nil)

		cmd := newMockCommand().
			withCID("QmTest").
			withBool(FlagClearMeta, true).
			withIsSet(FlagClearMeta, true)

		output := newTestOutput()
		pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgr, "", true, pinningServiceFactory)
		assert.NoError(t, err)
	})

	t.Run("clears and sets metadata successfully", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: "test-token",
		})

		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(nil)
		service.EXPECT().UpdatePin(mock.Anything, "QmTest", "", []string{"fresh", "start"}, true).Return(nil)

		cmd := newMockCommand().
			withCID("QmTest").
			withStringSlice(FlagMeta, []string{"fresh=start"}).
			withBool(FlagClearMeta, true).
			withIsSet(FlagClearMeta, true).
			withIsSet(FlagMeta, true)

		output := newTestOutput()
		pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgr, "", true, pinningServiceFactory)
		assert.NoError(t, err)
	})

	t.Run("updates name and metadata successfully", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: "test-token",
		})

		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(nil)
		service.EXPECT().UpdatePin(mock.Anything, "QmTest", "renamed", []string{"env", "prod"}, false).Return(nil)

		cmd := newMockCommand().
			withCID("QmTest").
			withString(FlagName, "renamed").
			withStringSlice(FlagMeta, []string{"env=prod"}).
			withIsSet(FlagName, true).
			withIsSet(FlagMeta, true)

		output := newTestOutput()
		pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgr, "", true, pinningServiceFactory)
		assert.NoError(t, err)
	})

	t.Run("dry run shows preview", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken:    "test-token",
			BaseEndpoint: "pinner.xyz",
			Secure:       true,
		})

		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(nil)

		cmd := newMockCommand().
			withCID("QmTest").
			withString(FlagName, "renamed").
			withStringSlice(FlagMeta, []string{"env=prod"}).
			withBool(FlagDryRun, true).
			withIsSet(FlagName, true).
			withIsSet(FlagMeta, true).
			withIsSet(FlagDryRun, true)

		output := newTestOutput()
		pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgr, "", true, pinningServiceFactory)
		assert.NoError(t, err)
	})
}
