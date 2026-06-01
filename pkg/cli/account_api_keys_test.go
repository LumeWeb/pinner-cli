package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"

	mock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	portalsdk "go.lumeweb.com/portal-sdk"
	portalsdkmocks "go.lumeweb.com/portal-sdk/mocks"
)

func newTestAPIKey(name, uuidStr string) *portalsdk.APIKey {
	data, _ := json.Marshal(map[string]string{"name": name, "token": "", "uuid": uuidStr})
	var key portalsdk.APIKey
	_ = json.Unmarshal(data, &key)
	return &key
}

func setupAPIKeyServiceWithAuth(t *testing.T, authToken string, setupAcc func(*portalsdkmocks.MockAccountAPI)) *apiKeyService {
	authSvc := NewMockAuthService(t)
	acc := portalsdkmocks.NewMockAccountAPI(t)
	setupAcc(acc)
	authSvc.EXPECT().GetAuthenticatedClient(mock.Anything).Return(acc, nil).Maybe()
	return &apiKeyService{
		authService: authSvc,
		authToken:   authToken,
	}
}

func TestAPIKeyService_ListAPIKeys(t *testing.T) {
	tests := []struct {
		name        string
		search      string
		setupAcc    func(*portalsdkmocks.MockAccountAPI)
		wantCount   int
		wantErr     bool
		errContains string
	}{
		{
			name:   "list all keys",
			search: "",
			setupAcc: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().ListAPIKeys(context.Background(), mock.Anything).
					Return([]*portalsdk.APIKey{
						newTestAPIKey("key1", "00000000-0000-0000-0000-000000000001"),
						newTestAPIKey("key2", "00000000-0000-0000-0000-000000000002"),
					}, 2, nil)
			},
			wantCount: 2,
		},
		{
			name:   "list with search",
			search: "my-key",
			setupAcc: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().ListAPIKeys(context.Background(), mock.Anything).
					Return([]*portalsdk.APIKey{
						newTestAPIKey("my-key", "00000000-0000-0000-0000-000000000003"),
					}, 1, nil)
			},
			wantCount: 1,
		},
		{
			name:   "empty list",
			search: "",
			setupAcc: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().ListAPIKeys(context.Background(), mock.Anything).
					Return([]*portalsdk.APIKey{}, 0, nil)
			},
			wantCount: 0,
		},
		{
			name:   "api error",
			search: "",
			setupAcc: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().ListAPIKeys(context.Background(), mock.Anything).
					Return(nil, 0, fmt.Errorf("server error"))
			},
			wantErr:     true,
			errContains: "server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := setupAPIKeyServiceWithAuth(t, "test-token", tt.setupAcc)

			keys, total, err := svc.ListAPIKeys(context.Background(), tt.search)
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantCount, total)
			require.Len(t, keys, tt.wantCount)
		})
	}
}

func TestAPIKeyService_CreateAPIKey(t *testing.T) {
	tests := []struct {
		name        string
		keyName     string
		setupAcc    func(*portalsdkmocks.MockAccountAPI)
		wantErr     bool
		errContains string
	}{
		{
			name:    "successful create",
			keyName: "new-key",
			setupAcc: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().CreateAPIKey(context.Background(), "new-key").
					Return(portalsdk.NewAPIKey("new-key", "new-key-token"), nil)
			},
		},
		{
			name:    "create fails",
			keyName: "bad-key",
			setupAcc: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().CreateAPIKey(context.Background(), "bad-key").
					Return(nil, fmt.Errorf("duplicate key name"))
			},
			wantErr:     true,
			errContains: "duplicate key name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := setupAPIKeyServiceWithAuth(t, "test-token", tt.setupAcc)

			apiKey, err := svc.CreateAPIKey(context.Background(), tt.keyName)
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.keyName, apiKey.Name)
		})
	}
}

func TestAPIKeyService_DeleteAPIKey(t *testing.T) {
	uuidStr := "00000000-0000-0000-0000-000000000001"

	tests := []struct {
		name        string
		idOrName    string
		force       bool
		authToken   string
		setupAcc    func(*portalsdkmocks.MockAccountAPI)
		wantErr     bool
		errContains string
	}{
		{
			name:      "delete by uuid",
			idOrName:  uuidStr,
			force:     false,
			authToken: makeAPIKeyJWT("other-uuid", "api"),
			setupAcc: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().DeleteAPIKey(context.Background(), uuidStr).Return(nil)
			},
		},
		{
			name:      "delete by name resolves to uuid",
			idOrName:  "my-key",
			force:     false,
			authToken: makeAPIKeyJWT("other-uuid", "api"),
			setupAcc: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().ListAPIKeys(context.Background(), mock.Anything).
					Return([]*portalsdk.APIKey{newTestAPIKey("my-key", uuidStr)}, 1, nil)
				acc.EXPECT().DeleteAPIKey(context.Background(), uuidStr).Return(nil)
			},
		},
		{
			name:        "self-delete blocked without force",
			idOrName:    uuidStr,
			force:       false,
			authToken:   makeAPIKeyJWT(uuidStr, "api"),
			setupAcc:    func(acc *portalsdkmocks.MockAccountAPI) {},
			wantErr:     true,
			errContains: "currently used for authentication",
		},
		{
			name:      "self-delete allowed with force",
			idOrName:  uuidStr,
			force:     true,
			authToken: makeAPIKeyJWT(uuidStr, "api"),
			setupAcc: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().DeleteAPIKey(context.Background(), uuidStr).Return(nil)
			},
		},
		{
			name:      "delete with login token never blocks",
			idOrName:  uuidStr,
			force:     false,
			authToken: makeAPIKeyJWT("", "login"),
			setupAcc: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().DeleteAPIKey(context.Background(), uuidStr).Return(nil)
			},
		},
		{
			name:      "delete by name not found",
			idOrName:  "nonexistent",
			force:     false,
			authToken: "test-token",
			setupAcc: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().ListAPIKeys(context.Background(), mock.Anything).
					Return([]*portalsdk.APIKey{}, 0, nil)
			},
			wantErr:     true,
			errContains: "API key not found",
		},
		{
			name:      "delete api error",
			idOrName:  uuidStr,
			force:     false,
			authToken: "test-token",
			setupAcc: func(acc *portalsdkmocks.MockAccountAPI) {
				acc.EXPECT().DeleteAPIKey(context.Background(), uuidStr).
					Return(fmt.Errorf("server error"))
			},
			wantErr:     true,
			errContains: "failed to delete API key",
		},
		{
			name:        "not authenticated",
			idOrName:    uuidStr,
			force:       false,
			authToken:   "",
			setupAcc:    func(acc *portalsdkmocks.MockAccountAPI) {},
			wantErr:     true,
			errContains: "not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := setupAPIKeyServiceWithAuth(t, tt.authToken, tt.setupAcc)

			err := svc.DeleteAPIKey(context.Background(), tt.idOrName, tt.force)
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestAPIKeyService_GetCurrentAPIKeyUUID(t *testing.T) {
	tests := []struct {
		name      string
		authToken string
		wantUUID  string
	}{
		{
			name:      "api key jwt returns subject",
			authToken: makeAPIKeyJWT("test-uuid-123", "api"),
			wantUUID: "test-uuid-123",
		},
		{
			name:      "login jwt returns empty",
			authToken: makeAPIKeyJWT("", "login"),
			wantUUID:  "",
		},
		{
			name:      "empty token returns empty",
			authToken: "",
			wantUUID:  "",
		},
		{
			name:      "invalid jwt returns empty",
			authToken: "not-a-jwt",
			wantUUID:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &apiKeyService{
				authToken: tt.authToken,
			}

			uuid := svc.GetCurrentAPIKeyUUID()
			require.Equal(t, tt.wantUUID, uuid)
		})
	}
}

func TestAPIKeyService_RequireAuthenticated(t *testing.T) {
	svc := &apiKeyService{authToken: ""}
	err := svc.RequireAuthenticated()
	require.Error(t, err)

	svc = &apiKeyService{authToken: "test-token"}
	err = svc.RequireAuthenticated()
	require.NoError(t, err)
}

func TestIsUUIDString(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"00000000-0000-0000-0000-000000000001", true},
		{"550e8400-e29b-41d4-a716-446655440000", true},
		{"my-key-name", false},
		{"short", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			require.Equal(t, tt.want, isUUIDString(tt.input))
		})
	}
}

// makeAPIKeyJWT creates a minimal JWT string with the given subject and audience.
func makeAPIKeyJWT(sub, aud string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"sub":"%s","aud":"%s"}`, sub, aud)))
	return header + "." + payload + ".fake-signature"
}
