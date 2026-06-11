package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	go_pinning_service_http_client "github.com/ipfs/boxo/pinning/remote/client"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	climocks "go.lumeweb.com/pinner-cli/pkg/cli/internal/mocks"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

const testAuthToken = "test-token"

func TestNewPinningService(t *testing.T) {
	t.Run("creates service with authenticated client when auth token exists", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com")

		assert.IsType(t, &PinningServiceDefault{}, service)
		ps := service.(*PinningServiceDefault)
		assert.NotNil(t, ps.pinningClient)
	})

	t.Run("creates service without client when auth token is empty", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: "",
		})

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com")

		assert.IsType(t, &PinningServiceDefault{}, service)
		ps := service.(*PinningServiceDefault)
		assert.Nil(t, ps.pinningClient)
	})
}

func TestPinningService_Pin(t *testing.T) {
	t.Run("successfully pins CID", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		mockResult := NewMockPinStatusGetter(t, testCID, "", go_pinning_service_http_client.StatusPinned)
		client.EXPECT().Add(mock.Anything, testCID, mock.Anything).Return(mockResult, nil)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		result, err := service.Pin(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", "", false)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", result.CID)
	})

	t.Run("successfully pins CID with name", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		mockResult := NewMockPinResult(t, "req-123", go_pinning_service_http_client.StatusPinned)
		client.EXPECT().Add(mock.Anything, testCID, mock.Anything).Return(mockResult, nil)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		result, err := service.Pin(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", "test-name", false)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", result.CID)
	})

	t.Run("returns error when not authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()

		service := &PinningServiceDefault{
			pinningClient: nil,
			configMgr:     cfgMgr,
			output:        output,
			apiEndpoint:   "https://api.test.com",
		}

		_, err := service.Pin(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", "", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "please run 'pinner auth login' first")
	})

	t.Run("returns error for invalid CID", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)
		output := newTestOutput()

		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		_, err := service.Pin(context.Background(), "invalid-cid", "", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid CID")
	})

	t.Run("returns error when pinning fails", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		client.EXPECT().Add(mock.Anything, testCID).Return(
			nil,
			errors.New("pinning service error"),
		)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		_, err := service.Pin(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", "", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Pin failed")
	})

	t.Run("returns auth error when pin gets 401", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		client.EXPECT().Add(mock.Anything, testCID).Return(
			nil,
			fmt.Errorf("remote pinning service returned http error 401: unauthorized"),
		)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		_, err := service.Pin(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", "", false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotAuthenticated))
	})
}

func TestPinningService_List(t *testing.T) {
	t.Run("successfully lists pins", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		mockPin := NewMockPinStatusGetter(t, testCID, "test-name", go_pinning_service_http_client.StatusPinned)

		client.EXPECT().LsSync(mock.Anything, mock.Anything, mock.Anything).Return(
			[]go_pinning_service_http_client.PinStatusGetter{mockPin},
			nil,
		)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		pins, err := service.List(context.Background(), "", 0, "")
		require.NoError(t, err)
		assert.Len(t, pins, 1)
		assert.Equal(t, "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", pins[0].CID)
		assert.Equal(t, "test-name", pins[0].Name)
		assert.Equal(t, "req-123", pins[0].RequestID)
	})

	t.Run("returns error when not authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()

		service := &PinningServiceDefault{
			pinningClient: nil,
			configMgr:     cfgMgr,
			output:        output,
			apiEndpoint:   "https://api.test.com",
		}

		_, err := service.List(context.Background(), "", 0, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not authenticated")
	})

	t.Run("returns error when listing fails", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		client.EXPECT().LsSync(mock.Anything, mock.Anything, mock.Anything).Return(
			nil,
			errors.New("list service error"),
		)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		_, err := service.List(context.Background(), "", 0, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "List pins failed")
	})

	t.Run("returns auth error when listing gets 401", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		client.EXPECT().LsSync(mock.Anything, mock.Anything, mock.Anything).Return(
			nil,
			fmt.Errorf("remote pinning service returned http error 401: unauthorized"),
		)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		_, err := service.List(context.Background(), "", 0, "")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotAuthenticated))
	})
}

func TestPinningService_Status(t *testing.T) {
	t.Run("successfully gets pin status", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		mockPin := NewMockPin(t, testCID, "test-name")
		mockResult := NewMockPinStatusGetterWithPin(t, mockPin, go_pinning_service_http_client.StatusPinned)

		client.EXPECT().LsSync(mock.Anything, mock.Anything).Return(
			[]go_pinning_service_http_client.PinStatusGetter{mockResult},
			nil,
		)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		status, err := service.Status(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", false)
		require.NoError(t, err)
		assert.Equal(t, "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", status.CID)
		assert.Equal(t, "pinned", status.Status)
	})

	t.Run("returns error when not authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()

		service := &PinningServiceDefault{
			pinningClient: nil,
			configMgr:     cfgMgr,
			output:        output,
			apiEndpoint:   "https://api.test.com",
		}

		_, err := service.Status(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not authenticated")
	})

	t.Run("returns error for invalid CID", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)
		output := newTestOutput()

		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		_, err := service.Status(context.Background(), "invalid-cid", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid CID")
	})

	t.Run("returns error when pin not found", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		client.EXPECT().LsSync(mock.Anything, mock.Anything).Return(
			[]go_pinning_service_http_client.PinStatusGetter{},
			nil,
		)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		_, err := service.Status(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pin not found")
	})

	t.Run("returns error when status check fails", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		client.EXPECT().LsSync(mock.Anything, mock.Anything).Return(
			nil,
			errors.New("status check error"),
		)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		_, err := service.Status(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Get pin status failed")
	})

	t.Run("returns error when status check fails with auth error", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		client.EXPECT().LsSync(mock.Anything, mock.Anything).Return(
			nil,
			fmt.Errorf("remote pinning service returned http error 401: unauthorized"),
		)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		_, err := service.Status(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotAuthenticated))
	})
}

func TestPinningService_Unpin(t *testing.T) {
	t.Run("successfully unpins CID", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		mockPin := NewMockPin(t, testCID, "test-name")
		mockResult := NewMockPinStatusGetterWithPin(t, mockPin, go_pinning_service_http_client.StatusPinned)

		client.EXPECT().LsSync(mock.Anything, mock.Anything).Return(
			[]go_pinning_service_http_client.PinStatusGetter{mockResult},
			nil,
		)
		client.EXPECT().DeleteByID(mock.Anything, "req-123").Return(nil)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		result, err := service.Unpin(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", true)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", result.CID)
	})

	t.Run("returns early when confirm is false", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)
		output := newTestOutput()

		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		result, err := service.Unpin(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", false)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", result.CID)
	})

	t.Run("returns error when not authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()

		service := &PinningServiceDefault{
			pinningClient: nil,
			configMgr:     cfgMgr,
			output:        output,
			apiEndpoint:   "https://api.test.com",
		}

		_, err := service.Unpin(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "please run 'pinner auth login' first")
	})

	t.Run("returns error for invalid CID", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)
		output := newTestOutput()

		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		_, err := service.Unpin(context.Background(), "invalid-cid", true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid CID")
	})

	t.Run("returns error when pin not found", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		client.EXPECT().LsSync(mock.Anything, mock.Anything).Return(
			[]go_pinning_service_http_client.PinStatusGetter{},
			nil,
		)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		_, err := service.Unpin(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pin not found")
	})

	t.Run("returns error when unpinning fails", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		mockPin := NewMockPin(t, testCID, "test-name")
		mockResult := NewMockPinStatusGetterWithPin(t, mockPin, go_pinning_service_http_client.StatusPinned)

		client.EXPECT().LsSync(mock.Anything, mock.Anything).Return(
			[]go_pinning_service_http_client.PinStatusGetter{mockResult},
			nil,
		)
		client.EXPECT().DeleteByID(mock.Anything, "req-123").Return(
			errors.New("unpin service error"),
		)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		_, err := service.Unpin(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Unpin failed")
	})
}

func TestPinningService_UpdateMetadata(t *testing.T) {
	t.Run("successfully updates metadata", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		mockPin := NewMockPin(t, testCID, "test-name")
		mockResult := NewMockPinStatusGetterWithPin(t, mockPin, go_pinning_service_http_client.StatusPinned)

		client.EXPECT().LsSync(mock.Anything, mock.Anything).Return(
			[]go_pinning_service_http_client.PinStatusGetter{mockResult},
			nil,
		)
		client.EXPECT().Replace(mock.Anything, "req-123", testCID, mock.Anything).Return(
			nil,
			nil,
		)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		err := service.UpdateMetadata(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", []string{"key", "value"}, false)
		require.NoError(t, err)
	})

	t.Run("returns error when not authenticated", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		output := newTestOutput()

		service := &PinningServiceDefault{
			pinningClient: nil,
			configMgr:     cfgMgr,
			output:        output,
			apiEndpoint:   "https://api.test.com",
		}

		err := service.UpdateMetadata(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", []string{"key", "value"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "please run 'pinner auth login' first")
	})

	t.Run("returns error for invalid metadata pairs", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)
		output := newTestOutput()

		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		err := service.UpdateMetadata(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", []string{"key"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "metadata key-value pairs must be provided in pairs")
	})

	t.Run("returns error for invalid CID", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)
		output := newTestOutput()

		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		err := service.UpdateMetadata(context.Background(), "invalid-cid", []string{"key", "value"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid CID")
	})

	t.Run("returns error when pin not found", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		client.EXPECT().LsSync(mock.Anything, mock.Anything).Return(
			[]go_pinning_service_http_client.PinStatusGetter{},
			nil,
		)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		err := service.UpdateMetadata(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", []string{"key", "value"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pin not found")
	})

	t.Run("returns error when update fails", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		client := climocks.NewMockPinningClient(t)

		mockPin := NewMockPin(t, testCID, "test-name")
		mockResult := NewMockPinStatusGetterWithPin(t, mockPin, go_pinning_service_http_client.StatusPinned)

		client.EXPECT().LsSync(mock.Anything, mock.Anything).Return(
			[]go_pinning_service_http_client.PinStatusGetter{mockResult},
			nil,
		)
		client.EXPECT().Replace(mock.Anything, "req-123", testCID, mock.Anything).Return(
			nil,
			errors.New("update service error"),
		)

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		err := service.UpdateMetadata(context.Background(), "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn", []string{"key", "value"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Update pin failed")
	})
}

func TestPinningService_waitForPinCompletion(t *testing.T) {
	t.Run("successfully waits for pin completion", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		client := climocks.NewMockPinningClient(t)
		output := newTestOutput()

		callCount := 0

		client.EXPECT().GetStatusByID(mock.Anything, "req-123").RunAndReturn(
			func(ctx context.Context, requestID string) (go_pinning_service_http_client.PinStatusGetter, error) {
				callCount++
				if callCount == 1 {
					return NewMockPinStatusGetter(t, testCID, "", go_pinning_service_http_client.StatusPinning), nil
				}
				return NewMockPinStatusGetter(t, testCID, "", go_pinning_service_http_client.StatusPinned), nil
			},
		).Times(2)

		service := &PinningServiceDefault{
			pinningClient: client,
			configMgr:     cfgMgr,
			output:        output,
			apiEndpoint:   "https://api.test.com",
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err := service.waitForPinCompletion(ctx, "req-123")
		require.NoError(t, err)
	})

	t.Run("returns error on context cancellation", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		client := climocks.NewMockPinningClient(t)
		output := newTestOutput()

		service := &PinningServiceDefault{
			pinningClient: client,
			configMgr:     cfgMgr,
			output:        output,
			apiEndpoint:   "https://api.test.com",
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := service.waitForPinCompletion(ctx, "req-123")
		require.Error(t, err)
	})

	t.Run("returns error when pinning fails", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		client := climocks.NewMockPinningClient(t)
		output := newTestOutput()

		client.EXPECT().GetStatusByID(mock.Anything, "req-123").Return(
			NewMockPinStatusGetter(t, testCID, "", go_pinning_service_http_client.StatusFailed),
			nil,
		)

		service := &PinningServiceDefault{
			pinningClient: client,
			configMgr:     cfgMgr,
			output:        output,
			apiEndpoint:   "https://api.test.com",
		}

		err := service.waitForPinCompletion(context.Background(), "req-123")
		require.Error(t, err)
		assert.Equal(t, ErrPinningFailed, err)
	})

	t.Run("returns error when status check fails", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		client := climocks.NewMockPinningClient(t)
		output := newTestOutput()

		client.EXPECT().GetStatusByID(mock.Anything, "req-123").Return(
			nil,
			errors.New("status check error"),
		)

		service := &PinningServiceDefault{
			pinningClient: client,
			configMgr:     cfgMgr,
			output:        output,
			apiEndpoint:   "https://api.test.com",
		}

		err := service.waitForPinCompletion(context.Background(), "req-123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Check pin status failed")
	})
}

func TestPinningService_watchPinStatus(t *testing.T) {
	t.Run("successfully watches pin status", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		}).Maybe()

		client := climocks.NewMockPinningClient(t)

		mockPin := NewMockPin(t, testCID, "test-name")
		mockResult := NewMockPinStatusGetterWithPin(t, mockPin, go_pinning_service_http_client.StatusPinned)

		client.EXPECT().LsSync(mock.Anything, mock.Anything).Return(
			[]go_pinning_service_http_client.PinStatusGetter{mockResult},
			nil,
		).Maybe()

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		// watchPinStatus is a private method, we test it through the public Status method with watch=true
		// But for this test, we'll create a service and call the private method via type assertion
		serviceDefault := service.(*PinningServiceDefault)
		_, err := serviceDefault.watchPinStatus(ctx, "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn")
		require.NoError(t, err)
	})

	t.Run("stops on context cancellation", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
			AuthToken: testAuthToken,
		}).Maybe()

		client := climocks.NewMockPinningClient(t)

		mockPin := NewMockPin(t, testCID, "test-name")
		mockResult := NewMockPinStatusGetterWithPin(t, mockPin, go_pinning_service_http_client.StatusPinned)

		client.EXPECT().LsSync(mock.Anything, mock.Anything).Return(
			[]go_pinning_service_http_client.PinStatusGetter{mockResult},
			nil,
		).Maybe()

		output := newTestOutput()
		service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// watchPinStatus is a private method, we test it through the public Status method with watch=true
		// But for this test, we'll create a service and call the private method via type assertion
		serviceDefault := service.(*PinningServiceDefault)
		_, err := serviceDefault.watchPinStatus(ctx, "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn")
		require.NoError(t, err)
	})
}

// Helper functions for creating test mocks

// testCID is a valid CID for testing purposes
var testCID, _ = cid.Decode("QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn")

type mockPin struct {
	cid       cid.Cid
	name      string
	requestID string
	status    go_pinning_service_http_client.Status
	meta      map[string]string
	created   time.Time
	origins   []string
}

func NewMockPin(t testing.TB, c cid.Cid, name string) *mockPin {
	return &mockPin{
		cid:       c,
		name:      name,
		requestID: "req-123",
		status:    go_pinning_service_http_client.StatusPinned,
		meta:      make(map[string]string),
		created:   time.Now(),
		origins:   []string{},
	}
}

func (m *mockPin) GetCid() cid.Cid                                  { return m.cid }
func (m *mockPin) GetName() string                                  { return m.name }
func (m *mockPin) GetRequestId() string                             { return m.requestID }
func (m *mockPin) GetStatus() go_pinning_service_http_client.Status { return m.status }
func (m *mockPin) GetMeta() map[string]string                       { return m.meta }
func (m *mockPin) GetCreated() time.Time                            { return m.created }
func (m *mockPin) GetDelegates() []multiaddr.Multiaddr              { return nil }
func (m *mockPin) GetOrigins() []string                             { return m.origins }

func (m *mockPin) String() string {
	return m.cid.String()
}

func (m *mockPin) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		CID     string            `json:"cid"`
		Name    string            `json:"name,omitempty"`
		Origins []string          `json:"origins,omitempty"`
		Meta    map[string]string `json:"meta,omitempty"`
	}{
		CID:     m.cid.String(),
		Name:    m.name,
		Origins: m.origins,
		Meta:    m.meta,
	})
}

type mockPinStatusGetter struct {
	pin *mockPin
}

func NewMockPinStatusGetter(t testing.TB, c cid.Cid, name string, status go_pinning_service_http_client.Status) go_pinning_service_http_client.PinStatusGetter {
	return &mockPinStatusGetter{
		pin: &mockPin{
			cid:       c,
			name:      name,
			requestID: "req-123",
			status:    status,
			meta:      make(map[string]string),
			created:   time.Now(),
			origins:   []string{},
		},
	}
}

func NewMockPinStatusGetterWithPin(t testing.TB, pin *mockPin, status go_pinning_service_http_client.Status) go_pinning_service_http_client.PinStatusGetter {
	if pin == nil {
		c, _ := cid.Decode("QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn")
		pin = &mockPin{
			cid:       c,
			name:      "",
			requestID: "req-123",
			status:    status,
			meta:      make(map[string]string),
			created:   time.Now(),
			origins:   []string{},
		}
	}
	pin.status = status
	return &mockPinStatusGetter{pin: pin}
}

func NewMockPinResult(t testing.TB, requestID string, status go_pinning_service_http_client.Status) go_pinning_service_http_client.PinStatusGetter {
	c, _ := cid.Decode("QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn")
	return &mockPinStatusGetter{
		pin: &mockPin{
			cid:       c,
			name:      "",
			requestID: requestID,
			status:    status,
			meta:      make(map[string]string),
			created:   time.Now(),
			origins:   []string{},
		},
	}
}

func (m *mockPinStatusGetter) GetPin() go_pinning_service_http_client.PinGetter {
	return m.pin
}
func (m *mockPinStatusGetter) GetStatus() go_pinning_service_http_client.Status {
	return m.pin.status
}
func (m *mockPinStatusGetter) GetRequestId() string {
	return m.pin.requestID
}
func (m *mockPinStatusGetter) GetCreated() time.Time {
	return m.pin.created
}
func (m *mockPinStatusGetter) GetDelegates() []multiaddr.Multiaddr {
	return nil
}

func (m *mockPinStatusGetter) GetInfo() map[string]string {
	return nil
}

func (m *mockPinStatusGetter) String() string {
	return m.pin.String()
}

func (m *mockPinStatusGetter) MarshalJSON() ([]byte, error) {
	return m.pin.MarshalJSON()
}
