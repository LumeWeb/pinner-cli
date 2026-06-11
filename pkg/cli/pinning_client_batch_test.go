package cli

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	go_pinning_service_http_client "github.com/ipfs/boxo/pinning/remote/client"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	climocks "go.lumeweb.com/pinner-cli/pkg/cli/internal/mocks"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

var (
	batchCID1, _ = cid.Decode("QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn")
	batchCID2, _ = cid.Decode("QmPZ9gcCEpqKTo6aq61g2nXGUhM4iCL3ewB6LDXZCtioEB")
	batchCID3, _ = cid.Decode("QmSnuWmxptJZdLJpRarEy8h7u5ZdhbZaHjyspVvX7wVEQv")
)

func newAuthenticatedBatchService(t *testing.T, client *climocks.MockPinningClient) *PinningServiceDefault {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Maybe().Return(&config.Config{
		AuthToken: testAuthToken,
	})
	output := newTestOutput()
	service := NewPinningService(cfgMgr, output, "https://api.test.com", WithPinningClient(client))
	return service.(*PinningServiceDefault)
}

func TestPinningServiceDefault_PinBatch(t *testing.T) {
	t.Run("happy path with 3 CIDs", func(t *testing.T) {
		client := climocks.NewMockPinningClient(t)

		for _, c := range []cid.Cid{batchCID1, batchCID2, batchCID3} {
			mockResult := NewMockPinStatusGetter(t, c, "batch-pin", go_pinning_service_http_client.StatusPinned)
			client.EXPECT().Add(context.Background(), c, mock.Anything).Return(mockResult, nil)
		}

		service := newAuthenticatedBatchService(t, client)

		result, err := service.PinBatch(context.Background(),
			[]string{batchCID1.String(), batchCID2.String(), batchCID3.String()},
			"batch-pin",
			BatchOptions{Parallel: 2},
		)

		require.NoError(t, err)
		assert.Equal(t, 3, result.Total)
		assert.Len(t, result.Succeeded, 3)
		assert.Empty(t, result.Failed)
		assert.Empty(t, result.Skipped)
		assert.Greater(t, result.Duration, time.Duration(0))
	})

	t.Run("empty CID list returns empty result", func(t *testing.T) {
		client := climocks.NewMockPinningClient(t)
		service := newAuthenticatedBatchService(t, client)

		result, err := service.PinBatch(context.Background(), []string{}, "batch-pin", BatchOptions{})

		require.NoError(t, err)
		assert.Equal(t, &BatchResult{}, result)
	})

	t.Run("ContinueOn=true collects failures", func(t *testing.T) {
		client := climocks.NewMockPinningClient(t)

		mockResult1 := NewMockPinStatusGetter(t, batchCID1, "", go_pinning_service_http_client.StatusPinned)
		client.EXPECT().Add(context.Background(), batchCID1, mock.Anything).Return(mockResult1, nil)

		client.EXPECT().Add(context.Background(), batchCID2, mock.Anything).Return(
			nil, errors.New("pin failed for cid2"),
		)

		mockResult3 := NewMockPinStatusGetter(t, batchCID3, "", go_pinning_service_http_client.StatusPinned)
		client.EXPECT().Add(context.Background(), batchCID3, mock.Anything).Return(mockResult3, nil)

		service := newAuthenticatedBatchService(t, client)

		result, err := service.PinBatch(context.Background(),
			[]string{batchCID1.String(), batchCID2.String(), batchCID3.String()},
			"",
			BatchOptions{Parallel: 1, ContinueOn: true},
		)

		require.NoError(t, err)
		assert.Equal(t, 3, result.Total)
		assert.Len(t, result.Succeeded, 2)
		assert.Len(t, result.Failed, 1)
		assert.Equal(t, batchCID2.String(), result.Failed[0].CID)
		assert.Contains(t, result.Failed[0].Error, "pin failed for cid2")
	})

	t.Run("ContinueOn=false returns first error", func(t *testing.T) {
		client := climocks.NewMockPinningClient(t)

		mockResult1 := NewMockPinStatusGetter(t, batchCID1, "", go_pinning_service_http_client.StatusPinned)
		client.EXPECT().Add(context.Background(), batchCID1, mock.Anything).Return(mockResult1, nil)

		client.EXPECT().Add(context.Background(), batchCID2, mock.Anything).Return(
			nil, errors.New("pin failed for cid2"),
		)

		mockResult3 := NewMockPinStatusGetter(t, batchCID3, "", go_pinning_service_http_client.StatusPinned)
		client.EXPECT().Add(context.Background(), batchCID3, mock.Anything).Return(mockResult3, nil)

		service := newAuthenticatedBatchService(t, client)

		result, err := service.PinBatch(context.Background(),
			[]string{batchCID1.String(), batchCID2.String(), batchCID3.String()},
			"",
			BatchOptions{Parallel: 1, ContinueOn: false},
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "pin failed for cid2")
		assert.Equal(t, 3, result.Total)
		assert.Empty(t, result.Failed)
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

		result, err := service.PinBatch(context.Background(),
			[]string{batchCID1.String()},
			"test",
			BatchOptions{},
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not authenticated")
		assert.Nil(t, result)
	})

	t.Run("defaults parallel to 1 when zero or negative", func(t *testing.T) {
		client := climocks.NewMockPinningClient(t)

		mockResult := NewMockPinStatusGetter(t, batchCID1, "", go_pinning_service_http_client.StatusPinned)
		client.EXPECT().Add(context.Background(), batchCID1, mock.Anything).Return(mockResult, nil)

		service := newAuthenticatedBatchService(t, client)

		result, err := service.PinBatch(context.Background(),
			[]string{batchCID1.String()},
			"",
			BatchOptions{Parallel: 0},
		)

		require.NoError(t, err)
		assert.Len(t, result.Succeeded, 1)
	})

	t.Run("invalid CID in batch with ContinueOn", func(t *testing.T) {
		client := climocks.NewMockPinningClient(t)
		service := newAuthenticatedBatchService(t, client)

		result, err := service.PinBatch(context.Background(),
			[]string{"invalid-cid"},
			"",
			BatchOptions{Parallel: 1, ContinueOn: true},
		)

		require.NoError(t, err)
		assert.Equal(t, 1, result.Total)
		assert.Len(t, result.Failed, 1)
		assert.Equal(t, "invalid-cid", result.Failed[0].CID)
		assert.Contains(t, result.Failed[0].Error, "invalid CID")
	})
}

func TestPinningServiceDefault_UnpinBatch(t *testing.T) {
	t.Run("happy path with 3 CIDs", func(t *testing.T) {
		client := climocks.NewMockPinningClient(t)

		for _, c := range []cid.Cid{batchCID1, batchCID2, batchCID3} {
			mockPin := NewMockPin(t, c, "")
			mockResult := NewMockPinStatusGetterWithPin(t, mockPin, go_pinning_service_http_client.StatusPinned)
			client.EXPECT().LsSync(context.Background(), mock.Anything).Return(
				[]go_pinning_service_http_client.PinStatusGetter{mockResult}, nil,
			)
			client.EXPECT().DeleteByID(context.Background(), mock.Anything).Return(nil)
		}

		service := newAuthenticatedBatchService(t, client)

		result, err := service.UnpinBatch(context.Background(),
			[]string{batchCID1.String(), batchCID2.String(), batchCID3.String()},
			BatchOptions{Parallel: 2},
		)

		require.NoError(t, err)
		assert.Equal(t, 3, result.Total)
		assert.Len(t, result.Succeeded, 3)
		assert.Empty(t, result.Failed)
	})

	t.Run("empty CID list returns empty result", func(t *testing.T) {
		client := climocks.NewMockPinningClient(t)
		service := newAuthenticatedBatchService(t, client)

		result, err := service.UnpinBatch(context.Background(), []string{}, BatchOptions{})

		require.NoError(t, err)
		assert.Equal(t, &BatchResult{}, result)
	})

	t.Run("ContinueOn=true collects failures", func(t *testing.T) {
		client := climocks.NewMockPinningClient(t)

		mockPin1 := NewMockPin(t, batchCID1, "")
		mockResult1 := NewMockPinStatusGetterWithPin(t, mockPin1, go_pinning_service_http_client.StatusPinned)
		mockPin3 := NewMockPin(t, batchCID3, "")
		mockResult3 := NewMockPinStatusGetterWithPin(t, mockPin3, go_pinning_service_http_client.StatusPinned)

		lsCallCount := atomic.Int32{}
		client.EXPECT().LsSync(context.Background(), mock.Anything).RunAndReturn(
			func(ctx context.Context, opts ...go_pinning_service_http_client.LsOption) ([]go_pinning_service_http_client.PinStatusGetter, error) {
				n := lsCallCount.Add(1)
				switch n {
				case 1:
					return []go_pinning_service_http_client.PinStatusGetter{mockResult1}, nil
				case 2:
					return []go_pinning_service_http_client.PinStatusGetter{}, nil
				case 3:
					return []go_pinning_service_http_client.PinStatusGetter{mockResult3}, nil
				default:
					return nil, nil
				}
			},
		).Times(3)

		deleteCallCount := atomic.Int32{}
		client.EXPECT().DeleteByID(context.Background(), mock.Anything).RunAndReturn(
			func(ctx context.Context, id string) error {
				n := deleteCallCount.Add(1)
				if n == 1 {
					return nil
				}
				return nil
			},
		).Times(2)

		service := newAuthenticatedBatchService(t, client)

		result, err := service.UnpinBatch(context.Background(),
			[]string{batchCID1.String(), batchCID2.String(), batchCID3.String()},
			BatchOptions{Parallel: 1, ContinueOn: true},
		)

		require.NoError(t, err)
		assert.Equal(t, 3, result.Total)
		assert.Len(t, result.Succeeded, 2)
		assert.Len(t, result.Failed, 1)
		assert.Equal(t, batchCID2.String(), result.Failed[0].CID)
	})

	t.Run("ContinueOn=false returns first error", func(t *testing.T) {
		client := climocks.NewMockPinningClient(t)

		mockPin1 := NewMockPin(t, batchCID1, "")
		mockResult1 := NewMockPinStatusGetterWithPin(t, mockPin1, go_pinning_service_http_client.StatusPinned)
		mockPin3 := NewMockPin(t, batchCID3, "")
		mockResult3 := NewMockPinStatusGetterWithPin(t, mockPin3, go_pinning_service_http_client.StatusPinned)

		lsCallCount := atomic.Int32{}
		client.EXPECT().LsSync(context.Background(), mock.Anything).RunAndReturn(
			func(ctx context.Context, opts ...go_pinning_service_http_client.LsOption) ([]go_pinning_service_http_client.PinStatusGetter, error) {
				n := lsCallCount.Add(1)
				switch n {
				case 1:
					return []go_pinning_service_http_client.PinStatusGetter{mockResult1}, nil
				case 2:
					return []go_pinning_service_http_client.PinStatusGetter{}, nil
				case 3:
					return []go_pinning_service_http_client.PinStatusGetter{mockResult3}, nil
				default:
					return nil, nil
				}
			},
		).Times(3)

		client.EXPECT().DeleteByID(context.Background(), mock.Anything).Return(nil).Times(2)

		service := newAuthenticatedBatchService(t, client)

		result, err := service.UnpinBatch(context.Background(),
			[]string{batchCID1.String(), batchCID2.String(), batchCID3.String()},
			BatchOptions{Parallel: 1, ContinueOn: false},
		)

		require.Error(t, err)
		assert.Empty(t, result.Failed)
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

		result, err := service.UnpinBatch(context.Background(),
			[]string{batchCID1.String()},
			BatchOptions{},
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not authenticated")
		assert.Nil(t, result)
	})
}

func TestPinningServiceDefault_UnpinAll(t *testing.T) {
	t.Run("unpins all listed pins", func(t *testing.T) {
		client := climocks.NewMockPinningClient(t)

		pins := make([]go_pinning_service_http_client.PinStatusGetter, 3)
		cids := []cid.Cid{batchCID1, batchCID2, batchCID3}
		for i, c := range cids {
			p := &mockPin{
				cid:       c,
				name:      fmt.Sprintf("pin-%d", i+1),
				requestID: fmt.Sprintf("req-%d", i+1),
				status:    go_pinning_service_http_client.StatusPinned,
				meta:      map[string]string{},
				created:   time.Now(),
				origins:   []string{},
			}
			pins[i] = &mockPinStatusGetter{pin: p}
		}
		client.EXPECT().LsSync(context.Background(), mock.Anything).Return(pins, nil)

		for range cids {
			client.EXPECT().DeleteByID(context.Background(), mock.Anything).Return(nil)
		}

		service := newAuthenticatedBatchService(t, client)

		result, err := service.UnpinAll(context.Background(), "", BatchOptions{Parallel: 2})

		require.NoError(t, err)
		assert.Equal(t, 3, result.Total)
		assert.Len(t, result.Succeeded, 3)
		assert.Empty(t, result.Failed)
	})

	t.Run("empty list returns empty result", func(t *testing.T) {
		client := climocks.NewMockPinningClient(t)

		client.EXPECT().LsSync(context.Background(), mock.Anything).Return(
			[]go_pinning_service_http_client.PinStatusGetter{}, nil,
		)

		service := newAuthenticatedBatchService(t, client)

		result, err := service.UnpinAll(context.Background(), "", BatchOptions{})

		require.NoError(t, err)
		assert.Equal(t, &BatchResult{}, result)
	})

	t.Run("returns error when list fails", func(t *testing.T) {
		client := climocks.NewMockPinningClient(t)

		client.EXPECT().LsSync(context.Background(), mock.Anything).Return(
			nil, errors.New("list service error"),
		)

		service := newAuthenticatedBatchService(t, client)

		result, err := service.UnpinAll(context.Background(), "", BatchOptions{})

		require.Error(t, err)
		assert.Nil(t, result)
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

		result, err := service.UnpinAll(context.Background(), "", BatchOptions{})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not authenticated")
		assert.Nil(t, result)
	})

	t.Run("ContinueOn=true collects delete failures", func(t *testing.T) {
		client := climocks.NewMockPinningClient(t)

		pins := make([]go_pinning_service_http_client.PinStatusGetter, 2)
		for i, c := range []cid.Cid{batchCID1, batchCID2} {
			p := &mockPin{
				cid:       c,
				name:      fmt.Sprintf("pin-%d", i+1),
				requestID: fmt.Sprintf("req-%d", i+1),
				status:    go_pinning_service_http_client.StatusPinned,
				meta:      map[string]string{},
				created:   time.Now(),
				origins:   []string{},
			}
			pins[i] = &mockPinStatusGetter{pin: p}
		}
		client.EXPECT().LsSync(context.Background(), mock.Anything).Return(pins, nil)

		var deleteCallCount atomic.Int32
		client.EXPECT().DeleteByID(context.Background(), mock.Anything).RunAndReturn(
			func(ctx context.Context, id string) error {
				n := deleteCallCount.Add(1)
				if n == 2 {
					return errors.New("delete failed")
				}
				return nil
			},
		).Times(2)

		service := newAuthenticatedBatchService(t, client)

		result, err := service.UnpinAll(context.Background(), "", BatchOptions{
			Parallel:   1,
			ContinueOn: true,
		})

		require.NoError(t, err)
		assert.Equal(t, 2, result.Total)
		assert.Len(t, result.Succeeded, 1)
		assert.Len(t, result.Failed, 1)
	})

	t.Run("ContinueOn=false returns first delete error", func(t *testing.T) {
		client := climocks.NewMockPinningClient(t)

		pins := make([]go_pinning_service_http_client.PinStatusGetter, 2)
		for i, c := range []cid.Cid{batchCID1, batchCID2} {
			p := &mockPin{
				cid:       c,
				name:      fmt.Sprintf("pin-%d", i+1),
				requestID: fmt.Sprintf("req-%d", i+1),
				status:    go_pinning_service_http_client.StatusPinned,
				meta:      map[string]string{},
				created:   time.Now(),
				origins:   []string{},
			}
			pins[i] = &mockPinStatusGetter{pin: p}
		}
		client.EXPECT().LsSync(context.Background(), mock.Anything).Return(pins, nil)

		var callCount atomic.Int32
		client.EXPECT().DeleteByID(context.Background(), mock.Anything).RunAndReturn(
			func(ctx context.Context, id string) error {
				n := callCount.Add(1)
				if n == 1 {
					return errors.New("delete failed")
				}
				return nil
			},
		).Times(2)

		service := newAuthenticatedBatchService(t, client)

		result, err := service.UnpinAll(context.Background(), "", BatchOptions{
			Parallel:   1,
			ContinueOn: false,
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "delete failed")
		assert.Empty(t, result.Failed)
	})

	t.Run("with status filter passes filter to list", func(t *testing.T) {
		client := climocks.NewMockPinningClient(t)

		p := &mockPin{
			cid:       batchCID1,
			name:      "failed-pin",
			requestID: "req-failed",
			status:    go_pinning_service_http_client.StatusFailed,
			meta:      map[string]string{},
			created:   time.Now(),
			origins:   []string{},
		}
		client.EXPECT().LsSync(context.Background(), mock.Anything).Return(
			[]go_pinning_service_http_client.PinStatusGetter{&mockPinStatusGetter{pin: p}}, nil,
		)
		client.EXPECT().DeleteByID(context.Background(), mock.Anything).Return(nil)

		service := newAuthenticatedBatchService(t, client)

		result, err := service.UnpinAll(context.Background(), "failed", BatchOptions{Parallel: 1})

		require.NoError(t, err)
		assert.Equal(t, 1, result.Total)
		assert.Len(t, result.Succeeded, 1)
	})
}
