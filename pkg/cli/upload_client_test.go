package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	contentfs "go.lumeweb.com/ipfs-content/fs"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	"go.lumeweb.com/pinner-cli/pkg/internal"
	portalsdkmocks "go.lumeweb.com/portal-sdk/mocks"
)

const (
	configToken   = "sample-config-token"
	overrideToken = "sample-override-token"
)

type uploadTestHelpers struct {
	t             *testing.T
	cfgMgr        *configmocks.MockManager
	cfg           *config.Config
	output        Output
	service       *UploadServiceDefault
	accountClient *portalsdkmocks.MockAccountAPI
}

func newUploadTestHelpers(t *testing.T) *uploadTestHelpers {
	cfgMgr := configmocks.NewMockManager(t)
	output := newTestOutput()
	accountClient := portalsdkmocks.NewMockAccountAPI(t)

	cfg := &config.Config{
		MaxRetries: 3,
	}
	cfgMgr.EXPECT().Config().Return(cfg).Maybe()

	opts := []UploadServiceOption{WithUploadAccountClient(accountClient)}

	service := NewUploadService(cfgMgr, output, opts...)
	return &uploadTestHelpers{
		t:             t,
		cfgMgr:        cfgMgr,
		cfg:           cfg,
		output:        output,
		service:       service.(*UploadServiceDefault),
		accountClient: accountClient,
	}
}

func (h *uploadTestHelpers) setupConfig(authToken, baseEndpoint string, memoryLimit uint64) {
	h.cfg.AuthToken = authToken
	h.cfg.BaseEndpoint = baseEndpoint
	h.cfg.MemoryLimit = memoryLimit
	h.cfg.Secure = false
	h.cfg.MaxRetries = 3
	h.cfgMgr.EXPECT().Config().Return(h.cfg).Maybe()
	h.service.ipfsEndpoint = baseEndpoint
}

func (h *uploadTestHelpers) setupUploadExpectations(authToken, baseEndpoint string, memoryLimit uint64, uploadLimit int64) {
	h.accountClient.EXPECT().UploadLimit(mock.Anything).Return(uploadLimit, nil)
	h.setupConfig(authToken, baseEndpoint, memoryLimit)
}

func (h *uploadTestHelpers) createTestFile(content string) string {
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	require.NoError(h.t, err)
	_, err = tmpFile.WriteString(content)
	require.NoError(h.t, err)
	require.NoError(h.t, tmpFile.Close())
	h.t.Cleanup(func() {
		require.NoError(h.t, os.Remove(tmpFile.Name()))
	})
	return tmpFile.Name()
}

// createUploadMockServer creates a test server that handles the SDK's /api/upload endpoint.
// The SDK builds the upload URL as baseURL + "/api/upload", so the test server
// must handle that path.
func createUploadMockServer(t *testing.T, handler http.HandlerFunc) (string, *httptest.Server) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/upload", handler)
	server := httptest.NewServer(mux)

	u, _ := url.Parse(server.URL)
	baseEndpoint := "http://localhost:" + u.Port()

	return baseEndpoint, server
}

func (h *uploadTestHelpers) createTestDirectory(files map[string]string) string {
	tmpDir := h.t.TempDir()
	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		if dirPath := filepath.Dir(path); dirPath != "." {
			err := os.MkdirAll(filepath.Join(tmpDir, dirPath), 0755)
			require.NoError(h.t, err)
		}
		err := os.WriteFile(fullPath, []byte(content), 0644)
		require.NoError(h.t, err)
	}
	return tmpDir
}

func TestNewUploadService(t *testing.T) {
	t.Run("creates service with dependencies", func(t *testing.T) {
		h := newUploadTestHelpers(t)

		h.setupConfig("", "", 0)

		assert.IsType(t, &UploadServiceDefault{}, h.service)
		assert.NotNil(t, h.service.accountClient)
		assert.Equal(t, h.cfgMgr, h.service.configMgr)
		assert.Equal(t, h.output, h.service.output)
		assert.Empty(t, h.service.authToken)
	})
}

func TestUploadServiceDefault_WithAuthToken(t *testing.T) {
	t.Run("sets auth token override", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		assert.Empty(t, h.service.authToken)

		modifiedService := h.service.WithAuthToken(testAuthToken)
		assert.Equal(t, testAuthToken, h.service.authToken)
		assert.Same(t, h.service, modifiedService)
	})
}

func TestUploadServiceDefault_Upload(t *testing.T) {
	t.Run("returns error when not authenticated", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		h.cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: "",
		})

		tmpFile := h.createTestFile("test content")
		f, err := os.Open(tmpFile)
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		filesystem := contentfs.NewSingleFileFS(f, "test.txt")
		_, err = h.service.Upload(context.Background(), filesystem, "test.txt", false)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not authenticated")
	})

	t.Run("returns error for non-existent path", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		h.cfgMgr.EXPECT().Config().Return(&config.Config{
			AuthToken: testAuthToken,
		})

		// Create a fake filesystem that returns errors
		errFS := &errorFS{err: os.ErrNotExist}
		_, err := h.service.Upload(context.Background(), errFS, "test.txt", false)

		require.Error(t, err)
	})

	t.Run("uploads directory successfully", func(t *testing.T) {
		baseEndpoint, server := createUploadMockServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "Bearer "+testAuthToken, r.Header.Get("Authorization"))
			assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")

			err := r.ParseMultipartForm(int64(ipfs.DefaultUploadLimit))
			require.NoError(t, err)

			file, _, err := r.FormFile("file")
			require.NoError(t, err)
			defer require.NoError(t, file.Close())

			body, err := io.ReadAll(file)
			require.NoError(t, err)
			assert.Greater(t, len(body), 10, "CAR data should have header")

			// Verify CAR format using GetCarRoots
			carReader := bytes.NewReader(body)
			roots, err := internal.GetCarRoots(carReader, false)
			require.NoError(t, err, "CAR data should be valid")
			assert.NotEmpty(t, roots, "CAR should have roots")

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"CID":"bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"}`))
		})
		defer server.Close()

		h := newUploadTestHelpers(t)
		tmpDir := h.createTestDirectory(map[string]string{"test.txt": "test content"})

		h.setupUploadExpectations(testAuthToken, baseEndpoint, uint64(ipfs.DefaultUploadLimit), int64(100*1024*1024))

		filesystem := os.DirFS(tmpDir)
		result, err := h.service.Upload(context.Background(), filesystem, "test-dir", false)

		require.NoError(t, err)
		assert.NotEmpty(t, result.CID)
	})

	t.Run("uploads small file via CAR", func(t *testing.T) {
		baseEndpoint, server := createUploadMockServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "Bearer "+testAuthToken, r.Header.Get("Authorization"))
			assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")

			err := r.ParseMultipartForm(int64(ipfs.DefaultUploadLimit))
			require.NoError(t, err)

			file, _, err := r.FormFile("file")
			require.NoError(t, err)
			defer require.NoError(t, file.Close())

			body, err := io.ReadAll(file)
			require.NoError(t, err)
			assert.NotEmpty(t, body)

			w.WriteHeader(http.StatusOK)
		})
		defer server.Close()

		h := newUploadTestHelpers(t)
		tmpFile := h.createTestFile("test content")

		h.setupUploadExpectations(testAuthToken, baseEndpoint, ipfs.DefaultUploadLimit, int64(ipfs.DefaultUploadLimit))

		f, err := os.Open(tmpFile)
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		filesystem := contentfs.NewSingleFileFS(f, "test.txt")
		result, err := h.service.Upload(context.Background(), filesystem, "test.txt", false)

		require.NoError(t, err)
		assert.NotEmpty(t, result.CID)
	})

	t.Run("uses default upload limit when account client fails", func(t *testing.T) {
		baseEndpoint, server := createUploadMockServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		defer server.Close()

		h := newUploadTestHelpers(t)
		tmpFile := h.createTestFile("test content")

		h.accountClient.EXPECT().UploadLimit(mock.Anything).Return(int64(0), errors.New("api error"))
		h.setupConfig(testAuthToken, baseEndpoint, ipfs.DefaultUploadLimit)

		f, err := os.Open(tmpFile)
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		filesystem := contentfs.NewSingleFileFS(f, "test.txt")
		result, err := h.service.Upload(context.Background(), filesystem, "test.txt", false)

		require.NoError(t, err)
		assert.NotEmpty(t, result.CID)
	})

	t.Run("returns error on upload failure", func(t *testing.T) {
		baseEndpoint, server := createUploadMockServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("upload failed"))
		})
		defer server.Close()

		h := newUploadTestHelpers(t)
		tmpFile := h.createTestFile("test content")

		h.setupUploadExpectations(testAuthToken, baseEndpoint, ipfs.DefaultUploadLimit, int64(ipfs.DefaultUploadLimit))

		f, err := os.Open(tmpFile)
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		filesystem := contentfs.NewSingleFileFS(f, "test.txt")
		_, err = h.service.Upload(context.Background(), filesystem, "test.txt", false)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "upload failed")
	})

	t.Run("respects auth token override", func(t *testing.T) {
		baseEndpoint, server := createUploadMockServer(t, func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			assert.Equal(t, "Bearer "+overrideToken, authHeader)
			w.WriteHeader(http.StatusOK)
		})
		defer server.Close()

		h := newUploadTestHelpers(t)
		tmpFile := h.createTestFile("test content")

		h.setupUploadExpectations(configToken, baseEndpoint, ipfs.DefaultUploadLimit, int64(ipfs.DefaultUploadLimit))
		h.service.WithAuthToken(overrideToken)

		f, err := os.Open(tmpFile)
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		filesystem := contentfs.NewSingleFileFS(f, "test.txt")
		result, err := h.service.Upload(context.Background(), filesystem, "test.txt", false)

		require.NoError(t, err)
		assert.NotEmpty(t, result.CID)
	})
}

// errorFS is a test helper that returns errors for all operations
type errorFS struct {
	err error
}

func (e *errorFS) Open(name string) (fs.File, error) {
	return nil, e.err
}

func (e *errorFS) Stat(name string) (fs.FileInfo, error) {
	return nil, e.err
}

func TestUploadServiceDefault_Upload_WaitForPin(t *testing.T) {
	t.Run("waits for pin successfully", func(t *testing.T) {
		baseEndpoint, server := createUploadMockServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		defer server.Close()

		h := newUploadTestHelpers(t)
		tmpFile := h.createTestFile("test content")

		h.setupUploadExpectations(testAuthToken, baseEndpoint, ipfs.DefaultUploadLimit, int64(ipfs.DefaultUploadLimit))

		f, err := os.Open(tmpFile)
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		filesystem := contentfs.NewSingleFileFS(f, "test.txt")
		result, err := h.service.Upload(context.Background(), filesystem, "test.txt", false)

		require.NoError(t, err)
		assert.NotEmpty(t, result.CID)
	})
}

func TestUploadServiceDefault_RequireAuthenticated(t *testing.T) {
	t.Run("returns nil when auth token is available", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		h.cfg.AuthToken = configToken
		h.cfgMgr.EXPECT().Config().Return(h.cfg)

		err := h.service.RequireAuthenticated()
		assert.NoError(t, err)
	})

	t.Run("returns error when no auth token", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		h.cfg.AuthToken = ""
		h.cfgMgr.EXPECT().Config().Return(h.cfg)

		err := h.service.RequireAuthenticated()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not authenticated")
	})

	t.Run("returns nil when override token is set", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		h.cfg.AuthToken = ""
		h.service.WithAuthToken(overrideToken)
		h.cfgMgr.EXPECT().Config().Return(h.cfg)

		err := h.service.RequireAuthenticated()
		assert.NoError(t, err)
	})
}

func TestUploadServiceDefault_getAuthToken(t *testing.T) {
	t.Run("returns override token when set", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		h.cfg.AuthToken = configToken
		h.service.WithAuthToken(overrideToken)
		h.cfgMgr.EXPECT().Config().Return(h.cfg)

		token := h.service.getAuthToken()
		assert.Equal(t, overrideToken, token)
	})

	t.Run("falls back to config token when no override", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		h.cfg.AuthToken = configToken
		h.cfgMgr.EXPECT().Config().Return(h.cfg)

		token := h.service.getAuthToken()
		assert.Equal(t, configToken, token)
	})

	t.Run("returns empty when neither available", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		h.cfg.AuthToken = ""
		h.cfgMgr.EXPECT().Config().Return(h.cfg)

		token := h.service.getAuthToken()
		assert.Empty(t, token)
	})
}

func TestUploadServiceDefault_resolveAuthToken(t *testing.T) {
	t.Run("returns config token when no auth service", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		h.cfg.AuthToken = configToken
		h.cfgMgr.EXPECT().Config().Return(h.cfg)

		token, err := h.service.resolveAuthToken(context.Background())
		require.NoError(t, err)
		assert.Equal(t, configToken, token)
	})

	t.Run("returns raw token when JWT decode fails", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		h.service.authService = NewMockAuthService(t)
		h.cfg.AuthToken = "not-a-jwt"
		h.cfgMgr.EXPECT().Config().Return(h.cfg)

		token, err := h.service.resolveAuthToken(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "not-a-jwt", token)
	})

	t.Run("exchanges API key JWT for login JWT", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		h.service.authService = NewMockAuthService(t)

		apiKeyJWT := createUploadTestJWT(t, "api")
		loginJWT := "login-jwt-token"

		h.cfg.AuthToken = apiKeyJWT
		h.cfgMgr.EXPECT().Config().Return(h.cfg)
		h.accountClient.EXPECT().LoginWithAPIKey(mock.Anything, apiKeyJWT).Return(loginJWT, nil)

		token, err := h.service.resolveAuthToken(context.Background())
		require.NoError(t, err)
		assert.Equal(t, loginJWT, token)
	})

	t.Run("returns error when API key exchange fails", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		h.service.authService = NewMockAuthService(t)

		apiKeyJWT := createUploadTestJWT(t, "api")
		h.cfg.AuthToken = apiKeyJWT
		h.cfgMgr.EXPECT().Config().Return(h.cfg)
		h.accountClient.EXPECT().LoginWithAPIKey(mock.Anything, apiKeyJWT).Return("", errors.New("exchange failed"))

		token, err := h.service.resolveAuthToken(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to exchange API key")
		assert.Empty(t, token)
	})

	t.Run("returns login JWT as-is when purpose is login", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		h.service.authService = NewMockAuthService(t)

		loginJWT := createUploadTestJWT(t, "login")
		h.cfg.AuthToken = loginJWT
		h.cfgMgr.EXPECT().Config().Return(h.cfg)

		token, err := h.service.resolveAuthToken(context.Background())
		require.NoError(t, err)
		assert.Equal(t, loginJWT, token)
	})
}

func TestUploadServiceDefault_wrapUploadError(t *testing.T) {
	t.Run("returns nil for nil error", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		result := h.service.wrapUploadError(nil)
		assert.Nil(t, result)
	})

	t.Run("wraps error with Upload context", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		innerErr := errors.New("something went wrong")
		result := h.service.wrapUploadError(innerErr)
		require.Error(t, result)
		assert.Contains(t, result.Error(), "Upload failed")
		assert.True(t, errors.Is(result, innerErr))
	})
}

func TestUploadServiceDefault_waitForPin(t *testing.T) {
	t.Run("returns error when account endpoint unreachable", func(t *testing.T) {
		h := newUploadTestHelpers(t)
		h.cfgMgr.EXPECT().Config().Return(h.cfg)

		err := h.service.waitForPin(context.Background(), "bafybeigtest", "test-token")
		require.Error(t, err)
	})

	t.Run("returns error when no operations found for CID", func(t *testing.T) {
		h := newUploadTestHelpers(t)

		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[],"total":0}`))
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		u, _ := url.Parse(server.URL)
		h.service.accountEndpoint = "http://localhost:" + u.Port()

		err := h.service.waitForPin(context.Background(), "bafybeigtest", "test-token")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "operation not found")
	})

	t.Run("uses fresh context for pin polling when parent context is cancelled", func(t *testing.T) {
		h := newUploadTestHelpers(t)

		var pollCount int32
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			// Single operation poll: GET /api/operations/{id}
			if parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/operations/"), "/"); len(parts) == 1 && parts[0] != "" && r.URL.Path != "/api/operations" {
				pollCount++
				status := "completed"
				if atomic.LoadInt32(&pollCount) < 3 {
					status = "pending"
				}
				_, _ = fmt.Fprintf(w, `{"id":1,"status":"%s","operation":"pin","protocol":"ipfs","progress_percent":100,"operation_display_name":"Pin","protocol_display_name":"IPFS","status_display_name":"%s","status_message":"","started_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`, status, status)
				return
			}

			// Default: operations list response
			_, _ = w.Write([]byte(`{"data":[{"id":1,"status":"pending","operation":"pin","protocol":"ipfs","progress_percent":0,"operation_display_name":"Pin","protocol_display_name":"IPFS","status_display_name":"Pending","status_message":"","started_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}],"total":1}`))
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		u, _ := url.Parse(server.URL)
		h.service.accountEndpoint = "http://localhost:" + u.Port()

		// Cancel the parent context immediately — the fix should use a fresh
		// context for pin polling, so this should NOT prevent the operation
		// from being polled to completion.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := h.service.waitForPin(ctx, "bafybeigtest", "test-token")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, atomic.LoadInt32(&pollCount), int32(1))
	})
}

func createUploadTestJWT(t *testing.T, audience string) string {
	t.Helper()
	claims := &jwt.RegisteredClaims{
		Audience: jwt.ClaimStrings{audience},
		Issuer:   "test-issuer",
		Subject:  "test-subject",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte("test-secret"))
	require.NoError(t, err)
	return signed
}

func TestUploadServiceDefaultIntegration(t *testing.T) {
	t.Run("handles complex directory structure", func(t *testing.T) {
		baseEndpoint, server := createUploadMockServer(t, func(w http.ResponseWriter, r *http.Request) {
			err := r.ParseMultipartForm(int64(ipfs.DefaultUploadLimit))
			require.NoError(t, err)

			file, _, err := r.FormFile("file")
			require.NoError(t, err)
			defer require.NoError(t, file.Close())

			body, err := io.ReadAll(file)
			require.NoError(t, err)
			assert.NotEmpty(t, body)
			w.WriteHeader(http.StatusOK)
		})
		defer server.Close()

		h := newUploadTestHelpers(t)
		tmpDir := h.createTestDirectory(map[string]string{
			"file1.txt":        "content1",
			"subdir/file2.txt": "content2",
		})

		h.setupUploadExpectations(testAuthToken, baseEndpoint, ipfs.DefaultUploadLimit, int64(ipfs.DefaultUploadLimit))

		filesystem := os.DirFS(tmpDir)
		result, err := h.service.Upload(context.Background(), filesystem, "test-dir", false)

		require.NoError(t, err)
		assert.NotEmpty(t, result.CID)
	})

	t.Run("handles empty directory", func(t *testing.T) {
		baseEndpoint, server := createUploadMockServer(t, func(w http.ResponseWriter, r *http.Request) {
			err := r.ParseMultipartForm(int64(ipfs.DefaultUploadLimit))
			require.NoError(t, err)

			file, _, err := r.FormFile("file")
			require.NoError(t, err)
			defer require.NoError(t, file.Close())

			body, err := io.ReadAll(file)
			require.NoError(t, err)
			assert.NotEmpty(t, body)
			w.WriteHeader(http.StatusOK)
		})
		defer server.Close()

		h := newUploadTestHelpers(t)
		tmpDir := h.t.TempDir()

		h.setupUploadExpectations(testAuthToken, baseEndpoint, ipfs.DefaultUploadLimit, int64(ipfs.DefaultUploadLimit))

		filesystem := os.DirFS(tmpDir)
		result, err := h.service.Upload(context.Background(), filesystem, "empty-dir", false)

		require.NoError(t, err)
		assert.NotEmpty(t, result.CID)
	})
}
