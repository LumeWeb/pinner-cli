package cli

import (
	"context"
	"errors"
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

// TestUploadAppliesPinNameWhenWaiting verifies that on a wait=true upload the
// caller-supplied name is written to the pin's Name metadata via UpdatePin.
func TestUploadAppliesPinNameWhenWaiting(t *testing.T) {
	baseEndpoint, server := createUploadMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"CID":"bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"}`))
	})
	defer server.Close()

	h := newUploadTestHelpers(t)
	h.wireAccountMockServer(t)
	pinning := NewMockPinningService(t)
	h.service.pinningService = pinning
	tmpFile := h.createTestFile("test content")
	h.setupUploadExpectations(testAuthToken, baseEndpoint, ipfs.DefaultUploadLimit, int64(ipfs.DefaultUploadLimit))

	pinning.expectPinningVerified()
	pinning.EXPECT().
		UpdatePin(mock.Anything, mock.Anything, "my document", mock.Anything, false).
		Return(nil).Once()

	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	filesystem := contentfs.NewSingleFileFS(f, "test.txt")

	result, err := h.service.Upload(context.Background(), filesystem, "my document", true)
	require.NoError(t, err)
	assert.NotEmpty(t, result.CID)
}

// TestUploadAppliesPinNameWhenNotWaiting verifies that a fire-and-forget
// (wait=false) upload still applies the pin name when a pinning service is
// wired in (as the MCP path always does): the long-lived MCP server outlives
// the synchronous call, so the name is set even though the caller did not wait.
func TestUploadAppliesPinNameWhenNotWaiting(t *testing.T) {
	baseEndpoint, server := createUploadMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"CID":"bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"}`))
	})
	defer server.Close()

	h := newUploadTestHelpers(t)
	pinning := NewMockPinningService(t)
	h.service.pinningService = pinning
	tmpFile := h.createTestFile("test content")
	h.setupUploadExpectations(testAuthToken, baseEndpoint, ipfs.DefaultUploadLimit, int64(ipfs.DefaultUploadLimit))

	pinning.EXPECT().
		UpdatePin(mock.Anything, mock.Anything, "my document", mock.Anything, false).
		Return(nil).Once()

	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	filesystem := contentfs.NewSingleFileFS(f, "test.txt")

	result, err := h.service.Upload(context.Background(), filesystem, "my document", false)
	require.NoError(t, err)
	assert.NotEmpty(t, result.CID)
}

// TestUploadRetriesPinNameWhenPinNotYetRegistered verifies that on the
// fire-and-forget (wait=false) path a pin that has not yet registered in the
// pinning API does not silently drop the caller's name: UpdatePin's LsSync
// returns ErrPinNotFound until the pin appears, and the short retry recovers by
// applying the name once the pin is visible. This is the race the plain
// success-once mock cannot catch.
func TestUploadRetriesPinNameWhenPinNotYetRegistered(t *testing.T) {
	baseEndpoint, server := createUploadMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"CID":"bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"}`))
	})
	defer server.Close()

	h := newUploadTestHelpers(t)
	pinning := NewMockPinningService(t)
	h.service.pinningService = pinning
	tmpFile := h.createTestFile("test content")
	h.setupUploadExpectations(testAuthToken, baseEndpoint, ipfs.DefaultUploadLimit, int64(ipfs.DefaultUploadLimit))

	// The pinned-at-upload pin is not yet visible: LsSync reports not-found on
	// the first attempt, then the name applies once the pin registers.
	pinning.EXPECT().
		UpdatePin(mock.Anything, mock.Anything, "my document", mock.Anything, false).
		Return(ErrPinNotFound).Once()
	pinning.EXPECT().
		UpdatePin(mock.Anything, mock.Anything, "my document", mock.Anything, false).
		Return(nil).Once()

	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	filesystem := contentfs.NewSingleFileFS(f, "test.txt")

	result, err := h.service.Upload(context.Background(), filesystem, "my document", false)
	require.NoError(t, err)
	assert.NotEmpty(t, result.CID)

	// Both UpdatePin calls were consumed, proving the retry recovered from the
	// transient ErrPinNotFound and applied the name. mockery.AssertExpectations
	// runs automatically via NewMockPinningService's cleanup.
}

// TestUploadNameFailureIsNotFatalWhenNotWaiting verifies that on a fire-and-forget
// (wait=false) upload a failure to set the pin name is non-fatal: the upload
// still returns success. Only on the waited path is a name-set failure surfaced.
func TestUploadNameFailureIsNotFatalWhenNotWaiting(t *testing.T) {
	baseEndpoint, server := createUploadMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"CID":"bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"}`))
	})
	defer server.Close()

	h := newUploadTestHelpers(t)
	pinning := NewMockPinningService(t)
	h.service.pinningService = pinning
	tmpFile := h.createTestFile("test content")
	h.setupUploadExpectations(testAuthToken, baseEndpoint, ipfs.DefaultUploadLimit, int64(ipfs.DefaultUploadLimit))

	pinning.EXPECT().
		UpdatePin(mock.Anything, mock.Anything, "my document", mock.Anything, false).
		Return(errors.New("update failed")).Times(3)

	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	filesystem := contentfs.NewSingleFileFS(f, "test.txt")

	result, err := h.service.Upload(context.Background(), filesystem, "my document", false)
	require.NoError(t, err)
	assert.NotEmpty(t, result.CID)
}

// TestUploadSkipsPinNameWithoutService verifies the name-set is skipped when no
// pinning service is wired in (e.g. the CLI upload path).
func TestUploadSkipsPinNameWithoutService(t *testing.T) {
	baseEndpoint, server := createUploadMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"CID":"bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"}`))
	})
	defer server.Close()

	h := newUploadTestHelpers(t)
	h.wireAccountMockServer(t)
	tmpFile := h.createTestFile("test content")
	h.setupUploadExpectations(testAuthToken, baseEndpoint, ipfs.DefaultUploadLimit, int64(ipfs.DefaultUploadLimit))

	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	filesystem := contentfs.NewSingleFileFS(f, "test.txt")

	result, err := h.service.Upload(context.Background(), filesystem, "my document", true)
	require.NoError(t, err)
	assert.NotEmpty(t, result.CID)
}

// TestUploadSkipsPinNameWhenEmpty verifies the name-set is skipped when the
// resolved name is empty.
func TestUploadSkipsPinNameWhenEmpty(t *testing.T) {
	baseEndpoint, server := createUploadMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"CID":"bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"}`))
	})
	defer server.Close()

	h := newUploadTestHelpers(t)
	h.wireAccountMockServer(t)
	pinning := NewMockPinningService(t)
	h.service.pinningService = pinning
	tmpFile := h.createTestFile("test content")
	h.setupUploadExpectations(testAuthToken, baseEndpoint, ipfs.DefaultUploadLimit, int64(ipfs.DefaultUploadLimit))

	// waitForPin still verifies pin status via the service; empty name must not
	// trigger any UpdatePin (deliberately no UpdatePin expectation).
	pinning.expectPinningVerified()

	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	filesystem := contentfs.NewSingleFileFS(f, "test.txt")

	result, err := h.service.Upload(context.Background(), filesystem, "", true)
	require.NoError(t, err)
	assert.NotEmpty(t, result.CID)
}

// TestUploadNameFailureIsNotFatalWhenWaiting verifies that even on the waited
// path a failure to set the pin name does not fail the upload: the name-set is
// a post-upload metadata label, never part of the upload's core success, so it
// is always best-effort and its failure is downgraded to a warning.
func TestUploadNameFailureIsNotFatalWhenWaiting(t *testing.T) {
	baseEndpoint, server := createUploadMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"CID":"bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"}`))
	})
	defer server.Close()

	h := newUploadTestHelpers(t)
	h.wireAccountMockServer(t)
	pinning := NewMockPinningService(t)
	h.service.pinningService = pinning
	tmpFile := h.createTestFile("test content")
	h.setupUploadExpectations(testAuthToken, baseEndpoint, ipfs.DefaultUploadLimit, int64(ipfs.DefaultUploadLimit))

	pinning.expectPinningVerified()
	pinning.EXPECT().
		UpdatePin(mock.Anything, mock.Anything, "my document", mock.Anything, false).
		Return(errors.New("update failed")).Times(3)

	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	filesystem := contentfs.NewSingleFileFS(f, "test.txt")

	result, err := h.service.Upload(context.Background(), filesystem, "my document", true)
	require.NoError(t, err)
	assert.NotEmpty(t, result.CID)
}
