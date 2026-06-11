package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	"go.lumeweb.com/portal-sdk/admin"
)

func TestAdminWebsitesBlock(t *testing.T) {
	tests := []struct {
		name        string
		websiteID   string
		setupMocks  func(*configmocks.MockManager, *MockWebsiteAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:      "successful block",
			websiteID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockWebsiteAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().BlockWebsite(mock.Anything, "1").Return(&admin.Website{}, nil)
			},
			wantErr: false,
		},
		{
			name:      "returns error when website ID is missing",
			websiteID: "",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockWebsiteAdminService) {},
			wantErr:     true,
			errContains: "website ID is required",
		},
		{
			name:      "returns error when not authenticated",
			websiteID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockWebsiteAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name:      "returns error when service fails",
			websiteID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockWebsiteAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().BlockWebsite(mock.Anything, "1").Return(nil, errors.New("block failed"))
			},
			wantErr:     true,
			errContains: "block failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
			service := NewMockWebsiteAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			args := &mockArgs{}
			if tt.websiteID != "" {
				args.args = []string{tt.websiteID}
			}
			cmd := &adminWebsitesBlockArgs{args: args}

			serviceFactory := func(cm config.Manager, out Output) WebsiteAdminService {
				return service
			}

			err := adminWebsitesBlockAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

type adminWebsitesBlockArgs struct {
	args cli.Args
}

func (m *adminWebsitesBlockArgs) Args() cli.Args {
	return m.args
}

func TestAdminWebsitesUnblock(t *testing.T) {
	tests := []struct {
		name        string
		websiteID   string
		setupMocks  func(*configmocks.MockManager, *MockWebsiteAdminService)
		wantErr     bool
		errContains string
	}{
		{
			name:      "successful unblock",
			websiteID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockWebsiteAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().UnblockWebsite(mock.Anything, "1").Return(&admin.Website{}, nil)
			},
			wantErr: false,
		},
		{
			name:      "returns error when website ID is missing",
			websiteID: "",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockWebsiteAdminService) {},
			wantErr:     true,
			errContains: "website ID is required",
		},
		{
			name:      "returns error when not authenticated",
			websiteID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockWebsiteAdminService) {
				service.EXPECT().RequireAuthenticated().Return(ErrNotAuthenticated)
			},
			wantErr:     true,
			errContains: "not authenticated",
		},
		{
			name:      "returns error when service fails",
			websiteID: "1",
			setupMocks: func(cfgMgr *configmocks.MockManager, service *MockWebsiteAdminService) {
				service.EXPECT().RequireAuthenticated().Return(nil)
				service.EXPECT().UnblockWebsite(mock.Anything, "1").Return(nil, errors.New("unblock failed"))
			},
			wantErr:     true,
			errContains: "unblock failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
			service := NewMockWebsiteAdminService(t)
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr, service)
			}

			args := &mockArgs{}
			if tt.websiteID != "" {
				args.args = []string{tt.websiteID}
			}
			cmd := &adminWebsitesUnblockArgs{args: args}

			serviceFactory := func(cm config.Manager, out Output) WebsiteAdminService {
				return service
			}

			err := adminWebsitesUnblockAction(context.Background(), cmd, output, cfgMgr, serviceFactory)

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

type adminWebsitesUnblockArgs struct {
	args cli.Args
}

func (m *adminWebsitesUnblockArgs) Args() cli.Args {
	return m.args
}
