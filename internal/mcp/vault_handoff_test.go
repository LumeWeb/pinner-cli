package mcp

import (
	"context"
	"encoding/json"
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
	"github.com/urfave/cli/v3"
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

// TestVaultCreateResumePendingToDone verifies pinner_vault_create_resume (the
// shared NewResumeTool template with the create continuation registered) polls
// a create hand-off from pending to done as the seed drop is consumed.
func TestVaultCreateResumePendingToDone(t *testing.T) {
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	seedDrop := NewSeedDrop(time.Minute)
	seedDrop.SetBaseURL("http://127.0.0.1:9999")

	// Simulate a create start: mint a seed drop, then register the create
	// continuation against a handle holding the token (as the invoke path does).
	url := seedDrop.Register("default", "alpha beta gamma")
	token := vaultTokenFromURL(url)
	require.NotEmpty(t, token)
	handle := handles.Create("pending", map[string]any{handleDataToken: token})
	reg.Begin(handle, vaultCreateResumeContinuation(seedDrop, handles, reg))

	resume := NewVaultCreateResumeDescriptor(reg, handles)

	// Before the seed is picked up: pending needs_human.
	r, err := resume.Handler(context.Background(), ToolRequest{
		Name:      vaultCreateResumeToolName,
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, r)
	assert.Equal(t, vaultCreateResumeToolName, sc["resume_tool"])
	assert.NotContains(t, r.Text, "alpha beta gamma", "the seed must never appear in resume text")
	assert.NotContains(t, sc, "alpha beta gamma")

	// Consume the seed the way a browser GET would (single-use display).
	mux := http.NewServeMux()
	seedDrop.registerHandlers(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
	require.Equal(t, 200, rec.Code)

	// Now resume reports terminal done.
	r, err = resume.Handler(context.Background(), ToolRequest{
		Name:      vaultCreateResumeToolName,
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	requireVaultDone(t, r)
	assert.NotContains(t, r.Text, "alpha beta gamma")
}

// TestVaultRestoreResumePendingToDone verifies pinner_vault_restore_resume
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
	assert.Equal(t, vaultRestoreToolName, sc["resume_tool"], "a failed restore must steer back to pinner_vault_restore")
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
	assert.NotEqual(t, vaultRestoreToolName, sc["resume_tool"], "a mid-approval restore must not steer to restart")

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
// expired handle passed to pinner_vault_restore_resume steers the agent back
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
	assert.Equal(t, vaultRestoreToolName, sc["resume_tool"], "restore dead handle must steer to pinner_vault_restore")
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
	assert.Equal(t, vaultRestoreToolName, sc["resume_tool"], "restore expired handle must steer to pinner_vault_restore")
	assert.Contains(t, sc["detail"].(string), "expired")
}

// TestVaultCreateResumeDeadHandleSteersRestart verifies an unknown handle
// passed to pinner_vault_create_resume steers back to pinner_vault_create.
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
	assert.Equal(t, vaultCreateToolName, sc["resume_tool"], "create dead handle must steer to pinner_vault_create")
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

	root := &cli.Command{
		Name:  "pinner",
		Flags: []cli.Flag{&cli.BoolFlag{Name: "agent", Usage: "agent mode"}},
		Commands: []*cli.Command{
			{
				Name: "vault",
				Commands: []*cli.Command{
					{Name: "restore", Action: func(ctx context.Context, cmd *cli.Command) error { return nil }},
				},
			},
		},
	}
	oob, _, _ := buildRestoreServer()
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	catalog, err := buildCatalog(root, true, nil, nil, oob, reg, handles)
	require.NoError(t, err)

	// Invoke the restore tool through the catalog-op handler: it resolves the
	// target profile (no profile/env/registry -> "default") and mints a
	// one-time restore_url + resume handle from the OOB coordinator, without
	// relying on CLI stdout.
	res, err := catalog.Invoke(context.Background(), vaultRestoreToolName, map[string]any{})
	require.NoError(t, err)
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
	// Isolate vault paths so CreatePending writes seed + pending profile into a
	// temp dir, and the seeded hub/core never reaches a real config.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	}

	root := &cli.Command{
		Name:  "pinner",
		Flags: []cli.Flag{&cli.BoolFlag{Name: "agent", Usage: "agent mode"}},
		Commands: []*cli.Command{
			{
				Name: "vault",
				Commands: []*cli.Command{
					{Name: "create", Action: func(ctx context.Context, cmd *cli.Command) error { return nil }},
				},
			},
		},
	}

	seedDrop := NewSeedDrop(time.Minute)
	seedDrop.SetBaseURL("http://127.0.0.1:9999")
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	catalog, err := buildCatalog(root, true, nil, seedDrop, nil, reg, handles)
	require.NoError(t, err)

	// Invoke the create tool through the catalog-op handler: it runs
	// Provisioner.CreatePending (real fresh seed + 0600 file + pending profile),
	// then the MCP layer mints a one-time seed_url + resume handle. The mnemonic
	// must never appear in the result.
	res, err := catalog.Invoke(context.Background(), vaultCreateToolName, map[string]any{"profile": "testcreate"})
	require.NoError(t, err)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok, "create hand-off must carry structured content")
	require.Contains(t, sc["seed_url"].(string), "/seed/")
	require.NotEmpty(t, sc["handle"])
	assert.Equal(t, vaultCreateResumeToolName, sc["resume_tool"])

	// Seed-never-transits assertion: the actual generated mnemonic must not
	// appear anywhere in the hand-off (Text or StructuredContent). Reading it
	// from the seed file enforces the real guarantee rather than a constant.
	mnemonic, err := os.ReadFile(vault.SeedPath("testcreate"))
	require.NoError(t, err, "seed file must exist under the isolated home")
	require.NotEmpty(t, mnemonic)
	blob, _ := json.Marshal(sc)
	require.NotContains(t, string(blob), string(mnemonic), "mnemonic must never appear in structured content")
	require.NotContains(t, res.Text, string(mnemonic), "mnemonic must never appear in hand-off text")
	// The pending profile + seed file must exist under the isolated home.
	reg2, err := vault.LoadRegistry()
	require.NoError(t, err)
	require.Contains(t, reg2.Profiles, "testcreate")
}

// TestVaultCreateSetupHandlerAliasesCamelCaseDeviceName verifies the
// pinner_vault_create setup handler routes args through the catalog's
// NormalizeOperationInput, so a model sending camelCase "deviceName" for the
// kebab-declared "device-name" arg reaches the op handler (and the resulting
// profile) instead of being silently dropped to the empty-string default.
func TestVaultCreateSetupHandlerAliasesCamelCaseDeviceName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	}

	seedDrop := NewSeedDrop(time.Minute)
	seedDrop.SetBaseURL("http://127.0.0.1:9999")
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()

	handler := vaultCreateSetupHandler(seedDrop, reg, handles)
	res, err := handler(context.Background(), ToolRequest{
		Name: vaultCreateToolName,
		Arguments: map[string]any{
			"profile":    "aliasdev",
			"deviceName": "agent-issued-device",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "create must succeed with camelCase deviceName: %s", res.Text)

	// The aliased device name must land on the pending profile.
	loaded, err := vault.LoadRegistry()
	require.NoError(t, err)
	prof, ok := loaded.Profiles["aliasdev"]
	require.True(t, ok, "pending profile must exist")
	assert.Equal(t, "agent-issued-device", prof.DeviceName)
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
	seedDrop := NewSeedDrop(time.Minute)
	seedDrop.SetBaseURL("http://127.0.0.1:9999")

	url := seedDrop.Register("default", "alpha beta gamma")
	token := vaultTokenFromURL(url)
	require.NotEmpty(t, token)
	handle := handles.Create("pending", map[string]any{handleDataToken: token})
	reg.Begin(handle, vaultCreateResumeContinuation(seedDrop, handles, reg))

	resume := NewVaultCreateResumeDescriptor(reg, handles)

	// Before expiry: pending.
	r, err := resume.Handler(context.Background(), ToolRequest{
		Name: vaultCreateResumeToolName, Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	requireHandoff(t, r)

	// Advance the clock past the 1m TTL so the token is handoffExpired.
	seedDrop.setNow(func() time.Time { return time.Now().Add(2 * time.Minute) })

	// The resume must NOT report done — it must steer to a fresh vault create.
	r, err = resume.Handler(context.Background(), ToolRequest{
		Name: vaultCreateResumeToolName, Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, r)
	assert.Equal(t, vaultCreateToolName, sc["resume_tool"],
		"expired seed token must steer to pinner_vault_create, not report done")
	assert.NotEqual(t, StatusDone, sc["status"], "expired token must never read as a completed vault create")
	assert.NotContains(t, r.Text, "alpha beta gamma")

	// The expired continuation + backing handle must be cleared so the agent
	// is not left polling a dead flow: a follow-up poll now hits the
	// template's dead-handle branch (unknown handle).
	r, err = resume.Handler(context.Background(), ToolRequest{
		Name: vaultCreateResumeToolName, Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc = requireHandoff(t, r)
	assert.Equal(t, vaultCreateToolName, sc["resume_tool"], "cleared expiration must read as dead-handle restart")
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
	assert.Equal(t, vaultRestoreToolName, sc["resume_tool"],
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
	seedDrop := NewSeedDrop(time.Minute)
	seedDrop.SetBaseURL("http://127.0.0.1:9999")

	// A token that was never minted is absent from the coordinator.
	handle := handles.Create("pending", map[string]any{handleDataToken: "never-minted"})
	reg.Begin(handle, vaultCreateResumeContinuation(seedDrop, handles, reg))
	resume := NewVaultCreateResumeDescriptor(reg, handles)

	r, err := resume.Handler(context.Background(), ToolRequest{
		Name: vaultCreateResumeToolName, Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)
	sc := requireHandoff(t, r)
	assert.Equal(t, vaultCreateToolName, sc["resume_tool"],
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
	assert.Equal(t, vaultCreateToolName, sc["resume_tool"], "cleared flow must read as dead-handle restart")
}
