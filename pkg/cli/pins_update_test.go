package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

type mockPinsUpdateCommandGetter struct {
	cid       string
	name      string
	meta      []string
	clearMeta bool
	dryRun    bool
	isSet     map[string]bool
}

func (m *mockPinsUpdateCommandGetter) String(name string) string {
	if name == FlagName {
		return m.name
	}
	return ""
}

func (m *mockPinsUpdateCommandGetter) StringSlice(name string) []string {
	if name == FlagMeta {
		return m.meta
	}
	return nil
}

func (m *mockPinsUpdateCommandGetter) Bool(name string) bool {
	switch name {
	case FlagClearMeta:
		return m.clearMeta
	case FlagDryRun:
		return m.dryRun
	}
	return false
}

func (m *mockPinsUpdateCommandGetter) IsSet(name string) bool {
	return m.isSet[name]
}

func (m *mockPinsUpdateCommandGetter) GetCID() string {
	return m.cid
}

func TestPinsUpdate(t *testing.T) {
	t.Run("returns error when cid is missing", func(t *testing.T) {
		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(nil)

		cmd := &mockPinsUpdateCommandGetter{
			cid:   "",
			isSet: map[string]bool{FlagName: true},
		}
		output := NewOutputFormatter(false, false, false, false)
		cfgMgrFactory := func() (config.Manager, error) {
			return configmocks.NewMockManager(t), nil
		}
		pinningServiceFactory := func(cm config.Manager, out Output) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cid is required")
	})

	t.Run("returns error when no update fields provided", func(t *testing.T) {
		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(nil)

		cmd := &mockPinsUpdateCommandGetter{
			cid:   "QmTest",
			isSet: map[string]bool{},
		}
		output := NewOutputFormatter(false, false, false, false)
		cfgMgrFactory := func() (config.Manager, error) {
			return configmocks.NewMockManager(t), nil
		}
		pinningServiceFactory := func(cm config.Manager, out Output) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least one field must be provided for update")
	})

	t.Run("returns error for invalid meta pair", func(t *testing.T) {
		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(nil)

		cmd := &mockPinsUpdateCommandGetter{
			cid:   "QmTest",
			meta:  []string{"invalid"},
			isSet: map[string]bool{FlagMeta: true},
		}
		output := NewOutputFormatter(false, false, false, false)
		cfgMgrFactory := func() (config.Manager, error) {
			return configmocks.NewMockManager(t), nil
		}
		pinningServiceFactory := func(cm config.Manager, out Output) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected key=value format")
	})

	t.Run("returns error when not authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{})

		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)

		cmd := &mockPinsUpdateCommandGetter{
			cid:   "QmTest",
			name:  "renamed",
			isSet: map[string]bool{FlagName: true},
		}
		output := NewOutputFormatter(false, false, false, false)
		cfgMgrFactory := func() (config.Manager, error) {
			return cfgMgr, nil
		}
		pinningServiceFactory := func(cm config.Manager, out Output) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)
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

		cmd := &mockPinsUpdateCommandGetter{
			cid:   "QmTest",
			name:  "renamed",
			isSet: map[string]bool{FlagName: true},
		}
		output := NewOutputFormatter(false, false, false, false)
		cfgMgrFactory := func() (config.Manager, error) {
			return cfgMgr, nil
		}
		pinningServiceFactory := func(cm config.Manager, out Output) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)
		assert.NoError(t, err)
	})

	t.Run("updates metadata successfully", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: "test-token",
		})

		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(nil)
		service.EXPECT().UpdatePin(mock.Anything, "QmTest", "", []string{"env=prod"}, false).Return(nil)

		cmd := &mockPinsUpdateCommandGetter{
			cid:   "QmTest",
			meta:  []string{"env=prod"},
			isSet: map[string]bool{FlagMeta: true},
		}
		output := NewOutputFormatter(false, false, false, false)
		cfgMgrFactory := func() (config.Manager, error) {
			return cfgMgr, nil
		}
		pinningServiceFactory := func(cm config.Manager, out Output) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)
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

		cmd := &mockPinsUpdateCommandGetter{
			cid:       "QmTest",
			clearMeta: true,
			isSet:     map[string]bool{FlagClearMeta: true},
		}
		output := NewOutputFormatter(false, false, false, false)
		cfgMgrFactory := func() (config.Manager, error) {
			return cfgMgr, nil
		}
		pinningServiceFactory := func(cm config.Manager, out Output) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)
		assert.NoError(t, err)
	})

	t.Run("clears and sets metadata successfully", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: "test-token",
		})

		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(nil)
		service.EXPECT().UpdatePin(mock.Anything, "QmTest", "", []string{"fresh=start"}, true).Return(nil)

		cmd := &mockPinsUpdateCommandGetter{
			cid:       "QmTest",
			meta:      []string{"fresh=start"},
			clearMeta: true,
			isSet:     map[string]bool{FlagClearMeta: true, FlagMeta: true},
		}
		output := NewOutputFormatter(false, false, false, false)
		cfgMgrFactory := func() (config.Manager, error) {
			return cfgMgr, nil
		}
		pinningServiceFactory := func(cm config.Manager, out Output) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)
		assert.NoError(t, err)
	})

	t.Run("updates name and metadata successfully", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: "test-token",
		})

		service := NewMockPinningService(t)
		service.EXPECT().RequireAuthenticated().Return(nil)
		service.EXPECT().UpdatePin(mock.Anything, "QmTest", "renamed", []string{"env=prod"}, false).Return(nil)

		cmd := &mockPinsUpdateCommandGetter{
			cid:   "QmTest",
			name:  "renamed",
			meta:  []string{"env=prod"},
			isSet: map[string]bool{FlagName: true, FlagMeta: true},
		}
		output := NewOutputFormatter(false, false, false, false)
		cfgMgrFactory := func() (config.Manager, error) {
			return cfgMgr, nil
		}
		pinningServiceFactory := func(cm config.Manager, out Output) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)
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

		cmd := &mockPinsUpdateCommandGetter{
			cid:    "QmTest",
			name:   "renamed",
			meta:   []string{"env=prod"},
			dryRun: true,
			isSet:  map[string]bool{FlagName: true, FlagMeta: true, FlagDryRun: true},
		}
		output := NewOutputFormatter(false, false, false, false)
		cfgMgrFactory := func() (config.Manager, error) {
			return cfgMgr, nil
		}
		pinningServiceFactory := func(cm config.Manager, out Output) PinningService {
			return service
		}

		err := pinsUpdate(context.Background(), cmd, output, cfgMgrFactory, pinningServiceFactory)
		assert.NoError(t, err)
	})
}
