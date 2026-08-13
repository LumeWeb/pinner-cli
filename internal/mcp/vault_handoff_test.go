package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// ---- resume-tool (template) tests -----------------------------------------

// requireVaultDone extracts a terminal "done" structured result.
func requireVaultDone(t *testing.T, r ToolResult) {
	t.Helper()
	require.False(t, r.IsError, "expected done result, got error: %s", r.Text)
	sc, ok := r.StructuredContent.(map[string]any)
	require.True(t, ok)
	require.Equal(t, StatusDone, sc["status"])
}

// TestVaultCreateResumePendingToDone verifies vault_create_resume (the
// shared NewResumeTool template with the create continuation registered) polls
// a create hand-off from pending to done as the vault is created/activated and
// the fresh seed is retrieved from its one-time seeddrop.
func TestVaultCreateResumePendingToDone(t *testing.T) {
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	oob, mux, _ := buildCreateServer()

	// Simulate a create start: mint an OOB create URL, then register the create
	// continuation against a handle holding the token (as the invoke path does).
	createURL := oob.Register("default")
	token := vaultTokenFromURL(createURL)
	require.NotEmpty(t, token)
	handle := handles.Create("pending", map[string]any{handleDataToken: token})
	reg.Begin(handle, vaultCreateResumeContinuation(oob, handles, reg))

	resume := NewVaultCreateResumeDescriptor(reg, handles)

	// Before the create page is acted on: pending needs_human.
	r, err := resume.Handler(context.Background(), ToolRequest{
		Name:      vaultCreateResumeToolName,
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, r)
	assert.Equal(t, vaultCreateResumeToolName, sc["resume_tool"])
	assert.NotContains(t, r.Text, "fresh generated seed phrase", "the seed must never appear in resume text")
	assert.NotContains(t, sc, "fresh generated seed phrase")

	// Drive the create: POST the create page. This runs the fake runner, which
	// activates the vault and mints a one-time seeddrop for the fresh seed. The
	// seeddrop URL is embedded in the streamed page.
	postReq := httptest.NewRequest("POST", createURL, nil)
	postReq.Header.Set("Origin", "http://127.0.0.1:9999")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, postReq)
	require.Equal(t, http.StatusOK, rec.Code)
	// Extract the seed_url from the streamed done page.
	seedLink := rec.Body.String()
	require.Contains(t, seedLink, "seed-link")
	seedURL := extractSeedURL(t, seedLink)
	require.NotEmpty(t, seedURL)

	// Resume now: the vault is active but the seed not yet retrieved -> pending.
	r, err = resume.Handler(context.Background(), ToolRequest{
		Name:      vaultCreateResumeToolName,
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	requireHandoff(t, r) // still needs_human: seed not picked up yet

	// Confirm the seed the way a browser would: GET to render, then the
	// explicit same-origin confirmation POST consumes the seeddrop. A bare GET
	// (which cannot prove delivery) must leave the state pending.
	recSeedGet := httptest.NewRecorder()
	mux.ServeHTTP(recSeedGet, httptest.NewRequest("GET", seedURL, nil))
	require.Equal(t, http.StatusOK, recSeedGet.Code)
	r, err = resume.Handler(context.Background(), ToolRequest{
		Name:      vaultCreateResumeToolName,
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	requireHandoff(t, r) // still needs_human: seed shown but not confirmed yet

	recSeed := httptest.NewRecorder()
	confirmReq := httptest.NewRequest("POST", seedURL, nil)
	confirmReq.Header.Set("Origin", "http://127.0.0.1:9999")
	mux.ServeHTTP(recSeed, confirmReq)
	require.Equal(t, http.StatusOK, recSeed.Code)

	// Now resume reports terminal done.
	r, err = resume.Handler(context.Background(), ToolRequest{
		Name:      vaultCreateResumeToolName,
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	requireVaultDone(t, r)
	assert.NotContains(t, r.Text, "fresh generated seed phrase")
}

// extractSeedURL pulls the /seed/<token> href out of a streamed create done
// page. It looks for the href after the seed-link marker (the page also carries
// the Sia approval href earlier, which must be skipped).
func extractSeedURL(t *testing.T, body string) string {
	t.Helper()
	marker := "seed-link"
	idx := strings.LastIndex(body, marker)
	if idx < 0 {
		return ""
	}
	rest := body[idx:]
	start := strings.Index(rest, "href=\"")
	if start < 0 {
		return ""
	}
	rest = rest[start+len("href=\""):]
	end := strings.Index(rest, "\"")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// TestVaultRestoreResumePendingToDone verifies vault_restore_resume
// polls a restore hand-off from pending to done as the human submits the OOB
// restore form. It models the OOB restore coordinator with a fake
// RestoreRunner (no network).
func TestVaultRestoreResumePendingToDone(t *testing.T) {
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	oob, mux, runner := buildRestoreServer()

	url := oob.Register("default")
	token := vaultTokenFromURL(url)
	require.NotEmpty(t, token)
	handle := handles.Create("pending", map[string]any{handleDataToken: token})
	reg.Begin(handle, vaultRestoreResumeContinuation(oob, handles, reg))

	resume := NewVaultRestoreResumeDescriptor(reg, handles)

	// Before restore: pending.
	r, err := resume.Handler(context.Background(), ToolRequest{
		Name:      vaultRestoreResumeToolName,
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, r)
	assert.Equal(t, vaultRestoreResumeToolName, sc["resume_tool"])
	assert.NotContains(t, r.Text, "secret words", "the seed must never appear in resume text")

	// Submit the restore form the way a browser POST would (single-use collect).
	postReq := httptest.NewRequest("POST", url, strings.NewReader("mnemonic=secret+words"))
	postReq.Header.Set("Origin", "http://127.0.0.1:9999")
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRec := httptest.NewRecorder()
	mux.ServeHTTP(postRec, postReq)
	require.Equal(t, 200, postRec.Code)
	require.Equal(t, 1, runner.calls)

	// Now resume reports terminal done.
	r, err = resume.Handler(context.Background(), ToolRequest{
		Name:      vaultRestoreResumeToolName,
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	requireVaultDone(t, r)
	assert.NotContains(t, r.Text, "secret words")
}

// TestVaultRestoreResumeFailedSteersRestart verifies that when RunRestore fails
// (wrong mnemonic or Sia approval/registration error), the restore resume
// continuation steers the agent to restart instead of reporting "the vault has
// been restored". The claimed token's outcome is recorded as failed and never
// maps to StatusDone, matching the OOB page's error banner.
func TestVaultRestoreResumeFailedSteersRestart(t *testing.T) {
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	oob, mux, runner := buildRestoreServer()
	runner.err = errors.New("approval/registration failed: seed rejected")

	url := oob.Register("default")
	token := vaultTokenFromURL(url)
	require.NotEmpty(t, token)
	handle := handles.Create("pending", map[string]any{handleDataToken: token})
	reg.Begin(handle, vaultRestoreResumeContinuation(oob, handles, reg))

	resume := NewVaultRestoreResumeDescriptor(reg, handles)

	// Submit the restore form; RunRestore fails and records a failed outcome.
	postReq := httptest.NewRequest("POST", url, strings.NewReader("mnemonic=secret+words"))
	postReq.Header.Set("Origin", "http://127.0.0.1:9999")
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRec := httptest.NewRecorder()
	mux.ServeHTTP(postRec, postReq)
	require.Equal(t, 1, runner.calls)

	// Resume must steer to restart, NOT report done.
	r, err := resume.Handler(context.Background(), ToolRequest{
		Name:      vaultRestoreResumeToolName,
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, r)
	assert.Equal(t, compiledVaultRestoreToolName, sc["resume_tool"], "a failed restore must steer back to pinner_vault_restore")
	assert.NotEqual(t, StatusDone, sc["status"], "a failed restore must never report StatusDone")
	assert.NotContains(t, r.Text, "secret words")
	assert.NotContains(t, r.Text, "has been restored", "must not claim the vault was restored on failure")
}

// TestVaultRestoreResumePendingDuringApproval verifies that after the browser
// form is submitted but while RunRestore is still blocked on the Sia approval,
// the resume continuation keeps reporting pending (needs_human) rather than
// treating the claimed-but-unsettled token as a dead hand-off and steering the
// agent to restart mid-approval.
func TestVaultRestoreResumePendingDuringApproval(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	oob := NewOOBRestore(&fakeRestoreRunner{profile: "default", started: started, release: release}, time.Minute)
	oob.SetBaseURL("http://127.0.0.1:9999")
	mux := http.NewServeMux()
	oob.registerHandlers(mux)

	url := oob.Register("default")
	token := vaultTokenFromURL(url)
	require.NotEmpty(t, token)
	handle := handles.Create("pending", map[string]any{handleDataToken: token})
	reg.Begin(handle, vaultRestoreResumeContinuation(oob, handles, reg))

	resume := NewVaultRestoreResumeDescriptor(reg, handles)

	// Submit the form in a goroutine; RunRestore blocks on the approval.
	postReq := httptest.NewRequest("POST", url, strings.NewReader("mnemonic=secret+words"))
	postReq.Header.Set("Origin", "http://127.0.0.1:9999")
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRec := httptest.NewRecorder()
	go mux.ServeHTTP(postRec, postReq)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("restore did not start")
	}

	// Resume while the approval is outstanding: must remain pending (needs_human),
	// NOT a dead-handle steer to restart.
	r, err := resume.Handler(context.Background(), ToolRequest{
		Name:      vaultRestoreResumeToolName,
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, r)
	assert.Equal(t, vaultRestoreResumeToolName, sc["resume_tool"], "a mid-approval restore must keep reporting needs_human to the resume tool")
	assert.NotEqual(t, StatusDone, sc["status"], "a mid-approval restore must not report done")
	assert.NotEqual(t, compiledVaultRestoreToolName, sc["resume_tool"], "a mid-approval restore must not steer to restart")

	// Let the restore finish. Poll until the continuation observes the settled
	// outcome rather than racing the blocking restore's completion.
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		r, err = resume.Handler(context.Background(), ToolRequest{
			Name:      vaultRestoreResumeToolName,
			Arguments: map[string]any{"handle": handle},
		})
		require.NoError(t, err)
		if sc, ok := r.StructuredContent.(map[string]any); ok && sc["status"] == StatusDone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("restore did not settle to done within deadline: " + r.Text)
		}
		time.Sleep(10 * time.Millisecond)
	}
	requireVaultDone(t, r)
}

// TestVaultRestoreResumeFreesOutcome verifies that once the continuation
// consumes a terminal result, the per-token outcome record is removed from the
// coordinator map, so settled restores do not accumulate.
func TestVaultRestoreResumeFreesOutcome(t *testing.T) {
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	oob, mux, _ := buildRestoreServer()

	url := oob.Register("default")
	token := vaultTokenFromURL(url)
	require.NotEmpty(t, token)
	handle := handles.Create("pending", map[string]any{handleDataToken: token})
	reg.Begin(handle, vaultRestoreResumeContinuation(oob, handles, reg))
	resume := NewVaultRestoreResumeDescriptor(reg, handles)

	postReq := httptest.NewRequest("POST", url, strings.NewReader("mnemonic=secret+words"))
	postReq.Header.Set("Origin", "http://127.0.0.1:9999")
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(httptest.NewRecorder(), postReq)

	// Outcome was recorded while restoring, then freed once the continuation
	// consumed the terminal done result.
	oob.mu.Lock()
	_, wasRecorded := oob.outcomes[token]
	oob.mu.Unlock()
	require.True(t, wasRecorded, "outcome should be recorded once the restore settles")

	_, err := resume.Handler(context.Background(), ToolRequest{
		Name:      vaultRestoreResumeToolName,
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)

	oob.mu.Lock()
	_, stillThere := oob.outcomes[token]
	oob.mu.Unlock()
	assert.False(t, stillThere, "a consumed terminal outcome must be freed from the map")
}

// TestVaultRestoreResumeDeadHandleSteersRestart verifies that an unknown or
// expired handle passed to vault_restore_resume steers the agent back
// to pinner_vault_restore instead of retrying a dead handle.
func TestVaultRestoreResumeDeadHandleSteersRestart(t *testing.T) {
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	resume := NewVaultRestoreResumeDescriptor(reg, handles)

	// Unknown handle.
	r, err := resume.Handler(context.Background(), ToolRequest{
		Name:      vaultRestoreResumeToolName,
		Arguments: map[string]any{"handle": "does-not-exist"},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, r)
	assert.Equal(t, compiledVaultRestoreToolName, sc["resume_tool"], "restore dead handle must steer to pinner_vault_restore")
	assert.Contains(t, sc["detail"].(string), "unknown handle")

	// Expired handle.
	handle := handles.Create("pending", map[string]any{handleDataToken: "tok"})
	handles.now = func() time.Time { return time.Now().Add(2 * DefaultSessionTTL) }
	r, err = resume.Handler(context.Background(), ToolRequest{
		Name:      vaultRestoreResumeToolName,
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc = requireHandoff(t, r)
	assert.Equal(t, compiledVaultRestoreToolName, sc["resume_tool"], "restore expired handle must steer to pinner_vault_restore")
	assert.Contains(t, sc["detail"].(string), "expired")
}

// TestVaultCreateResumeDeadHandleSteersRestart verifies an unknown handle
// passed to vault_create_resume steers back to pinner_vault_create.
func TestVaultCreateResumeDeadHandleSteersRestart(t *testing.T) {
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	resume := NewVaultCreateResumeDescriptor(reg, handles)

	r, err := resume.Handler(context.Background(), ToolRequest{
		Name:      vaultCreateResumeToolName,
		Arguments: map[string]any{"handle": "nope"},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, r)
	assert.Equal(t, compiledVaultCreateToolName, sc["resume_tool"], "create dead handle must steer to pinner_vault_create")
}

// TestVaultResumeNotConfigured verifies the resume templates degrade to a
// structured not-configured hand-off when the machinery is absent.
func TestVaultResumeNotConfigured(t *testing.T) {
	r, err := NewVaultCreateResumeDescriptor(nil, nil).Handler(context.Background(), ToolRequest{
		Name: vaultCreateResumeToolName, Arguments: map[string]any{"handle": "x"},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, r)
	assert.Equal(t, ReasonInteractiveOnly, sc["reason"])
}

// ---- start hand-off structured content (full invoke path) -----------------

func TestVaultRestoreStartHandoffIncludesHandleAndResumeTool(t *testing.T) {
	// Isolate vault paths so resolveRestoreProfile's registry read never
	// depends on a real host registry (which varies across CI environments and
	// platforms). Mirrors the create-start test below.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	}

	oob, _, _ := buildRestoreServer()
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	catalog, err := buildCatalog(compilerRoot(), true, nil, nil, oob, nil, reg, handles,
		withCatalogDeps(func() *CatalogDepsBundle {
			return &CatalogDepsBundle{VaultSetup: catalogops.VaultDeps{}}
		}))
	require.NoError(t, err)

	// The compiled vault.restore entry is routed through the OOB setup handler.
	restoreEntry, ok := catalog.Get(compiledVaultRestoreToolName)
	require.True(t, ok, "compiled vault.restore must be present in compiler mode")

	// Invoke the restore tool through the compiled entry: it resolves the
	// target profile (no profile/env/registry -> "default") and mints a
	// one-time restore_url + resume handle from the OOB coordinator, without
	// relying on CLI stdout.
	res, err := restoreEntry.Handler(context.Background(), ToolRequest{Name: compiledVaultRestoreToolName, Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, res.IsError, "compiled vault.restore must produce a hand-off: %s", res.Text)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok, "restore hand-off must carry structured content")
	// Both the one-time URL and the resume handle + resume tool must be present.
	require.Contains(t, sc["restore_url"].(string), "/restore/")
	require.NotEmpty(t, sc["handle"])
	assert.Equal(t, vaultRestoreResumeToolName, sc["resume_tool"])

	// The single-shot rollback invariant holds: the restore_url is returned as
	// before; the handle is an addition.
	assert.NotContains(t, sc, "secret words")
	assert.NotContains(t, res.Text, "secret words")
}

func TestVaultCreateStartHandoffIncludesHandleAndResumeTool(t *testing.T) {
	// Isolate vault paths so nothing reaches the seeded hub/core config.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	}

	oobCreate, _, _ := buildCreateServer()
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	catalog, err := buildCatalog(compilerRoot(), true, nil, nil, nil, oobCreate, reg, handles,
		withCatalogDeps(func() *CatalogDepsBundle {
			return &CatalogDepsBundle{VaultSetup: catalogops.VaultDeps{}}
		}))
	require.NoError(t, err)

	createEntry, ok := catalog.Get(compiledVaultCreateToolName)
	require.True(t, ok, "compiled vault.create must be present in compiler mode")

	// Invoke the create tool through the compiled entry: it targets the
	// requested profile for an out-of-band create (SSO + activation runs in the
	// browser), then the MCP layer mints a one-time create_url + resume handle.
	// No seed is minted or written at invoke time. The mnemonic must never
	// appear in the result.
	res, err := createEntry.Handler(context.Background(), ToolRequest{Name: compiledVaultCreateToolName, Arguments: map[string]any{"profile": "testcreate"}})
	require.NoError(t, err)
	require.False(t, res.IsError, "compiled vault.create must produce a hand-off: %s", res.Text)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok, "create hand-off must carry structured content")
	require.Contains(t, sc["create_url"].(string), "/create/")
	require.NotEmpty(t, sc["handle"])
	assert.Equal(t, vaultCreateResumeToolName, sc["resume_tool"])

	// The create must not have written a pending seed file or registered a
	// pending profile at invoke time; both happen out-of-band on browser POST.
	_, statErr := os.Stat(vault.SeedPath("testcreate"))
	assert.True(t, os.IsNotExist(statErr), "no seed file should be written at create hand-off time")
	reg2, err := vault.LoadRegistry()
	require.NoError(t, err)
	_, ok = reg2.Profiles["testcreate"]
	assert.False(t, ok, "no pending profile should be registered at create hand-off time")
}

// TestVaultCreateSetupHandlerMintsOneTimeCreateURL verifies the
// pinner_vault_create setup handler mints an out-of-band create_url via the
// OOB create coordinator against the requested profile, and that the seed is
// NOT minted at invoke time (it is generated only when the human approves Sia
// in the browser). camelCase-aliased args are accepted through the catalog's
// NormalizeOperationInput.
func TestVaultCreateSetupHandlerMintsOneTimeCreateURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	}

	oob, _, _ := buildCreateServer()
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()

	handler := vaultCreateSetupHandler(oob, reg, handles)
	res, err := handler(context.Background(), ToolRequest{
		Name: compiledVaultCreateToolName,
		Arguments: map[string]any{
			"profile": "aliasdev",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "create must succeed with a profile: %s", res.Text)

	// The handler must return a needs_human hand-off minting a one-time create
	// URL against the OOB create coordinator (the vault is created + activated
	// in the browser, not as a pending profile at op time).
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok, "create hand-off must carry structured content")
	createURL, _ := sc["create_url"].(string)
	require.Contains(t, createURL, "/create/", "the create hand-off must mint a one-time create_url")
	require.NotEmpty(t, sc["handle"], "the create hand-off must carry a resume handle")

	// The seed must never have been minted at invoke time; it is generated only
	// after the human approves the Sia connection in the browser.
	_, statErr := os.Stat(vault.SeedPath("aliasdev"))
	assert.True(t, os.IsNotExist(statErr), "no seed file should be written at create hand-off time")
}

// TestVaultCreateResumeExpiredTokenSteersRestart verifies the Kody high finding:
// an EXPIRED one-time seed_url must NOT be reported as a completed vault create
// (StatusDone) — it must terminate the continuation and steer the agent to
// start a fresh pinner_vault_create. Previously tokenDone conflated consumed
// and expired tokens (both absent from the store), so polling after the TTL
// elapsed falsely read "seed retrieved". See handoffEndpoint.resolve's
// handoffUsed vs handoffExpired distinction.
func TestVaultCreateResumeExpiredTokenSteersRestart(t *testing.T) {
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	oob, _, _ := buildCreateServer()

	url := oob.Register("default")
	token := vaultTokenFromURL(url)
	require.NotEmpty(t, token)
	handle := handles.Create("pending", map[string]any{handleDataToken: token})
	reg.Begin(handle, vaultCreateResumeContinuation(oob, handles, reg))

	resume := NewVaultCreateResumeDescriptor(reg, handles)

	// Before expiry: pending.
	r, err := resume.Handler(context.Background(), ToolRequest{
		Name: vaultCreateResumeToolName, Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	requireHandoff(t, r)

	// Advance the clock past the 1m TTL so the token is handoffExpired.
	oob.setNow(func() time.Time { return time.Now().Add(2 * time.Minute) })

	// The resume must NOT report done — it must steer to a fresh vault create.
	r, err = resume.Handler(context.Background(), ToolRequest{
		Name: vaultCreateResumeToolName, Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, r)
	assert.Equal(t, compiledVaultCreateToolName, sc["resume_tool"],
		"expired create token must steer to pinner_vault_create, not report done")
	assert.NotEqual(t, StatusDone, sc["status"], "expired token must never read as a completed vault create")
	assert.NotContains(t, r.Text, "fresh generated seed phrase")

	// The expired continuation + backing handle must be cleared so the agent
	// is not left polling a dead flow: a follow-up poll now hits the
	// template's dead-handle branch (unknown handle).
	r, err = resume.Handler(context.Background(), ToolRequest{
		Name: vaultCreateResumeToolName, Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc = requireHandoff(t, r)
	assert.Equal(t, compiledVaultCreateToolName, sc["resume_tool"], "cleared expiration must read as dead-handle restart")
}

// TestVaultRestoreResumeExpiredTokenSteersRestart mirrors the create expiry
// case for the restore flow: an EXPIRED restore_url must steer to a fresh
// pinner_vault_restore rather than falsely reporting "vault has been
// restored".
func TestVaultRestoreResumeExpiredTokenSteersRestart(t *testing.T) {
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	oob, _, _ := buildRestoreServer()

	url := oob.Register("default")
	token := vaultTokenFromURL(url)
	require.NotEmpty(t, token)
	handle := handles.Create("pending", map[string]any{handleDataToken: token})
	reg.Begin(handle, vaultRestoreResumeContinuation(oob, handles, reg))

	resume := NewVaultRestoreResumeDescriptor(reg, handles)

	// Before expiry: pending.
	r, err := resume.Handler(context.Background(), ToolRequest{
		Name: vaultRestoreResumeToolName, Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	requireHandoff(t, r)

	// Advance the clock past the restore TTL so the token is handoffExpired.
	oob.setNow(func() time.Time { return time.Now().Add(2 * time.Hour) })

	r, err = resume.Handler(context.Background(), ToolRequest{
		Name: vaultRestoreResumeToolName, Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, r)
	assert.Equal(t, compiledVaultRestoreToolName, sc["resume_tool"],
		"expired restore token must steer to pinner_vault_restore, not report done")
	assert.NotEqual(t, StatusDone, sc["status"], "expired token must never read as a completed restore")
}

// TestVaultResumeAbsentTokenSteersRestart covers the Kody edge: a token that
// no longer resolves — because it never existed, or because its spent
// tombstone was evicted by pruneSpentLocked at maxSpentTombstones — must NOT
// leave the agent pending forever (done=false, expired=false, not pending).
// It must terminate the continuation and steer to a fresh start, matching how
// expired and consumed tokens are handled.
func TestVaultResumeAbsentTokenSteersRestart(t *testing.T) {
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	oob, _, _ := buildCreateServer()

	// A token that was never minted is absent from the coordinator.
	handle := handles.Create("pending", map[string]any{handleDataToken: "never-minted"})
	reg.Begin(handle, vaultCreateResumeContinuation(oob, handles, reg))
	resume := NewVaultCreateResumeDescriptor(reg, handles)

	r, err := resume.Handler(context.Background(), ToolRequest{
		Name: vaultCreateResumeToolName, Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, r)
	assert.Equal(t, compiledVaultCreateToolName, sc["resume_tool"],
		"absent token must steer to pinner_vault_create, not pending forever")
	assert.NotEqual(t, StatusDone, sc["status"], "absent token must never read as a completed create")

	// The continuation + backing handle must be cleared so the agent is not
	// left polling a dead flow: the next poll hits the dead-handle branch.
	_, _, err = handles.Get(handle)
	assert.Error(t, err, "absent-token flow must retire the backing handle")
	r, err = resume.Handler(context.Background(), ToolRequest{
		Name: vaultCreateResumeToolName, Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc = requireHandoff(t, r)
	assert.Equal(t, compiledVaultCreateToolName, sc["resume_tool"], "cleared flow must read as dead-handle restart")
}
