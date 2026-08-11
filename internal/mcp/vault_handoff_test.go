package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
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

// vaultRestoreJSONRoot builds a minimal root whose `vault restore` Action emits
// the agent-mode JSON that declares the restore profile, so the invoke path can
// mint a restore_url and a resume handle.
func vaultRestoreJSONRoot(t *testing.T) (*cli.Command, *bool) {
	var ran bool
	root := &cli.Command{
		Name:  "pinner",
		Flags: []cli.Flag{&cli.BoolFlag{Name: "agent", Usage: "agent mode"}},
		Commands: []*cli.Command{
			{
				Name: "vault",
				Commands: []*cli.Command{
					{
						Name: "restore",
						Action: func(ctx context.Context, cmd *cli.Command) error {
							ran = true
							_, err := cmd.Root().Writer.Write([]byte(`{"profile":"default"}`))
							return err
						},
					},
				},
			},
		},
	}
	return root, &ran
}

func TestVaultRestoreStartHandoffIncludesHandleAndResumeTool(t *testing.T) {
	root, ran := vaultRestoreJSONRoot(t)
	oob, _, _ := buildRestoreServer()
	handles := NewAsyncHandleStore(DefaultSessionTTL, DefaultMaxSessions)
	reg := NewHandoffRegistry()
	catalog, err := buildCatalog(root, true, nil, nil, oob, reg, handles)
	require.NoError(t, err)

	res, err := catalog.Invoke(context.Background(), vaultRestoreToolName, map[string]any{})
	require.NoError(t, err)
	require.True(t, *ran)
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
	dir := t.TempDir()
	seedPath := filepath.Join(dir, "recovery.seed")
	require.NoError(t, os.WriteFile(seedPath, []byte("one two three\n"), 0600))

	var ran bool
	root := &cli.Command{
		Name:  "pinner",
		Flags: []cli.Flag{&cli.BoolFlag{Name: "agent", Usage: "agent mode"}},
		Commands: []*cli.Command{
			{
				Name: "vault",
				Commands: []*cli.Command{
					{
						Name: "create",
						Action: func(ctx context.Context, cmd *cli.Command) error {
							ran = true
							_, err := cmd.Root().Writer.Write([]byte(`{"profile":"default","seed_path":` + mustJSONQuote(seedPath) + `}`))
							return err
						},
					},
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

	res, err := catalog.Invoke(context.Background(), vaultCreateToolName, map[string]any{})
	require.NoError(t, err)
	require.True(t, ran)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok, "create hand-off must carry structured content")
	require.Contains(t, sc["seed_url"].(string), "/seed/")
	require.NotEmpty(t, sc["handle"])
	assert.Equal(t, vaultCreateResumeToolName, sc["resume_tool"])

	// Seed-never-transits assertion: the mnemonic must not appear anywhere.
	blob, _ := json.Marshal(sc)
	require.NotContains(t, string(blob), "one two three")
	require.NotContains(t, res.Text, "one two three")
}

// TestMintVaultHandoffDegradesWithoutResumeMachinery verifies that when the
// resume machinery (registry/handles) is absent, mintVaultHandoff returns
// empty handle so the single-shot hand-off is preserved unchanged.
func TestMintVaultHandoffDegradesWithoutResumeMachinery(t *testing.T) {
	restoreEntry := &ToolEntry{Behavior: ToolBehavior{RestoreURL: &RestoreURLSpec{ProfileField: "profile"}}}
	h, rt := mintVaultHandoff(restoreEntry, "http://127.0.0.1:9999/restore/tok", "", nil, nil, nil, nil)
	assert.Empty(t, h)
	assert.Empty(t, rt)
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
