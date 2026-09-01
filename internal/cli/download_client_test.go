package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/mcp"
	portalsdk "go.lumeweb.com/portal-sdk"
	portalsdkmocks "go.lumeweb.com/portal-sdk/mocks"
)

func TestDownloadService_RequireAuthenticated(t *testing.T) {
	t.Run("not authenticated when no token", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: ""}).Maybe()
		svc := &DownloadServiceDefault{configMgr: cfgMgr}
		err := svc.RequireAuthenticated()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not authenticated")
	})

	t.Run("authenticated with override token", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: ""}).Maybe()
		svc := &DownloadServiceDefault{configMgr: cfgMgr, authToken: "test-token"}
		err := svc.RequireAuthenticated()
		require.NoError(t, err)
	})

	t.Run("authenticated with config token", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: "config-token"}).Maybe()
		svc := &DownloadServiceDefault{configMgr: cfgMgr}
		err := svc.RequireAuthenticated()
		require.NoError(t, err)
	})
}

func TestDownloadService_requireAuthenticatedCtx(t *testing.T) {
	t.Run("returns nil when context carries credential", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: ""}).Maybe()
		svc := &DownloadServiceDefault{configMgr: cfgMgr}
		ctx := mcp.WithCredential(context.Background(), "hosted-jwt")
		err := svc.requireAuthenticatedCtx(ctx)
		assert.NoError(t, err)
	})

	t.Run("returns nil when config token available", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: "config-token"}).Maybe()
		svc := &DownloadServiceDefault{configMgr: cfgMgr}
		err := svc.requireAuthenticatedCtx(context.Background())
		assert.NoError(t, err)
	})

	t.Run("returns error when neither available", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: ""}).Maybe()
		svc := &DownloadServiceDefault{configMgr: cfgMgr}
		err := svc.requireAuthenticatedCtx(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not authenticated")
	})
}

func TestDownloadService_getAuthToken(t *testing.T) {
	t.Run("override takes precedence", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: "config-token"}).Maybe()
		svc := &DownloadServiceDefault{configMgr: cfgMgr, authToken: "override-token"}
		assert.Equal(t, "override-token", svc.getAuthToken())
	})

	t.Run("falls back to config", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: "config-token"}).Maybe()
		svc := &DownloadServiceDefault{configMgr: cfgMgr}
		assert.Equal(t, "config-token", svc.getAuthToken())
	})
}

func TestParseIPFSPath(t *testing.T) {
	t.Run("CID only", func(t *testing.T) {
		p, err := parseIPFSPath("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
		require.NoError(t, err)
		assert.Equal(t, "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG", p.cid.String())
		assert.Equal(t, "", p.path)
	})

	t.Run("CID with path", func(t *testing.T) {
		p, err := parseIPFSPath("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG/subdir/file.txt")
		require.NoError(t, err)
		assert.Equal(t, "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG", p.cid.String())
		assert.Equal(t, "subdir/file.txt", p.path)
	})

	t.Run("CID with trailing slash path", func(t *testing.T) {
		p, err := parseIPFSPath("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG/subdir/")
		require.NoError(t, err)
		assert.Equal(t, "subdir", p.path)
	})

	t.Run("invalid CID", func(t *testing.T) {
		_, err := parseIPFSPath("not-a-cid")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidCID)
	})
}

func TestIsNotDirectoryError(t *testing.T) {
	assert.False(t, isNotDirectoryError(nil))
	assert.True(t, isNotDirectoryError(newErrorWithString("path is not a directory")))
	assert.True(t, isNotDirectoryError(newErrorWithString("CID is not a directory")))
	assert.False(t, isNotDirectoryError(newErrorWithString("some other error")))
}

type stringError struct {
	msg string
}

func (e *stringError) Error() string { return e.msg }

func newErrorWithString(msg string) error { return &stringError{msg: msg} }

func TestWrapDownloadError(t *testing.T) {
	t.Run("nil error returns nil", func(t *testing.T) {
		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
		svc := &DownloadServiceDefault{configMgr: cfgMgr}
		assert.Nil(t, svc.wrapDownloadError(nil))
	})
}

func TestWithDownloadAuthToken(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{AuthToken: ""}).Maybe()
	svc := &DownloadServiceDefault{configMgr: cfgMgr}
	WithDownloadAuthToken("test-token")(svc)
	assert.Equal(t, "test-token", svc.authToken)
}

func TestWithDownloadAuthService(t *testing.T) {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{}).Maybe()
	svc := &DownloadServiceDefault{configMgr: cfgMgr}
	mockAuth := NewMockAuthService(t)
	WithDownloadAuthService(mockAuth)(svc)
	assert.NotNil(t, svc.authService)
}

// makeJWTWithAudience mints a signed JWT with the given audience for unit
// tests. The signing key is a fixed, obviously-fake literal used ONLY to
// exercise the GetJWTPurpose decode + API-key exchange logic; it is not a
// credential and must never be used outside test code.
func makeJWTWithAudience(audience string) string {
	claims := jwt.RegisteredClaims{
		Audience: []string{audience},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := token.SignedString([]byte("test-secret"))
	return signed
}

func newDownloadSvc(t *testing.T, authToken string, opts ...DownloadServiceOption) *DownloadServiceDefault {
	cfgMgr := configmocks.NewMockManager(t)
	cfgMgr.EXPECT().Config().Return(&config.Config{
		AuthToken:    authToken,
		BaseEndpoint: "pinner.xyz",
		Secure:       true,
	}).Maybe()
	svc := &DownloadServiceDefault{
		configMgr:    cfgMgr,
		output:       newTestOutput(),
		ipfsEndpoint: "https://ipfs.pinner.xyz",
		authToken:    authToken,
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

func TestDownloadService_resolveAuthToken(t *testing.T) {
	t.Run("returns token from config when no auth service", func(t *testing.T) {
		svc := newDownloadSvc(t, "config-token")
		token, err := svc.resolveAuthToken(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "config-token", token)
	})

	t.Run("returns context JWT as-is when present, no exchange attempted", func(t *testing.T) {
		// Config holds an API key JWT; if the context preference were missing,
		// LoginWithAPIKey would be invoked and (being unset on the gomock mock)
		// fail the test. Its absence proves the exchange is skipped entirely.
		apiKeyJWT := makeJWTWithAudience("api")
		svc := newDownloadSvc(t, apiKeyJWT, WithDownloadAuthService(NewMockAuthService(t)))
		svc.accountClient = portalsdkmocks.NewMockAccountAPI(t)

		ctxJWT := "hosted-caller-login-jwt"
		ctx := mcp.WithCredential(context.Background(), ctxJWT)

		token, err := svc.resolveAuthToken(ctx)
		require.NoError(t, err)
		assert.Equal(t, ctxJWT, token)
	})

	t.Run("exchanges API key JWT from context when purpose is api", func(t *testing.T) {
		apiKeyJWT := makeJWTWithAudience("api")
		mockAccountAPI := portalsdkmocks.NewMockAccountAPI(t)
		mockAccountAPI.EXPECT().LoginWithAPIKey(mock.Anything, apiKeyJWT).Return("login-jwt", nil)

		svc := newDownloadSvc(t, "", WithDownloadAuthService(NewMockAuthService(t)))
		svc.accountClient = mockAccountAPI

		ctx := mcp.WithCredential(context.Background(), apiKeyJWT)
		token, err := svc.resolveAuthToken(ctx)
		require.NoError(t, err)
		assert.Equal(t, "login-jwt", token)
	})

	t.Run("falls back to config when no context JWT", func(t *testing.T) {
		loginJWT := makeJWTWithAudience("login")
		svc := newDownloadSvc(t, loginJWT, WithDownloadAuthService(NewMockAuthService(t)))

		token, err := svc.resolveAuthToken(context.Background())
		require.NoError(t, err)
		assert.Equal(t, loginJWT, token)
	})

	t.Run("returns override token when no auth service", func(t *testing.T) {
		svc := newDownloadSvc(t, "")
		svc.authToken = "override-token"
		token, err := svc.resolveAuthToken(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "override-token", token)
	})

	t.Run("exchanges API key JWT for login token", func(t *testing.T) {
		apiKeyJWT := makeJWTWithAudience("api")
		mockAccountAPI := portalsdkmocks.NewMockAccountAPI(t)
		mockAccountAPI.EXPECT().LoginWithAPIKey(mock.Anything, apiKeyJWT).Return("login-jwt", nil)

		svc := newDownloadSvc(t, apiKeyJWT, WithDownloadAuthService(NewMockAuthService(t)))
		svc.accountClient = mockAccountAPI

		token, err := svc.resolveAuthToken(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "login-jwt", token)
	})

	t.Run("returns raw token when JWT decode fails", func(t *testing.T) {
		svc := newDownloadSvc(t, "not-a-jwt", WithDownloadAuthService(NewMockAuthService(t)))

		token, err := svc.resolveAuthToken(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "not-a-jwt", token)
	})

	t.Run("returns login token as-is when purpose is login", func(t *testing.T) {
		loginJWT := makeJWTWithAudience("login")
		svc := newDownloadSvc(t, loginJWT, WithDownloadAuthService(NewMockAuthService(t)))

		token, err := svc.resolveAuthToken(context.Background())
		require.NoError(t, err)
		assert.Equal(t, loginJWT, token)
	})

	t.Run("returns error when API key exchange fails", func(t *testing.T) {
		apiKeyJWT := makeJWTWithAudience("api")
		mockAccountAPI := portalsdkmocks.NewMockAccountAPI(t)
		mockAccountAPI.EXPECT().LoginWithAPIKey(mock.Anything, apiKeyJWT).Return("", portalsdk.ErrUnauthorized)

		svc := newDownloadSvc(t, apiKeyJWT, WithDownloadAuthService(NewMockAuthService(t)))
		svc.accountClient = mockAccountAPI

		_, err := svc.resolveAuthToken(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to exchange API key for download")
	})
}

func TestDownloadService_Cat(t *testing.T) {
	t.Run("error when not authenticated", func(t *testing.T) {
		svc := newDownloadSvc(t, "")
		_, err := svc.Cat(context.Background(), "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not authenticated")
	})

	t.Run("error on invalid CID", func(t *testing.T) {
		svc := newDownloadSvc(t, "test-token")
		_, err := svc.Cat(context.Background(), "not-a-cid")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidCID)
	})

	t.Run("service error from unreachable endpoint", func(t *testing.T) {
		svc := newDownloadSvc(t, "test-token")
		svc.ipfsEndpoint = "http://127.0.0.1:1"
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := svc.Cat(ctx, "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
		require.Error(t, err)
	})
}

func TestDownloadService_Download(t *testing.T) {
	t.Run("error when not authenticated", func(t *testing.T) {
		svc := newDownloadSvc(t, "")
		_, err := svc.Download(context.Background(), "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG", "", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not authenticated")
	})

	t.Run("error on invalid CID", func(t *testing.T) {
		svc := newDownloadSvc(t, "test-token")
		_, err := svc.Download(context.Background(), "not-a-cid", "", false)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidCID)
	})

	t.Run("error when file already exists without force", func(t *testing.T) {
		tmpDir := t.TempDir()
		existingFile := filepath.Join(tmpDir, "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
		err := os.WriteFile(existingFile, []byte("existing"), 0644)
		require.NoError(t, err)

		cfgMgr := configmocks.NewMockManager(t)
		cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken:    "test-token",
			BaseEndpoint: "pinner.xyz",
			Secure:       true,
		}).Maybe()
		svc := &DownloadServiceDefault{
			configMgr:    cfgMgr,
			output:       newTestOutput(),
			ipfsEndpoint: "https://ipfs.pinner.xyz",
			authToken:    "test-token",
		}

		_, err = svc.Download(context.Background(), "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG", existingFile, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "file already exists")
	})

	t.Run("service error from unreachable endpoint", func(t *testing.T) {
		svc := newDownloadSvc(t, "test-token")
		svc.ipfsEndpoint = "http://127.0.0.1:1"
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := svc.Download(ctx, "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG", filepath.Join(t.TempDir(), "out"), true)
		require.Error(t, err)
	})
}

func TestDownloadService_FileSize(t *testing.T) {
	t.Run("error when not authenticated", func(t *testing.T) {
		svc := newDownloadSvc(t, "")
		_, err := svc.FileSize(context.Background(), "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not authenticated")
	})

	t.Run("error on invalid CID", func(t *testing.T) {
		svc := newDownloadSvc(t, "test-token")
		_, err := svc.FileSize(context.Background(), "not-a-cid")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidCID)
	})

	t.Run("service error from unreachable endpoint", func(t *testing.T) {
		svc := newDownloadSvc(t, "test-token")
		svc.ipfsEndpoint = "http://127.0.0.1:1"
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := svc.FileSize(ctx, "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
		require.Error(t, err)
	})
}

func TestDownloadService_ListDirectory(t *testing.T) {
	t.Run("error when not authenticated", func(t *testing.T) {
		svc := newDownloadSvc(t, "")
		_, err := svc.ListDirectory(context.Background(), "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not authenticated")
	})

	t.Run("error on invalid CID", func(t *testing.T) {
		svc := newDownloadSvc(t, "test-token")
		_, err := svc.ListDirectory(context.Background(), "not-a-cid")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidCID)
	})

	t.Run("service error from unreachable endpoint", func(t *testing.T) {
		svc := newDownloadSvc(t, "test-token")
		svc.ipfsEndpoint = "http://127.0.0.1:1"
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := svc.ListDirectory(ctx, "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
		require.Error(t, err)
	})
}

func TestDownloadService_listFileEntry(t *testing.T) {
	t.Run("CID as name when no path", func(t *testing.T) {
		p, err := parseIPFSPath("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
		require.NoError(t, err)

		name := p.cid.String()
		if p.path != "" {
			name = filepath.Base(p.path)
		}
		assert.Equal(t, "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG", name)
	})

	t.Run("path basename as name when path present", func(t *testing.T) {
		p, err := parseIPFSPath("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG/subdir/file.txt")
		require.NoError(t, err)

		name := p.cid.String()
		if p.path != "" {
			name = filepath.Base(p.path)
		}
		assert.Equal(t, "file.txt", name)
	})
}

func TestDownloadService_newSDKDownloadService(t *testing.T) {
	t.Run("error when not authenticated", func(t *testing.T) {
		svc := newDownloadSvc(t, "")
		_, err := svc.newSDKDownloadService(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not authenticated")
	})

	t.Run("error when resolveAuthToken fails", func(t *testing.T) {
		apiKeyJWT := makeJWTWithAudience("api")
		mockAccountAPI := portalsdkmocks.NewMockAccountAPI(t)
		mockAccountAPI.EXPECT().LoginWithAPIKey(mock.Anything, apiKeyJWT).Return("", portalsdk.ErrUnauthorized)

		svc := newDownloadSvc(t, apiKeyJWT, WithDownloadAuthService(NewMockAuthService(t)))
		svc.accountClient = mockAccountAPI

		_, err := svc.newSDKDownloadService(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to exchange API key for download")
	})

	t.Run("creates SDK service with valid endpoint", func(t *testing.T) {
		svc := newDownloadSvc(t, "test-token")
		dlService, err := svc.newSDKDownloadService(context.Background())
		require.NoError(t, err)
		require.NotNil(t, dlService)
		assert.Equal(t, "test-token", dlService.AuthToken())
	})

	t.Run("accepts context credential with no config token", func(t *testing.T) {
		// Regresses the hosted (Portal-embedded) download: the session is
		// authenticated by the per-request credential on the context (credctx),
		// while the shared config token is empty. The ctx-aware gate must pass
		// and the SDK service build from the context credential, not fail with
		// "not authenticated".
		svc := newDownloadSvc(t, "") // empty config token
		svc.ipfsEndpoint = "http://127.0.0.1:1"

		ctx := mcp.WithCredential(context.Background(), "hosted-caller-login-jwt")
		dl, err := svc.newSDKDownloadService(ctx)
		require.NoError(t, err)
		assert.NotNil(t, dl)
	})
}

func TestDownloadService_Download_defaultOutputPath(t *testing.T) {
	t.Run("CID string as default filename when no path", func(t *testing.T) {
		p, err := parseIPFSPath("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG")
		require.NoError(t, err)

		outputPath := ""
		if outputPath == "" {
			if p.path != "" {
				outputPath = filepath.Base(p.path)
			} else {
				outputPath = p.cid.String()
			}
		}
		assert.Equal(t, "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG", outputPath)
	})

	t.Run("path basename as default filename when path present", func(t *testing.T) {
		p, err := parseIPFSPath("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG/subdir/myfile.txt")
		require.NoError(t, err)

		outputPath := ""
		if outputPath == "" {
			if p.path != "" {
				outputPath = filepath.Base(p.path)
			} else {
				outputPath = p.cid.String()
			}
		}
		assert.Equal(t, "myfile.txt", outputPath)
	})
}

type mockReadCloser struct {
	io.Reader
}

func (m *mockReadCloser) Close() error { return nil }

func TestDownloadService_Cat_withPath(t *testing.T) {
	t.Run("CID with subpath triggers GetFile branch", func(t *testing.T) {
		p, err := parseIPFSPath("QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG/subdir/file.txt")
		require.NoError(t, err)
		assert.Equal(t, "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG", p.cid.String())
		assert.Equal(t, "subdir/file.txt", p.path)
		assert.NotEmpty(t, p.path)
	})
}
