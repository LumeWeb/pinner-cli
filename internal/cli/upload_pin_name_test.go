package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	contentfs "go.lumeweb.com/ipfs-content/fs"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// wireAccountMockServer points the upload service's account endpoint at an
// httptest server that reports an immediately-completed pin operation, so a
// wait=true upload's waitForPin resolves promptly.
func (h *uploadTestHelpers) wireAccountMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/api/operations/") {
			_, _ = w.Write([]byte(`{"id":1,"status":"completed","operation":"pin","protocol":"ipfs","progress_percent":100,"operation_display_name":"Pin","protocol_display_name":"IPFS","status_display_name":"Completed","status_message":"","started_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":1,"status":"completed","operation":"pin","protocol":"ipfs","progress_percent":100,"operation_display_name":"Pin","protocol_display_name":"IPFS","status_display_name":"Completed","status_message":"","started_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}],"total":1}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	h.service.accountEndpoint = "http://localhost:" + u.Port()
	return srv
}

// expectPinningVerified sets the Status mock expectation that waitForPin's
// post-operation verification needs (a "pinned" status) so the waited upload
// flow can proceed.
func (p *MockPinningService) expectPinningVerified() *mock.Call {
	return p.EXPECT().
		Status(mock.Anything, mock.Anything, false).
		Return(&PinStatus{Status: "pinned"}, nil).Once()
}

// newUploadNameServer returns a mock upload server that records the request so
// tests can assert how the caller name was threaded into the SDK upload, and
// responds with a CID so the upload succeeds.
func newUploadNameServer(t *testing.T, assertName func(*testing.T, *http.Request)) (string, *httptest.Server) {
	t.Helper()
	return createUploadMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if assertName != nil {
			assertName(t, r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"CID":"bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"}`))
	})
}

// TestUploadThreadsPinName verifies the caller-supplied name is passed through
// the SDK upload request (as the POST `name` query param), so the server names
// the pin at creation time rather than the CLI mutating it afterwards.
func TestUploadThreadsPinName(t *testing.T) {
	t.Run("when waiting", func(t *testing.T) {
		baseEndpoint, server := newUploadNameServer(t, func(t *testing.T, r *http.Request) {
			assert.Equal(t, "my document", r.URL.Query().Get("name"))
		})
		defer server.Close()

		h := newUploadTestHelpers(t)
		h.wireAccountMockServer(t)
		pinning := NewMockPinningService(t)
		h.service.pinningService = pinning
		tmpFile := h.createTestFile("test content")
		h.setupUploadExpectations(testAuthToken, baseEndpoint, ipfs.DefaultUploadLimit, int64(ipfs.DefaultUploadLimit))

		pinning.expectPinningVerified()

		f, err := os.Open(tmpFile)
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		filesystem := contentfs.NewSingleFileFS(f, "test.txt")

		result, err := h.service.Upload(context.Background(), filesystem, "my document", true, false)
		require.NoError(t, err)
		assert.NotEmpty(t, result.CID)
	})

	t.Run("when not waiting", func(t *testing.T) {
		baseEndpoint, server := newUploadNameServer(t, func(t *testing.T, r *http.Request) {
			assert.Equal(t, "my document", r.URL.Query().Get("name"))
		})
		defer server.Close()

		h := newUploadTestHelpers(t)
		tmpFile := h.createTestFile("test content")
		h.setupUploadExpectations(testAuthToken, baseEndpoint, ipfs.DefaultUploadLimit, int64(ipfs.DefaultUploadLimit))

		f, err := os.Open(tmpFile)
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		filesystem := contentfs.NewSingleFileFS(f, "test.txt")

		result, err := h.service.Upload(context.Background(), filesystem, "my document", false, false)
		require.NoError(t, err)
		assert.NotEmpty(t, result.CID)
	})
}

// TestUploadOmitsEmptyPinName verifies an empty name is not sent on the upload
// request, so the server applies its default pin name.
func TestUploadOmitsEmptyPinName(t *testing.T) {
	baseEndpoint, server := newUploadNameServer(t, func(t *testing.T, r *http.Request) {
		assert.Empty(t, r.URL.Query().Get("name"))
	})
	defer server.Close()

	h := newUploadTestHelpers(t)
	h.wireAccountMockServer(t)
	pinning := NewMockPinningService(t)
	h.service.pinningService = pinning
	tmpFile := h.createTestFile("test content")
	h.setupUploadExpectations(testAuthToken, baseEndpoint, ipfs.DefaultUploadLimit, int64(ipfs.DefaultUploadLimit))

	pinning.expectPinningVerified()

	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	filesystem := contentfs.NewSingleFileFS(f, "test.txt")

	result, err := h.service.Upload(context.Background(), filesystem, "", true, false)
	require.NoError(t, err)
	assert.NotEmpty(t, result.CID)
}
