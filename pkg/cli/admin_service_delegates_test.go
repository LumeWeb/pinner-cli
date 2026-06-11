package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

type mockAuthedService[S any] struct {
	authErr      error
	getServiceFn func(ctx context.Context) (S, error)
}

func (m *mockAuthedService[S]) RequireAuthenticated() error {
	return m.authErr
}

func (m *mockAuthedService[S]) getService(ctx context.Context) (S, error) {
	return m.getServiceFn(ctx)
}

func TestWith2(t *testing.T) {
	t.Run("auth failure returns zero value and error", func(t *testing.T) {
		mockSvc := &mockAuthedService[string]{
			authErr: errors.New("not authenticated"),
		}

		result, err := with2(mockSvc, context.Background(), func(s string) (int, error) {
			return 42, nil
		})

		require.Error(t, err)
		assert.Equal(t, "not authenticated", err.Error())
		assert.Equal(t, 0, result)
	})

	t.Run("getService failure returns zero value and error", func(t *testing.T) {
		mockSvc := &mockAuthedService[string]{
			authErr: nil,
			getServiceFn: func(ctx context.Context) (string, error) {
				return "", errors.New("service unavailable")
			},
		}

		result, err := with2(mockSvc, context.Background(), func(s string) (int, error) {
			return 42, nil
		})

		require.Error(t, err)
		assert.Equal(t, "service unavailable", err.Error())
		assert.Equal(t, 0, result)
	})

	t.Run("success calls fn and returns result", func(t *testing.T) {
		mockSvc := &mockAuthedService[string]{
			authErr: nil,
			getServiceFn: func(ctx context.Context) (string, error) {
				return "service", nil
			},
		}

		result, err := with2(mockSvc, context.Background(), func(s string) (int, error) {
			assert.Equal(t, "service", s)
			return 42, nil
		})

		require.NoError(t, err)
		assert.Equal(t, 42, result)
	})
}

func TestWith3(t *testing.T) {
	t.Run("auth failure returns nil, 0, and error", func(t *testing.T) {
		mockSvc := &mockAuthedService[string]{
			authErr: errors.New("not authenticated"),
		}

		result, count, err := with3(mockSvc, context.Background(), func(s string) ([]string, int, error) {
			return []string{"a"}, 1, nil
		})

		require.Error(t, err)
		assert.Equal(t, "not authenticated", err.Error())
		assert.Nil(t, result)
		assert.Equal(t, 0, count)
	})

	t.Run("getService failure returns nil, 0, and error", func(t *testing.T) {
		mockSvc := &mockAuthedService[string]{
			authErr: nil,
			getServiceFn: func(ctx context.Context) (string, error) {
				return "", errors.New("service unavailable")
			},
		}

		result, count, err := with3(mockSvc, context.Background(), func(s string) ([]string, int, error) {
			return []string{"a"}, 1, nil
		})

		require.Error(t, err)
		assert.Equal(t, "service unavailable", err.Error())
		assert.Nil(t, result)
		assert.Equal(t, 0, count)
	})

	t.Run("success calls fn and returns result", func(t *testing.T) {
		mockSvc := &mockAuthedService[string]{
			authErr: nil,
			getServiceFn: func(ctx context.Context) (string, error) {
				return "service", nil
			},
		}

		result, count, err := with3(mockSvc, context.Background(), func(s string) ([]string, int, error) {
			assert.Equal(t, "service", s)
			return []string{"a", "b"}, 2, nil
		})

		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, result)
		assert.Equal(t, 2, count)
	})
}

func TestWith0(t *testing.T) {
	t.Run("auth failure returns error", func(t *testing.T) {
		mockSvc := &mockAuthedService[string]{
			authErr: errors.New("not authenticated"),
		}

		err := with0(mockSvc, context.Background(), func(s string) error {
			return nil
		})

		require.Error(t, err)
		assert.Equal(t, "not authenticated", err.Error())
	})

	t.Run("getService failure returns error", func(t *testing.T) {
		mockSvc := &mockAuthedService[string]{
			authErr: nil,
			getServiceFn: func(ctx context.Context) (string, error) {
				return "", errors.New("service unavailable")
			},
		}

		err := with0(mockSvc, context.Background(), func(s string) error {
			return nil
		})

		require.Error(t, err)
		assert.Equal(t, "service unavailable", err.Error())
	})

	t.Run("success calls fn and returns nil", func(t *testing.T) {
		mockSvc := &mockAuthedService[string]{
			authErr: nil,
			getServiceFn: func(ctx context.Context) (string, error) {
				return "service", nil
			},
		}

		called := false
		err := with0(mockSvc, context.Background(), func(s string) error {
			assert.Equal(t, "service", s)
			called = true
			return nil
		})

		require.NoError(t, err)
		assert.True(t, called)
	})
}

func TestWith2i(t *testing.T) {
	t.Run("auth failure returns 0 and error", func(t *testing.T) {
		mockSvc := &mockAuthedService[string]{
			authErr: errors.New("not authenticated"),
		}

		result, err := with2i(mockSvc, context.Background(), func(s string) (int, error) {
			return 42, nil
		})

		require.Error(t, err)
		assert.Equal(t, "not authenticated", err.Error())
		assert.Equal(t, 0, result)
	})

	t.Run("getService failure returns 0 and error", func(t *testing.T) {
		mockSvc := &mockAuthedService[string]{
			authErr: nil,
			getServiceFn: func(ctx context.Context) (string, error) {
				return "", errors.New("service unavailable")
			},
		}

		result, err := with2i(mockSvc, context.Background(), func(s string) (int, error) {
			return 42, nil
		})

		require.Error(t, err)
		assert.Equal(t, "service unavailable", err.Error())
		assert.Equal(t, 0, result)
	})

	t.Run("success calls fn and returns result", func(t *testing.T) {
		mockSvc := &mockAuthedService[string]{
			authErr: nil,
			getServiceFn: func(ctx context.Context) (string, error) {
				return "service", nil
			},
		}

		result, err := with2i(mockSvc, context.Background(), func(s string) (int, error) {
			assert.Equal(t, "service", s)
			return 42, nil
		})

		require.NoError(t, err)
		assert.Equal(t, 42, result)
	})
}

func TestQuotaAdminService_Delegation(t *testing.T) {
	ctx := context.Background()

	apiPurposeToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJhcGkifQ."

	newQuotaServiceWithGetServiceError := func(t *testing.T) *quotaAdminService {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken:    apiPurposeToken,
			BaseEndpoint: "http://127.0.0.1:1",
		}).Maybe()

		svc := NewQuotaAdminService(cfgMgr, nil, "http://127.0.0.1:1").(*quotaAdminService)
		return svc
	}

	t.Run("ListAllowances getService error", func(t *testing.T) {
		svc := newQuotaServiceWithGetServiceError(t)

		result, count, err := svc.ListAllowances(ctx)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Equal(t, 0, count)
	})

	t.Run("CreateAllowance getService error", func(t *testing.T) {
		svc := newQuotaServiceWithGetServiceError(t)

		result, err := svc.CreateAllowance(ctx, 1, "src", "type", 100, 100, 100, zeroTime())
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("UpdateAllowance getService error", func(t *testing.T) {
		svc := newQuotaServiceWithGetServiceError(t)

		result, err := svc.UpdateAllowance(ctx, "grant-1", 1, "src", "type", 100, 100, 100, zeroTime())
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("DeleteAllowance getService error", func(t *testing.T) {
		svc := newQuotaServiceWithGetServiceError(t)

		err := svc.DeleteAllowance(ctx, "grant-1")
		require.Error(t, err)
	})

	t.Run("GetStats getService error", func(t *testing.T) {
		svc := newQuotaServiceWithGetServiceError(t)

		result, err := svc.GetStats(ctx)
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("Cleanup getService error", func(t *testing.T) {
		svc := newQuotaServiceWithGetServiceError(t)

		result, err := svc.Cleanup(ctx, 30)
		require.Error(t, err)
		assert.Equal(t, 0, result)
	})

	t.Run("ListUserConfigs getService error", func(t *testing.T) {
		svc := newQuotaServiceWithGetServiceError(t)

		result, count, err := svc.ListUserConfigs(ctx)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Equal(t, 0, count)
	})

	t.Run("UpdateUserConfig getService error", func(t *testing.T) {
		svc := newQuotaServiceWithGetServiceError(t)

		result, err := svc.UpdateUserConfig(ctx, 1, nil)
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("ResetUserPlan getService error", func(t *testing.T) {
		svc := newQuotaServiceWithGetServiceError(t)

		err := svc.ResetUserPlan(ctx, 1)
		require.Error(t, err)
	})

	t.Run("Reconcile not authenticated", func(t *testing.T) {
		svc := newUnauthQuotaAdminService()
		result, count, err := svc.Reconcile(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, ErrNotAuthenticated, err)
		assert.Equal(t, "", result)
		assert.Equal(t, 0, count)
	})

	t.Run("Reconcile getService error", func(t *testing.T) {
		svc := newQuotaServiceWithGetServiceError(t)

		result, count, err := svc.Reconcile(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, "", result)
		assert.Equal(t, 0, count)
	})
}

func zeroTime() time.Time {
	return time.Time{}
}

func TestProfilingAdminService_Delegation(t *testing.T) {
	ctx := context.Background()

	apiPurposeToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJhcGkifQ."

	newProfilingServiceWithGetServiceError := func(t *testing.T) *profilingAdminService {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken:    apiPurposeToken,
			BaseEndpoint: "http://127.0.0.1:1",
		}).Maybe()

		svc := NewProfilingAdminService(cfgMgr, nil, "http://127.0.0.1:1").(*profilingAdminService)
		return svc
	}

	t.Run("GetProfileIndex not authenticated", func(t *testing.T) {
		svc := newUnauthProfilingAdminService()
		result, err := svc.GetProfileIndex(ctx)
		require.Error(t, err)
		assert.Equal(t, ErrNotAuthenticated, err)
		assert.Nil(t, result)
	})

	t.Run("GetProfileIndex getService error", func(t *testing.T) {
		svc := newProfilingServiceWithGetServiceError(t)
		result, err := svc.GetProfileIndex(ctx)
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("GetBlockProfile not authenticated", func(t *testing.T) {
		svc := newUnauthProfilingAdminService()
		result, err := svc.GetBlockProfile(ctx)
		require.Error(t, err)
		assert.Equal(t, ErrNotAuthenticated, err)
		assert.Nil(t, result)
	})

	t.Run("GetBlockProfile getService error", func(t *testing.T) {
		svc := newProfilingServiceWithGetServiceError(t)
		result, err := svc.GetBlockProfile(ctx)
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("SetBlockProfileRate not authenticated", func(t *testing.T) {
		svc := newUnauthProfilingAdminService()
		err := svc.SetBlockProfileRate(ctx, 1)
		require.Error(t, err)
		assert.Equal(t, ErrNotAuthenticated, err)
	})

	t.Run("SetBlockProfileRate getService error", func(t *testing.T) {
		svc := newProfilingServiceWithGetServiceError(t)
		err := svc.SetBlockProfileRate(ctx, 1)
		require.Error(t, err)
	})

	t.Run("GetCmdline not authenticated", func(t *testing.T) {
		svc := newUnauthProfilingAdminService()
		result, err := svc.GetCmdline(ctx)
		require.Error(t, err)
		assert.Equal(t, ErrNotAuthenticated, err)
		assert.Nil(t, result)
	})

	t.Run("GetCmdline getService error", func(t *testing.T) {
		svc := newProfilingServiceWithGetServiceError(t)
		result, err := svc.GetCmdline(ctx)
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("GetGoroutineProfile not authenticated", func(t *testing.T) {
		svc := newUnauthProfilingAdminService()
		result, err := svc.GetGoroutineProfile(ctx)
		require.Error(t, err)
		assert.Equal(t, ErrNotAuthenticated, err)
		assert.Nil(t, result)
	})

	t.Run("GetGoroutineProfile getService error", func(t *testing.T) {
		svc := newProfilingServiceWithGetServiceError(t)
		result, err := svc.GetGoroutineProfile(ctx)
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("GetHeapProfile not authenticated", func(t *testing.T) {
		svc := newUnauthProfilingAdminService()
		result, err := svc.GetHeapProfile(ctx)
		require.Error(t, err)
		assert.Equal(t, ErrNotAuthenticated, err)
		assert.Nil(t, result)
	})

	t.Run("GetHeapProfile getService error", func(t *testing.T) {
		svc := newProfilingServiceWithGetServiceError(t)
		result, err := svc.GetHeapProfile(ctx)
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("GetMutexProfile not authenticated", func(t *testing.T) {
		svc := newUnauthProfilingAdminService()
		result, err := svc.GetMutexProfile(ctx)
		require.Error(t, err)
		assert.Equal(t, ErrNotAuthenticated, err)
		assert.Nil(t, result)
	})

	t.Run("GetMutexProfile getService error", func(t *testing.T) {
		svc := newProfilingServiceWithGetServiceError(t)
		result, err := svc.GetMutexProfile(ctx)
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("SetMutexProfileFraction not authenticated", func(t *testing.T) {
		svc := newUnauthProfilingAdminService()
		err := svc.SetMutexProfileFraction(ctx, 1)
		require.Error(t, err)
		assert.Equal(t, ErrNotAuthenticated, err)
	})

	t.Run("SetMutexProfileFraction getService error", func(t *testing.T) {
		svc := newProfilingServiceWithGetServiceError(t)
		err := svc.SetMutexProfileFraction(ctx, 1)
		require.Error(t, err)
	})
}
