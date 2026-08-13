package catalogops

import (
	"context"
	"strings"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/pinning"

	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
)

// stubPinsService is a minimal PinningService whose mutation methods panic: the
// pins_rm XOR guard must fire before any service mutation is reached.
type stubPinsService struct{}

func (stubPinsService) RequireAuthenticated() error { return nil }
func (stubPinsService) Unpin(_ context.Context, _ string, _ bool) (*pinning.UnpinResult, error) {
	panic("Unpin must not be called")
}
func (stubPinsService) UnpinBatch(_ context.Context, _ []string, _ pinning.BatchOptions) (*pinning.BatchResult, error) {
	panic("UnpinBatch must not be called")
}
func (stubPinsService) UnpinAll(_ context.Context, _ string, _ pinning.BatchOptions) (*pinning.BatchResult, error) {
	panic("UnpinAll must not be called")
}
func (stubPinsService) Pin(_ context.Context, _, _ string, _ bool) (*pinning.PinResult, error) {
	panic("Pin must not be called")
}
func (stubPinsService) PinBatch(_ context.Context, _ []string, _ string, _ pinning.BatchOptions) (*pinning.BatchResult, error) {
	panic("PinBatch must not be called")
}
func (stubPinsService) List(_ context.Context, _ string, _ int, _ string) ([]pinning.Pin, error) {
	panic("List must not be called")
}
func (stubPinsService) Status(_ context.Context, _ string, _ bool) (*pinning.PinStatus, error) {
	panic("Status must not be called")
}
func (stubPinsService) UpdateMetadata(_ context.Context, _ string, _ []string, _ bool) error {
	panic("UpdateMetadata must not be called")
}
func (stubPinsService) UpdatePin(_ context.Context, _ string, _ string, _ []string, _ bool) error {
	panic("UpdatePin must not be called")
}

func stubPinsDeps(t *testing.T) PinsDeps {
	return PinsDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		NewAuthenticated: func(_ config.Manager, _ bool, _ string) pinning.PinningService {
			return stubPinsService{}
		},
		ServiceFactory: func(_ config.Manager, _ bool) pinning.PinningService {
			return stubPinsService{}
		},
	}
}

func pinsRmOperation(d PinsDeps) (catalog.Operation, bool) {
	for _, op := range PinsOperations(d) {
		if op.Name() == "pins_rm" {
			return op, true
		}
	}
	return nil, false
}

// TestPinsRmRejectsBothCidsAndAll pins the destructive selector contract:
// supplying both cids and all=true must error (the old behavior silently
// discarded cids and unpinned every pin), never reaching a service mutation.
func TestPinsRmRejectsBothCidsAndAll(t *testing.T) {
	op, ok := pinsRmOperation(stubPinsDeps(t))
	if !ok {
		t.Fatal("pins_rm operation not found")
	}
	_, err := op.Handler().Execute(context.Background(), map[string]any{
		"confirm": true, "cids": []string{"bafy"}, "all": true,
	})
	if err == nil {
		t.Fatal("pins_rm with both cids and all=true must error")
	}
	if !strings.Contains(err.Error(), "not both") {
		t.Fatalf("error must explain the cids/all conflict, got: %v", err)
	}
}

// TestPinsRmCidsOnlyNotBlockedByGuard ensures a plain cids-only call is not
// rejected by the guard: the dry-run path short-circuits before Unpin/UnpinAll,
// so reaching it (without error) proves the guard accepted the cids-only call.
func TestPinsRmCidsOnlyNotBlockedByGuard(t *testing.T) {
	op, ok := pinsRmOperation(stubPinsDeps(t))
	if !ok {
		t.Fatal("pins_rm operation not found")
	}
	_, err := op.Handler().Execute(context.Background(), map[string]any{
		"confirm": true, "cids": []string{"bafy"}, "dry-run": true,
	})
	if err != nil {
		t.Fatalf("cids-only call must pass the guard, got: %v", err)
	}
}
