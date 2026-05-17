package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/tus/tusd/v2/pkg/handler"
	"github.com/tus/tusd/v2/pkg/memorylocker"
	contentfs "go.lumeweb.com/ipfs-content/fs"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
	portalsdkmocks "go.lumeweb.com/portal-sdk/mocks"
)

// memoryStore implements a simple in-memory TUS store for testing
type memoryStore struct {
	mu      sync.RWMutex
	uploads map[string]*memoryUpload
}

type memoryUpload struct {
	mu     sync.Mutex
	info   handler.FileInfo
	data   []byte
	offset int64
	closed bool
}

// tusTestSetup holds all the components needed for TUS integration tests
type tusTestSetup struct {
	server       *httptest.Server
	tusHandler   *handler.Handler
	store        *memoryStore
	cfgMgr       *configmocks.MockManager
	accClient    *portalsdkmocks.MockAccountAPI
	output       Output
	service      UploadService
	baseEndpoint string
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		uploads: make(map[string]*memoryUpload),
	}
}

func (s *memoryStore) NewUpload(ctx context.Context, info handler.FileInfo) (handler.Upload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate a UUID if no ID is provided
	if info.ID == "" {
		info.ID = uuid.New().String()
	}

	upload := &memoryUpload{
		info: info,
		data: make([]byte, 0, info.Size),
	}
	s.uploads[info.ID] = upload
	return upload, nil
}

func (s *memoryStore) GetUpload(ctx context.Context, id string) (handler.Upload, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	upload, ok := s.uploads[id]
	if !ok {
		return nil, handler.ErrNotFound
	}
	return upload, nil
}

func (s *memoryStore) AsTerminatableUpload(upload handler.Upload) handler.TerminatableUpload {
	return upload.(*memoryUpload)
}

func (u *memoryUpload) GetInfo(ctx context.Context) (handler.FileInfo, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.info, nil
}

func (u *memoryUpload) WriteChunk(ctx context.Context, offset int64, src io.Reader) (int64, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	// Read all available data from source
	buf := new(bytes.Buffer)
	n, err := io.Copy(buf, src)
	if err != nil {
		return 0, err
	}

	data := buf.Bytes()

	// Ensure data slice has enough capacity
	if int64(cap(u.data)) < u.offset+int64(len(data)) {
		newData := make([]byte, len(u.data), u.offset+int64(len(data)))
		copy(newData, u.data)
		u.data = newData
	}

	// Ensure data slice has enough length
	if int64(len(u.data)) < u.offset {
		u.data = u.data[:u.offset]
	}

	// Append new data
	u.data = append(u.data, data...)
	u.offset += int64(n)

	return n, nil
}

func (u *memoryUpload) GetReader(ctx context.Context) (io.ReadCloser, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	return io.NopCloser(bytes.NewReader(u.data)), nil
}

func (u *memoryUpload) Terminate(ctx context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.closed = true
	return nil
}

func (u *memoryUpload) FinishUpload(ctx context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	return nil
}

// setupTUSTest creates a complete TUS test environment with server, mocks, and service
func setupTUSTest(t *testing.T, uploadLimit int64) *tusTestSetup {
	// Create TUS server with memory store
	store := newMemoryStore()
	locker := memorylocker.New()
	composer := handler.NewStoreComposer()
	composer.UseCore(store)
	composer.UseTerminater(store)
	composer.UseLocker(locker)

	tusHandler, err := handler.NewHandler(handler.Config{
		StoreComposer: composer,
		BasePath:      "/api/upload/tus",
	})
	require.NoError(t, err)

	// Create TUS server
	server := httptest.NewServer(http.StripPrefix("/api/upload/tus", tusHandler))

	// Parse server URL to get host:port
	serverURL, _ := url.Parse(server.URL)

	// Setup config mock with TUS endpoint pointing to test server
	t.Setenv("TEST_AUTH_TOKEN", testAuthToken)

	cfgMgr := configmocks.NewMockManager(t)
	cfg := &config.Config{
		AuthToken:    os.Getenv("TEST_AUTH_TOKEN"),
		BaseEndpoint: serverURL.Hostname() + ":" + serverURL.Port(),
		MemoryLimit:  100 * 1024 * 1024,
		Secure:       false,
	}
	cfgMgr.EXPECT().Config().Return(cfg)
	cfgMgr.EXPECT().Config().Maybe().Return(cfg)

	// Setup account client mock for upload limit
	accClient := portalsdkmocks.NewMockAccountAPI(t)
	accClient.EXPECT().UploadLimit(mock.Anything).Return(uploadLimit, nil)

	output := NewOutputFormatter(false, false, false, false)
	service := NewUploadService(cfgMgr, output, "https://api.test.com", WithUploadAccountClient(accClient))

	return &tusTestSetup{
		server:       server,
		tusHandler:   tusHandler,
		store:        store,
		cfgMgr:       cfgMgr,
		accClient:    accClient,
		output:       output,
		service:      service,
		baseEndpoint: serverURL.Hostname() + ":" + serverURL.Port(),
	}
}

// cleanupTUSTest closes the test server and performs cleanup
func (ts *tusTestSetup) cleanup() {
	if ts.server != nil {
		ts.server.Close()
	}
}

// createTempTestFile creates a temporary test file with the given content
func createTempTestFile(t *testing.T, content string) string {
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(tmpFile.Name()) })
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())
	return tmpFile.Name()
}

func TestUploadServiceDefault_Upload_TUS_Integration(t *testing.T) {
	t.Run("uploads large file via TUS", func(t *testing.T) {
		ts := setupTUSTest(t, int64(10))
		defer ts.cleanup()

		testContent := "test content for TUS upload"
		tmpFile := createTempTestFile(t, testContent)

		f, err := os.Open(tmpFile)
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		filesystem := contentfs.NewSingleFileFS(f, "test.txt")
		cid, err := ts.service.Upload(context.Background(), filesystem, "test.txt", false)

		require.NoError(t, err)
		assert.NotEmpty(t, cid)
	})
}
