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
	"go.lumeweb.com/portal-sdk/admin"
)

// testPlatformDomain builds an *admin.PlatformDomain. The embedded response
// type is not exported by the SDK wrapper, so its promoted fields are set via
// a helper.
func testPlatformDomain(id int, domain, namespace string, zoneID int, enabled bool) *admin.PlatformDomain {
	d := &admin.PlatformDomain{}
	d.Id = id
	d.Domain = domain
	d.Namespace = namespace
	d.ZoneId = zoneID
	d.Enabled = enabled
	return d
}

func TestAdminPlatformDomainsList(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*configmocks.MockManager, *MockPlatformDomainAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful list",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPlatformDomainAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().ListPlatformDomains(mock.Anything).Return(
					[]*admin.PlatformDomain{
						testPlatformDomain(1, "ipfs.pin.xyz", "icann", 10, true),
					}, 1, nil)
			},
			wantErr: false,
		},
		{
			name: "returns error when not authenticated",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPlatformDomainAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name: "returns error when service fails",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPlatformDomainAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().ListPlatformDomains(mock.Anything).Return(nil, 0, errors.New("list failed"))
			},
			wantErr:     true,
			errContains: "list failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
			service := NewMockPlatformDomainAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			err := adminPlatformDomainsListAction(context.Background(), output, cfgMgr, func(cm config.Manager, out Output) PlatformDomainAdminService {
				return service
			})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAdminPlatformDomainsRegister(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *mockCommand
		setupMocks  func(*configmocks.MockManager, *MockPlatformDomainAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful register",
			cmd: newMockCommand().
				withString(FlagDomain, "ipfs.pin.xyz").
				withString(FlagNamespace, "icann").
				withInt(FlagZoneID, 10),
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPlatformDomainAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().RegisterPlatformDomain(mock.Anything, mock.Anything).Return(
					testPlatformDomain(1, "ipfs.pin.xyz", "icann", 10, true), nil)
			},
			wantErr: false,
		},
		{
			name: "requires domain flag",
			cmd: newMockCommand().
				withString(FlagNamespace, "icann").
				withInt(FlagZoneID, 10),
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPlatformDomainAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
			},
			wantErr:     true,
			errContains: "--domain is required",
		},
		{
			name: "returns error when service fails",
			cmd: newMockCommand().
				withString(FlagDomain, "ipfs.pin.xyz").
				withString(FlagNamespace, "icann"),
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPlatformDomainAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().RegisterPlatformDomain(mock.Anything, mock.Anything).Return(nil, errors.New("register failed"))
			},
			wantErr:     true,
			errContains: "register failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
			service := NewMockPlatformDomainAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			err := adminPlatformDomainsRegisterAction(context.Background(), tt.cmd, output, cfgMgr, func(cm config.Manager, out Output) PlatformDomainAdminService {
				return service
			})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAdminPlatformDomainsUpdate(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *mockCommand
		setupMocks  func(*configmocks.MockManager, *MockPlatformDomainAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful update",
			cmd: newMockCommand().
				withArgs("1").
				withBool(FlagEnabled, true),
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPlatformDomainAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().UpdatePlatformDomain(mock.Anything, "1", mock.Anything).Return(
					testPlatformDomain(1, "ipfs.pin.xyz", "icann", 0, true), nil)
			},
			wantErr: false,
		},
		{
			name: "requires id",
			cmd: newMockCommand().
				withBool(FlagEnabled, false),
			setupMocks:  func(cfgMgr *configmocks.MockManager, service *MockPlatformDomainAdminService) {},
			wantErr:     true,
			errContains: "platform domain ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
			service := NewMockPlatformDomainAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			err := adminPlatformDomainsUpdateAction(context.Background(), tt.cmd, output, cfgMgr, func(cm config.Manager, out Output) PlatformDomainAdminService {
				return service
			})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAdminPlatformDomainsDelete(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *mockCommand
		setupMocks  func(*configmocks.MockManager, *MockPlatformDomainAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful delete",
			cmd:  newMockCommand().withArgs("1"),
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockPlatformDomainAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().DeletePlatformDomain(mock.Anything, "1").Return(nil)
			},
			wantErr: false,
		},
		{
			name:        "requires id",
			cmd:         newMockCommand(),
			setupMocks:  func(cfgMgr *configmocks.MockManager, service *MockPlatformDomainAdminService) {},
			wantErr:     true,
			errContains: "platform domain ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
			service := NewMockPlatformDomainAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			err := adminPlatformDomainsDeleteAction(context.Background(), tt.cmd, output, cfgMgr, func(cm config.Manager, out Output) PlatformDomainAdminService {
				return service
			})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
