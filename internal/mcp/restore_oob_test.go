package mcp

import (
	"context"
	"errors"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/mcp/oob"
)

type fakeRestoreRunner struct {
	profile string
	calls   int
	err     error // when set, RunRestore returns it

	mu      sync.Mutex
	started chan struct{} // if non-nil, closed when RunRestore begins
	release chan struct{} // if non-nil, RunRestore blocks until closed
}

func (f *fakeRestoreRunner) RestoreProfileName() string { return f.profile }

// callCount returns the number of RunRestore invocations, synchronized for use
// from multiple goroutines.
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

// buildRestore returns a wired oob.OOBRestore with a fake runner and a thrown-away
// mux on which /restore/ is mounted for direct serving in tests.
func buildRestoreServer() (*oob.OOBRestore, *http.ServeMux, *fakeRestoreRunner) {
	runner := &fakeRestoreRunner{profile: "default"}
	o := oob.NewOOBRestore(runner, time.Minute)
	o.SetBaseURL("http://127.0.0.1:9999")
	mux := http.NewServeMux()
	o.RegisterHandlers(mux)
	return o, mux, runner
}

func TestOOBRestoreFormSingleUse(t *testing.T) {
	o, mux, runner := buildRestoreServer()
	url := o.Register("default")
	require.Contains(t, url, "/restore/")

	// GET serves the form.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Recovery phrase")

	// The form's submit action must target THIS token, not an un-substituted
	// "{ token }" placeholder (a templ string-literal leak that makes the POST
	// hit /restore/%7B%20token%20%7D). Extract the token from the minted URL and
	// require the action to be the real /restore/<token> path.
	tok := strings.TrimPrefix(url, "http://127.0.0.1:9999/restore/")
	require.NotEmpty(t, tok)
	body := rec.Body.String()
	require.Contains(t, body, "action=\"/restore/"+tok+"\"", "form action must carry the real one-time token")
	require.NotContains(t, body, "{ token }", "the token placeholder must never render literally")
	require.NotContains(t, body, "%7B", "the placeholder must never be URL-encoded into the action")

	// POST from a foreign origin is rejected (CSRF).
	badReq := httptest.NewRequest(http.MethodPost, url, strings.NewReader("mnemonic=secret words"))
	badReq.Header.Set("Origin", "http://evil.example")
	badRec := httptest.NewRecorder()
	mux.ServeHTTP(badRec, badReq)
	require.Equal(t, http.StatusForbidden, badRec.Code)
	require.Zero(t, runner.calls, "no restore should run on a rejected POST")

	// POST with a matching origin runs the restore. The fake runner resolves
	// its onApproval immediately, so the response streams the Sia approval URL
	// and then the completed restore in one synchronous POST (the human sees
	// the approval link before WaitAndRegister would block on a real device).
	okReq := httptest.NewRequest(http.MethodPost, url, strings.NewReader("mnemonic=alpha+beta+gamma"))
	okReq.Header.Set("Origin", "http://127.0.0.1:9999")
	okReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	okRec := httptest.NewRecorder()
	mux.ServeHTTP(okRec, okReq)
	require.Equal(t, http.StatusOK, okRec.Code)
	require.Equal(t, 1, runner.calls, "restore must run exactly once")
	// The restore page must not be cached and must be HTML.
	require.Equal(t, "no-store", okRec.Header().Get("Cache-Control"), "restore page must not be cached")
	require.Contains(t, okRec.Header().Get("Content-Type"), "text/html", "restore page must be HTML")
	// The approval URL is surfaced to the human before completion.
	require.Contains(t, okRec.Body.String(), "http://approve.sia", "the Sia approval URL must be streamed to the browser")
	// And the result is rendered.
	require.Contains(t, okRec.Body.String(), "vault-abc123")
	// Streamed fragments must land INSIDE the page (before </body></html>), not
	// as raw siblings after a closed document.
	rbody := okRec.Body.String()
	require.True(t, strings.Contains(rbody, "</html>"), "the restore progress page must close its document")
	require.True(t, strings.Index(rbody, "http://approve.sia") < strings.Index(rbody, "</body>"),
		"the streamed approval link must appear before the closing </body>")
	require.True(t, strings.Index(rbody, "vault-abc123") < strings.Index(rbody, "</body>"),
		"the streamed result must appear before the closing </body>")
	require.True(t, strings.Index(rbody, `id="status"`) < strings.Index(rbody, "</body>"),
		"the #status container must open before </body> so fragments render inside it")

	// The token is now consumed: a repeat POST is spent (410 + spent page),
	// not found, and must not run restore again.
	repeat := httptest.NewRequest(http.MethodPost, url, strings.NewReader("mnemonic=again"))
	repeat.Header.Set("Origin", "http://127.0.0.1:9999")
	repeat.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	repeatRec := httptest.NewRecorder()
	mux.ServeHTTP(repeatRec, repeat)
	require.Equal(t, http.StatusGone, repeatRec.Code)
	require.Contains(t, repeatRec.Body.String(), "no longer active")
	require.Equal(t, 1, runner.calls, "a repeat POST must not run restore a second time")
}

func TestOOBRestoreExpiry(t *testing.T) {
	o, mux, _ := buildRestoreServer()
	url := o.Register("default")

	// Advance past expiry.
	o.SetNow(func() time.Time { return time.Now().Add(2 * time.Minute) })

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusGone, rec.Code, "an expired restore link must render 410 with the spent page")
	require.Contains(t, rec.Body.String(), "no longer active")
	require.Contains(t, rec.Body.String(), "expired")
	require.NotContains(t, rec.Body.String(), "Recovery phrase", "an expired restore must not render the form")
}

// TestSeedDropStdioLoopback verifies the stdio path (no base URL): Register
// returns a reachable 127.0.0.1 loopback URL and the drop confirms via POST.
func TestSeedDropStdioLoopback(t *testing.T) {
	d := oob.NewSeedDrop(time.Minute)
	// No base URL -> loopback listener is started lazily by Register.
	url := d.Register("default", "loopback seed words")
	require.True(t, strings.HasPrefix(url, "http://127.0.0.1:"), "expected loopback URL, got %q", url)
	defer d.Stop(context.Background())

	// Hit the real loopback listener; the seed is shown with a confirm form.
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(body), "loopback seed words")

	// A re-open BEFORE confirmation still renders the seed (a failed transport
	// or prefetch must not strand the human with a dead link).
	resp2, err := client.Get(url)
	require.NoError(t, err)
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode, "a GET before confirmation must still render the seed")
	require.Contains(t, string(body2), "loopback seed words")

	// The explicit, same-origin confirmation POST consumes the drop. The
	// accepted origin is the loopback URL host; a browser form submission sends
	// an Origin/Referer matching it, so mirror that.
	post, err := http.NewRequest(http.MethodPost, url, nil)
	require.NoError(t, err)
	post.Header.Set("Origin", url[:strings.Index(url, "/seed/")])
	resp3, err := client.Do(post)
	require.NoError(t, err)
	io.Copy(io.Discard, resp3.Body)
	resp3.Body.Close()
	require.Equal(t, http.StatusOK, resp3.StatusCode)

	// After confirmation the link is spent (410 + spent page).
	resp4, err := client.Get(url)
	require.NoError(t, err)
	defer resp4.Body.Close()
	body4, _ := io.ReadAll(resp4.Body)
	require.Equal(t, http.StatusGone, resp4.StatusCode)
	require.Contains(t, string(body4), "no longer active")
	require.NotContains(t, string(body4), "loopback seed words", "the seed must not be shown after confirmation")
}

// TestOOBRestoreStdioLoopback verifies the stdio path mints a loopback URL.
func TestOOBRestoreStdioLoopback(t *testing.T) {
	o := oob.NewOOBRestore(&fakeRestoreRunner{profile: "default"}, time.Minute)
	url := o.Register("default")
	require.True(t, strings.HasPrefix(url, "http://127.0.0.1:"), "expected loopback URL, got %q", url)

	// The form is reachable over the loopback listener.
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	defer o.Stop(context.Background())
}

// TestOOBRestoreValidationFailureDoesNotConsumeToken verifies a validation
// failure (empty mnemonic) does not burn the one-time restore URL: no restore
// ran, so the human can retry with a valid phrase.
func TestOOBRestoreValidationFailureDoesNotConsumeToken(t *testing.T) {
	o, mux, runner := buildRestoreServer()
	url := o.Register("default")

	// POST with an empty mnemonic fails validation but must not consume.
	empty := httptest.NewRequest(http.MethodPost, url, strings.NewReader("mnemonic="))
	empty.Header.Set("Origin", "http://127.0.0.1:9999")
	empty.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	emptyRec := httptest.NewRecorder()
	mux.ServeHTTP(emptyRec, empty)
	require.Equal(t, http.StatusBadRequest, emptyRec.Code)
	require.Zero(t, runner.calls, "no restore should run on an empty mnemonic")

	// The token must still be valid: a follow-up valid POST succeeds.
	okReq := httptest.NewRequest(http.MethodPost, url, strings.NewReader("mnemonic=alpha+beta+gamma"))
	okReq.Header.Set("Origin", "http://127.0.0.1:9999")
	okReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	okRec := httptest.NewRecorder()
	mux.ServeHTTP(okRec, okReq)
	require.Equal(t, http.StatusOK, okRec.Code, "a validation failure must not consume the one-time URL")
	require.Equal(t, 1, runner.calls, "the retry must run the restore exactly once")
}

// TestOOBRestoreErrorEscapedAndMarked verifies a genuine restore failure is
// unambiguous and safe: the page is not cached, the failure is surfaced with a
// distinct machine-detectable marker, the error text is escaped (no
// reflection/self-XSS), and the one-time token is consumed after the attempt.
// The HTTP status stays 200 because the page is streamed (the approval link is
// written before the outcome is known), so a 5xx cannot be applied; the
// distinct error marker is what makes the failure detectable.
func TestOOBRestoreErrorEscapedAndMarked(t *testing.T) {
	o, mux, runner := buildRestoreServer()
	// An error carrying raw markup exercises the escape path for a
	// reflection/self-XSS vector.
	runner.err = errors.New(`bad <script>alert("x")</script> input`)
	url := o.Register("default")

	postReq := httptest.NewRequest(http.MethodPost, url, strings.NewReader("mnemonic=alpha+beta+gamma"))
	postReq.Header.Set("Origin", "http://127.0.0.1:9999")
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRec := httptest.NewRecorder()
	mux.ServeHTTP(postRec, postReq)
	require.Equal(t, 1, runner.calls)
	// Not cached, and the failure is clearly marked.
	require.Equal(t, "no-store", postRec.Header().Get("Cache-Control"))
	require.Contains(t, postRec.Header().Get("Content-Type"), "text/html")
	require.Contains(t, postRec.Body.String(), `id="restore-error"`, "the failure must carry a distinct detectable marker")

	// The error is escaped in the streamed page (no raw injected markup).
	require.Contains(t, postRec.Body.String(), html.EscapeString(runner.err.Error()))
	require.NotContains(t, postRec.Body.String(), `alert("x")`, "error markup must be escaped, not reflected as executable HTML")
	require.NotContains(t, postRec.Body.String(), `<script>alert`, "the raw script tag must not be reflected verbatim")

	// The token is consumed after a genuine (failed) attempt: a re-POST is spent.
	repeat := httptest.NewRequest(http.MethodPost, url, strings.NewReader("mnemonic=again"))
	repeat.Header.Set("Origin", "http://127.0.0.1:9999")
	repeat.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	repeatRec := httptest.NewRecorder()
	mux.ServeHTTP(repeatRec, repeat)
	require.Equal(t, http.StatusGone, repeatRec.Code, "a genuine restore attempt consumes the one-time URL even on failure")
	require.Equal(t, 1, runner.calls, "a repeat POST must not run restore a second time")
}

// TestOOBRestoreConcurrentPOSTRejectedDuringApproval verifies the token is
// claimed atomically before the blocking restore, so a concurrent POST during
// the browser-approval window cannot re-run RunRestore (which would issue a
// second Sia approval or register a second device for the same seed). The first
// POST claims the token and blocks; the second is rejected and never reaches
// RunRestore.
// TestOOBRestorePruneOutcomes verifies that terminal outcome records older than
// the restore TTL are reaped, bounding the per-token outcome map even when a
// restore is never resumed. Non-terminal (in-progress) and fresh terminal
// records are retained.
// TestOOBRestoreConsumePOSTReportsConsumed verifies consumePOST returns true
// once the token is claimed (a genuine restore attempt), and false on a
// validation failure that runs no restore, so the core's dispatch removes the
// token only on a real attempt. This asserts the handler honors the
// consumePOST -> remove dispatch contract.
