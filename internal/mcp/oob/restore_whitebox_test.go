package oob

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRestoreRunner implements wizard.RestoreRunner for tests (no network).
// It optionally signals when RunRestore begins (started) and blocks until
// released, to exercise concurrent-POST behavior during the approval window.
type fakeRestoreRunner struct {
	profile string
	calls   int
	err     error // when set, RunRestore returns it

	mu      sync.Mutex
	started chan struct{} // if non-nil, closed when RunRestore begins
	release chan struct{} // if non-nil, RunRestore blocks until closed
}

func (f *fakeRestoreRunner) RestoreProfileName() string { return f.profile }

func (f *fakeRestoreRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeRestoreRunner) RunRestore(ctx context.Context, profile, mnemonic string, onApproval func(approvalURL string)) (string, error) {
	f.mu.Lock()
	f.calls++
	started, release := f.started, f.release
	f.mu.Unlock()
	if started != nil {
		close(started)
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if onApproval != nil {
		onApproval("http://approve.sia")
	}
	if f.err != nil {
		return "", f.err
	}
	if mnemonic == "" {
		return "", assert.AnError
	}
	return "vault-abc123", nil
}

func TestOOBRestoreConcurrentPOSTRejectedDuringApproval(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	runner := &fakeRestoreRunner{profile: "default", started: started, release: release}
	o := NewOOBRestore(runner, time.Minute)
	o.SetBaseURL("http://127.0.0.1:9999")
	mux := http.NewServeMux()
	o.RegisterHandlers(mux)
	url := o.Register("default")

	// First POST: claims the token and blocks inside RunRestore (the approval
	// window). Serve it on a goroutine so we can fire a concurrent POST.
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		req := httptest.NewRequest(http.MethodPost, url, strings.NewReader("mnemonic=alpha+beta+gamma"))
		req.Header.Set("Origin", "http://127.0.0.1:9999")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		mux.ServeHTTP(httptest.NewRecorder(), req)
	}()

	// Wait until the first restore is inside RunRestore (token already claimed).
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first restore never started")
	}

	// A concurrent POST during the approval window must be rejected and must not
	// run the restore a second time.
	second := httptest.NewRequest(http.MethodPost, url, strings.NewReader("mnemonic=again"))
	second.Header.Set("Origin", "http://127.0.0.1:9999")
	second.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	secondRec := httptest.NewRecorder()
	mux.ServeHTTP(secondRec, second)
	require.Equal(t, http.StatusGone, secondRec.Code, "a concurrent POST during the approval window must be rejected")
	require.Equal(t, 1, runner.callCount(), "RunRestore must run exactly once, never twice from a concurrent POST")

	close(release)
	select {
	case <-firstDone:
	case <-time.After(3 * time.Second):
		t.Fatal("first restore never returned after release")
	}
	require.Equal(t, 1, runner.callCount(), "restore must still have run exactly once")
}

func TestOOBRestoreConsumePOSTReportsConsumed(t *testing.T) {
	o := NewOOBRestore(&fakeRestoreRunner{profile: "default"}, time.Minute)
	o.SetBaseURL("http://127.0.0.1:9999")

	url := o.Register("default")
	token := VaultTokenFromURL(url)
	item, _ := o.core.Resolve(token)
	require.NotNil(t, item)

	req := httptest.NewRequest(http.MethodPost, url, strings.NewReader("mnemonic=secret+words"))
	req.Header.Set("Origin", "http://127.0.0.1:9999")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	assert.True(t, o.ConsumePOST(httptest.NewRecorder(), req, token, item),
		"a genuine restore attempt must report the token consumed")

	url2 := o.Register("default")
	token2 := VaultTokenFromURL(url2)
	item2, _ := o.core.Resolve(token2)
	require.NotNil(t, item2)

	req2 := httptest.NewRequest(http.MethodPost, url2, strings.NewReader("mnemonic="))
	req2.Header.Set("Origin", "http://127.0.0.1:9999")
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w2 := httptest.NewRecorder()
	assert.False(t, o.ConsumePOST(w2, req2, token2, item2),
		"a validation failure must NOT report the token consumed so it can be retried")
	assert.Equal(t, http.StatusBadRequest, w2.Code)
}
