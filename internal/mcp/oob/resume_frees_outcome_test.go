package oob

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/handoff"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/session"
)

// buildRestoreTestServer wires an OOBRestore with a fake runner and a mux with
// /restore/ mounted for tests that need a live restore hand-off.
func buildRestoreTestServer() (*OOBRestore, *http.ServeMux, *fakeRestoreRunner) {
	runner := &fakeRestoreRunner{profile: "default"}
	o := NewOOBRestore(runner, time.Minute)
	o.SetBaseURL("http://127.0.0.1:9999")
	mux := http.NewServeMux()
	o.RegisterHandlers(mux)
	return o, mux, runner
}

// TestVaultRestoreResumeFreesOutcome verifies that once the continuation
// consumes a terminal result, the per-token outcome record is removed from the
// coordinator map, so settled restores do not accumulate.
func TestVaultRestoreResumeFreesOutcome(t *testing.T) {
	handles := session.NewAsyncHandleStore(session.DefaultSessionTTL, session.DefaultMaxSessions)
	reg := handoff.NewHandoffRegistry()
	oob, mux, _ := buildRestoreTestServer()

	url := oob.Register("default")
	token := VaultTokenFromURL(url)
	require.NotEmpty(t, token)
	handle := handles.Create("pending", map[string]any{HandleDataToken: token})
	reg.Begin(handle, VaultRestoreResumeContinuation(oob, handles, reg))
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

	_, err := resume.Handler(context.Background(), model.ToolRequest{
		Name:      VaultRestoreResumeToolName,
		Arguments: map[string]any{"handle": handle},
	})
	require.NoError(t, err)

	oob.mu.Lock()
	_, stillThere := oob.outcomes[token]
	oob.mu.Unlock()
	assert.False(t, stillThere, "a consumed terminal outcome must be freed from the map")
}
