package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
)

func TestNewPinsAddCommandProperties(t *testing.T) {
	cmd := findCommand(newPinsCommand().Commands, "add")
	require.NotNil(t, cmd, "pins command should compile an 'add' subcommand")
	assert.Equal(t, "add", cmd.Name)
	assert.NotNil(t, cmd.Action)
	assert.NotEmpty(t, cmd.Flags)
	assert.Contains(t, cmd.Usage, "Pin existing content")
}

func TestPinsAddCommand_Flags(t *testing.T) {
	cmd := findCommand(newPinsCommand().Commands, "add")
	require.NotNil(t, cmd, "pins command should compile an 'add' subcommand")
	flagNames := getFlagNames(cmd)
	require.Contains(t, flagNames, "name")
	require.Contains(t, flagNames, "no-wait")
	require.Contains(t, flagNames, "file")
	require.Contains(t, flagNames, "parallel")
	require.Contains(t, flagNames, "dry-run")
	require.Contains(t, flagNames, "meta")
}

func TestPinsAdd_DryRun(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	service := NewMockPinningService(t)
	output := newTestOutput()

	cfgMgr.EXPECT().Config().Return(&config.Config{
		Secure:       true,
		BaseEndpoint: "pinner.xyz",
		AuthToken:    "test-token",
	})
	service.EXPECT().RequireAuthenticated().Return(nil)

	cmd := newMockCommand().
		withCID("QmXxx").
		withBool(FlagDryRun, true)

	pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
		return service
	}

	err := pinsAdd(context.Background(), cmd, output, cfgMgr, "", true, pinningServiceFactory)
	require.NoError(t, err)
}

func TestPinsAdd_NoMeta(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{
		Secure:       true,
		BaseEndpoint: "pinner.xyz",
		AuthToken:    "test-token",
		MaxRetries:   3,
	}).Maybe()
	service := NewMockPinningService(t)
	output := newTestOutput()

	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().Pin(mock.Anything, "QmXxx", "", true).Return(
		&PinResult{CID: "QmXxx", RequestID: "req-1", Status: "pinned"}, nil,
	)

	cmd := newMockCommand().withCID("QmXxx")

	pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
		return service
	}

	err := pinsAdd(context.Background(), cmd, output, cfgMgr, "", true, pinningServiceFactory)
	require.NoError(t, err)
}

func TestPinsAdd_WithMetadata(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{
		Secure:       true,
		BaseEndpoint: "pinner.xyz",
		AuthToken:    "test-token",
		MaxRetries:   3,
	}).Maybe()
	service := NewMockPinningService(t)
	output := newTestOutput()

	// pin() calls factory + RequireAuthenticated + Pin
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().Pin(mock.Anything, "QmXxx", "", true).Return(
		&PinResult{CID: "QmXxx", RequestID: "req-1", Status: "pinned"}, nil,
	)
	// pinsAdd metadata path calls factory + RequireAuthenticated + UpdateMetadata
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().UpdateMetadata(mock.Anything, "QmXxx", []string{"owner", "alice"}, false).Return(nil)

	cmd := newMockCommand().
		withCID("QmXxx").
		withStringSlice(FlagMeta, []string{"owner=alice"})

	pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
		return service
	}

	err := pinsAdd(context.Background(), cmd, output, cfgMgr, "", true, pinningServiceFactory)
	require.NoError(t, err)
}

func TestPinsAdd_PinError(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{
		Secure:       true,
		BaseEndpoint: "pinner.xyz",
		AuthToken:    "test-token",
		MaxRetries:   3,
	}).Maybe()
	service := NewMockPinningService(t)
	output := newTestOutput()

	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().Pin(mock.Anything, "QmXxx", "", true).Return(
		nil, errors.New("pinning failed"),
	)

	cmd := newMockCommand().withCID("QmXxx")

	pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
		return service
	}

	err := pinsAdd(context.Background(), cmd, output, cfgMgr, "", true, pinningServiceFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pinning failed")
}

func TestPinsAdd_MetadataUpdateError(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{
		Secure:       true,
		BaseEndpoint: "pinner.xyz",
		AuthToken:    "test-token",
		MaxRetries:   3,
	}).Maybe()
	service := NewMockPinningService(t)
	output := newTestOutput()

	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().Pin(mock.Anything, "QmXxx", "", true).Return(
		&PinResult{CID: "QmXxx", RequestID: "req-1", Status: "pinned"}, nil,
	)
	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().UpdateMetadata(mock.Anything, "QmXxx", []string{"owner", "alice"}, false).Return(errors.New("metadata update failed"))

	cmd := newMockCommand().
		withCID("QmXxx").
		withStringSlice(FlagMeta, []string{"owner=alice"})

	pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
		return service
	}

	err := pinsAdd(context.Background(), cmd, output, cfgMgr, "", true, pinningServiceFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pin succeeded but metadata update failed")
}

func TestPinsAdd_InvalidMetaFormat(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{
		Secure:       true,
		BaseEndpoint: "pinner.xyz",
		AuthToken:    "test-token",
		MaxRetries:   3,
	}).Maybe()
	service := NewMockPinningService(t)
	output := newTestOutput()

	service.EXPECT().RequireAuthenticated().Return(nil)
	service.EXPECT().Pin(mock.Anything, "QmXxx", "", true).Return(
		&PinResult{CID: "QmXxx", RequestID: "req-1", Status: "pinned"}, nil,
	)

	cmd := newMockCommand().
		withCID("QmXxx").
		withStringSlice(FlagMeta, []string{"invalid-no-equals"})

	pinningServiceFactory := func(cm config.Manager, _ bool) PinningService {
		return service
	}

	err := pinsAdd(context.Background(), cmd, output, cfgMgr, "", true, pinningServiceFactory)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid metadata pair")
}
