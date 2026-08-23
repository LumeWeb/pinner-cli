package tunnel

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/v2"
)

func TestSplitHostPort(t *testing.T) {
	cases := []struct {
		in    string
		host  string
		port  string
		valid bool
	}{
		{"8080", "127.0.0.1", "8080", true},
		{"127.0.0.1:8893", "127.0.0.1", "8893", true},
		{"[::1]:8893", "::1", "8893", true},
		{"", "", "", false},
		{"host:notaport", "", "", false},
	}
	for _, c := range cases {
		host, port, err := SplitHostPort(c.in)
		if !c.valid {
			assert.Error(t, err, "expected error for %q", c.in)
			continue
		}
		require.NoError(t, err, "split %q", c.in)
		assert.Equal(t, c.host, host)
		assert.Equal(t, c.port, port)
	}
}

func TestNgrokLocalURL(t *testing.T) {
	assert.Equal(t, "http://127.0.0.1:8893", LocalURL("127.0.0.1", "8893"))
	assert.Equal(t, "http://localhost:7000", LocalURL("localhost", "7000"))
	assert.Equal(t, "http://[::1]:8080", LocalURL("::1", "8080"))
}

func TestRequiresToken(t *testing.T) {
	// Explicit --token supplied.
	require.False(t, NewNgrokTunnel("", "tok").RequiresToken())

	// NGROK_AUTHTOKEN env set: token source present.
	t.Setenv("NGROK_AUTHTOKEN", "sekret")
	require.False(t, NewNgrokTunnel("", "").RequiresToken())

	// NGROK_CONFIG pointing at an existing config file counts as auth.
	dir := t.TempDir()
	cfg := filepath.Join(dir, "ngrok.yml")
	require.NoError(t, os.WriteFile(cfg, []byte("agent:\n  authtoken: x\n"), 0o600))
	t.Setenv("NGROK_AUTHTOKEN", "")
	t.Setenv("NGROK_CONFIG", cfg)
	require.False(t, NewNgrokTunnel("", "").RequiresToken())

	// No token, no env, no config file: token required.
	t.Setenv("NGROK_CONFIG", filepath.Join(dir, "missing.yml"))
	require.True(t, NewNgrokTunnel("", "").RequiresToken())

	// A config file that exists but declares no usable agent authtoken (empty
	// or broken; authtoken nested under tunnels/endpoints) must NOT satisfy the
	// token requirement, or the embedded agent would start unauthenticated.
	emptyCfg := filepath.Join(dir, "empty.yml")
	require.NoError(t, os.WriteFile(emptyCfg, []byte("version: 2\nagent:\n"), 0o600))
	t.Setenv("NGROK_CONFIG", emptyCfg)
	require.True(t, NewNgrokTunnel("", "").RequiresToken(), "config file with no agent authtoken must still require a token")

	nestedCfg := filepath.Join(dir, "nested.yml")
	require.NoError(t, os.WriteFile(nestedCfg, []byte(
		"version: 2\nlog:\n  level: debug\ntunnels:\n  test:\n    authtoken: not-an-agent-token\n"), 0o600))
	t.Setenv("NGROK_CONFIG", nestedCfg)
	require.True(t, NewNgrokTunnel("", "").RequiresToken(), "authtoken nested under non-agent block must not count")

	// A token persisted to the pinner config-manager store satisfies the
	// token requirement (no re-prompt / no rejection).
	t.Setenv("NGROK_CONFIG", filepath.Join(dir, "missing.yml"))
	mgr := newTestConfigManager(t, "storedtok")
	require.False(t, NewNgrokTunnelWithConfig("", "", mgr).RequiresToken(), "config-manager stored token satisfies the requirement")
}

func TestRequiresTokenDefaultConfigPath(t *testing.T) {
	// Exercise the default config-file branch (no NGROK_CONFIG override) by
	// pointing the OS config/home dir at a temp dir. The path assembled below
	// must match the per-OS default RequiresToken probes.
	t.Setenv("NGROK_CONFIG", "")
	t.Setenv("NGROK_AUTHTOKEN", "")

	var base, cfg string
	if runtime.GOOS == "windows" {
		base = t.TempDir()
		t.Setenv("LOCALAPPDATA", base)
		cfg = filepath.Join(base, "ngrok", "ngrok.yml")
	} else {
		base = t.TempDir()
		t.Setenv("HOME", base)
		if runtime.GOOS == "darwin" {
			cfg = filepath.Join(base, "Library", "Application Support", "ngrok", "ngrok.yml")
		} else {
			cfg = filepath.Join(base, ".config", "ngrok", "ngrok.yml")
		}
	}

	// No config file present yet: token required.
	require.True(t, NewNgrokTunnel("", "").RequiresToken())

	// Write the config file at the default location: token no longer required.
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("agent:\n  authtoken: x\n"), 0o600))
	require.False(t, NewNgrokTunnel("", "").RequiresToken())
}

// TestMissingTokenError locks in the provider-specific setup/token errors that
// serveHTTP surfaces when RequiresToken() is true. It also guards the contract
// that these are plain error returns: no provider path may open a browser or
// emit onboarding guidance from the server runtime (that is the installer's
// job). The msg is checked via ErrorContains so both concrete tunnels return
// their own actionable guidance without per-provider branching in the caller.
func TestMissingTokenError(t *testing.T) {
	ng := NewNgrokTunnel("", "")
	require.True(t, ng.RequiresToken(), "bare ngrok tunnel requires a token")
	require.ErrorContains(t, ng.MissingTokenError(), "ngrok tunnel requires an account token")

	// A cloudflared tunnel with no provisioned state file requires its
	// tunnel to be provisioned, not a token.
	cf := &CloudflaredTunnel{statePath: filepath.Join(t.TempDir(), "missing.json"), name: "t"}
	require.True(t, cf.RequiresToken(), "unprovisioned cloudflared tunnel requires setup")
	require.ErrorContains(t, cf.MissingTokenError(), "cloudflared tunnel is not provisioned")
	require.ErrorContains(t, cf.MissingTokenError(), "pinner mcp tunnel install")
}

func TestURLForOrigin(t *testing.T) {
	u, err := UrlForOrigin("127.0.0.1:8893")
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:8893", u)

	_, err = UrlForOrigin("notaport")
	require.Error(t, err)
}

// TestNgrokCustomDomainNormalization guards the https:// stripping applied to
// ngrok custom domains before ngrok.WithURL (BareHostname). A scheme-qualified
// domain must become a bare hostname or the SDK rejects it as malformed.
func TestNgrokCustomDomainNormalization(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"mcp.example.com", "mcp.example.com"},
		{"https://mcp.example.com", "mcp.example.com"},
		{"http://mcp.example.com", "mcp.example.com"},
		{"https://mcp.example.com/", "mcp.example.com"},
		{"  https://mcp.example.com  ", "mcp.example.com"},
	}
	for _, tc := range tests {
		got := BareHostname(tc.in)
		assert.Equal(t, tc.want, got, "BareHostname(%q)", tc.in)
	}
}

// blockingDialer is a ngrok.Dialer that blocks until its context is done,
// standing in for a ngrok control plane that is reachable at the TCP/TLS
// level but never answers (a genuinely stuck connect). It returns only when
// the bounded connect context aborts, so it exercises the timeout path the
// fix is guarding rather than an immediate dial error.
type blockingDialer struct{}

func (blockingDialer) Dial(string, string) (net.Conn, error) {
	return nil, errors.New("control plane unreachable")
}

func (blockingDialer) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	// Block until the bounded connect context aborts: the only way out is the
	// ctx.Done() branch, which is precisely what must hold for the fix.
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestNgrokStartBoundedConnect guards the bounded-connect fix in Start.
// ngrok's reconnecting session retries the control-plane connection forever
// with no deadline (and ignores cancellation internally), which used to hang
// the server silently when the control plane was unreachable. Start now
// establishes the session with a bounded context, so an unreachable control
// plane must fail fast with an error rather than hang.
//
// A blocking dialer simulates a connect that never completes until the bound's
// context aborts, and a shortened ngrokConnectTimeout keeps the test quick. The
// elapsed-time assertion proves the connect actually waited out the bound
// (300ms window) instead of failing immediately, which is the behavior the fix
// adds. Without the fix, Forward's unbounded reconnect would never abort and
// the test would time out.
func TestNgrokStartBoundedConnect(t *testing.T) {
	oldTimeout := ngrokConnectTimeout
	ngrokConnectTimeout = 300 * time.Millisecond
	t.Cleanup(func() { ngrokConnectTimeout = oldTimeout })

	ng := &ngrokTunnel{
		token:  "test-token",
		dialer: blockingDialer{},
	}

	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- ng.Start(context.Background(), "127.0.0.1:8893") }()

	select {
	case err := <-done:
		require.Error(t, err, "Start must fail when the ngrok control plane never answers")
		require.Contains(t, err.Error(), "connect to ngrok", "error should identify the connect step")
		// It must have waited out the bounded connect window rather than
		// failing immediately. Use ~2/3 of the window as a lower bound so the
		// assertion is robust to scheduling jitter.
		require.GreaterOrEqual(t, time.Since(start), ngrokConnectTimeout*2/3,
			"Start returned too quickly: the bounded connect window was not honored")
	case <-time.After(10 * time.Second):
		// A failure to return here means Start blocked forever in the ngrok
		// reconnecting session instead of honoring the bounded connect.
		t.Fatal("Start hung: bounded ngrok connect did not abort on a stuck control plane")
	}
}

// TestNgrokCheckAccountFailsFast guards the pre-flight login check added for
// the tunnel runtime. Before starting the tunnel, serveHTTP calls
// CheckAccount so an invalid/unreachable ngrok account fails fast with a clear
// error instead of hanging inside Start (ngrok's session retries a bad
// authtoken until its connect deadline). A blocking dialer stands in for a
// control plane that never answers, and a shortened ngrokLoginProbeTimeout
// keeps the test quick; the probe (not the larger connect window) must be the
// bound that aborts.
func TestNgrokCheckAccountFailsFast(t *testing.T) {
	oldProbe := ngrokLoginProbeTimeout
	oldConnect := ngrokConnectTimeout
	ngrokLoginProbeTimeout = 300 * time.Millisecond
	ngrokConnectTimeout = 30 * time.Second // probe, not connect, must be the bound
	t.Cleanup(func() {
		ngrokLoginProbeTimeout = oldProbe
		ngrokConnectTimeout = oldConnect
	})

	ng := &ngrokTunnel{
		token:  "test-token",
		dialer: blockingDialer{},
	}

	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- ng.CheckAccount(context.Background()) }()

	select {
	case err := <-done:
		require.Error(t, err, "CheckAccount must fail when the ngrok control plane never answers")
		require.Contains(t, err.Error(), "ngrok login check", "error should identify the login-check step")
		// It must have waited out the probe window rather than failing
		// immediately, and must not reach the much larger connect window.
		require.GreaterOrEqual(t, time.Since(start), ngrokLoginProbeTimeout*2/3,
			"CheckAccount returned too quickly: the login probe window was not honored")
		require.Less(t, time.Since(start), 5*time.Second,
			"CheckAccount took too long: the login probe must fail fast, not wait the connect window")
	case <-time.After(5 * time.Second):
		t.Fatal("CheckAccount hung: login probe did not abort on a stuck control plane")
	}
}

// TestCloudflaredStopAfterExit guards the exit-detection path of the embedded
// tunnel: once the in-process daemon has shut down (done closed), waitReady
// must observe the exit rather than spinning to its deadline, and a subsequent
// Stop must return promptly instead of blocking.
func TestCloudflaredStopAfterExit(t *testing.T) {
	done := make(chan struct{})
	close(done) // daemon already exited

	c := &CloudflaredTunnel{done: done}

	// waitReady must fail fast with the exit error, not time out.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := c.waitReady(ctx, "https://exited.invalid")
	assert.ErrorContains(t, err, "exited before the tunnel became ready")

	// Stop must return promptly instead of blocking on the closed channel.
	started := time.Now()
	assert.NoError(t, c.Stop(ctx))
	assert.Less(t, time.Since(started), 3*time.Second, "Stop blocked after process exit")
}

// TestCloudflaredStopBeforeStart guards the not-started path: Stop on a tunnel
// whose daemon was never launched must be a no-op rather than a panic or hang.
func TestCloudflaredStopBeforeStart(t *testing.T) {
	c := &CloudflaredTunnel{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	assert.NoError(t, c.Stop(ctx))
}

// TestCloudflaredStartMissingState guards the provisioning gate: Start without
// a provisioned tunnel state (beyond a --domain) must report a clear error
// rather than attempt to build a tunnel from empty credentials.
func TestCloudflaredStartMissingState(t *testing.T) {
	c := &CloudflaredTunnel{domain: "mcp.example.com"}
	err := c.Start(context.Background(), "127.0.0.1:8893")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no provisioned cloudflare tunnel found")
}

// fakeNgrokForwarder is a minimal ngrok.EndpointForwarder for tests. It embeds
// the interface so every method is satisfied with zero boilerplate, overriding
// only URL (used by Start/setReady) and Done (used by waitReady). Calling any
// other method would panic, but the production code path exercised by these
// tests only touches URL and Done.
type fakeNgrokForwarder struct {
	ngrok.EndpointForwarder
	u    *url.URL
	done chan struct{}
}

func (f *fakeNgrokForwarder) URL() *url.URL { return f.u }

func (f *fakeNgrokForwarder) Done() <-chan struct{} {
	if f.done == nil {
		f.done = make(chan struct{})
	}
	return f.done
}

// fakeNgrokAgent is a minimal ngrok.Agent for tests, embedding the interface to
// avoid implementing the full surface. It records the connect context handed to
// it and returns configurable errors/forwarders from Connect and Forward —
// the only Agent methods the tunnel code calls.
type fakeNgrokAgent struct {
	ngrok.Agent
	connectErr error
	forwardErr error
	fwd        ngrok.EndpointForwarder
	connectCtx context.Context
}

func (f *fakeNgrokAgent) Connect(ctx context.Context) error {
	f.connectCtx = ctx
	return f.connectErr
}

func (f *fakeNgrokAgent) Forward(_ context.Context, _ *ngrok.Upstream, _ ...ngrok.EndpointOption) (ngrok.EndpointForwarder, error) {
	// Model the real SDK: an agent binds its session to the context passed to
	// Connect, so once that context is cancelled the session is closed. Forward
	// then fails with "session closed" even though the agent still believes it
	// is connected. This is what let the regression tests below catch premature
	// cancellation of the connect context.
	if f.connectCtx != nil && f.connectCtx.Err() != nil {
		return nil, errors.New("session closed")
	}
	if f.forwardErr != nil {
		return nil, f.forwardErr
	}
	return f.fwd, nil
}

// TestNgrokCheckAccountDoesNotPoisonStartSession is the regression test for
// the "session closed" failure. An ngrok agent's session is bound to the
// context passed to Connect: cancelling that context closes the session. The
// login probe previously ran through connectedAgent, which cached the probe's
// agent; when the short-lived probe context was then cancelled the cached
// session was torn down, so Start reused a dead agent and agent.Forward failed
// with "start ngrok tunnel: failed to start tunnel: session closed".
//
// The fix makes CheckAccount probe with its own throwaway agent (never cached)
// while Start always establishes a fresh, durable session. A factory-injected
// fake agent lets this test assert the exact contract that prevents the bug:
// CheckAccount builds exactly one probe agent, Start builds a second, distinct
// agent to forward through, and never reuses the probe's.
func TestNgrokCheckAccountDoesNotPoisonStartSession(t *testing.T) {
	var agents []*fakeNgrokAgent
	ng := &ngrokTunnel{
		domain: "mcp.example.com",
		token:  "test-token",
		agentFactory: func(...ngrok.AgentOption) (ngrok.Agent, error) {
			a := &fakeNgrokAgent{
				fwd: &fakeNgrokForwarder{u: &url.URL{Scheme: "https", Host: "mcp.example.com"}},
			}
			agents = append(agents, a)
			return a, nil
		},
	}

	// A successful login probe must fail fast with no error and, crucially,
	// must not cache the probed agent for later reuse.
	require.NoError(t, ng.CheckAccount(context.Background()))
	require.Len(t, agents, 1, "CheckAccount must build exactly one throwaway probe agent")
	probe := agents[0]

	// Start must establish its own fresh session and still succeed after the
	// probe. A custom domain is used so Start takes the pre-assigned-URL
	// branch and does not probe public reachability.
	err := ng.Start(context.Background(), "127.0.0.1:8893")
	require.NoError(t, err, "Start must succeed with its own session after a login probe")

	require.Len(t, agents, 2, "Start must build a fresh agent rather than reuse the probe's")
	startAgent := agents[1]
	require.NotSame(t, probe, startAgent, "Start must not reuse the CheckAccount probe agent")

	// The probe's connect ran under a short probe-bound context (its own
	// throwaway session), while Start's connect ran under the caller context —
	// confirming the two sessions are independent.
	require.NotNil(t, probe.connectCtx, "probe agent must have received a connect context")
	require.NotNil(t, startAgent.connectCtx, "start agent must have received a connect context")
	require.NotEqual(t, probe.connectCtx, startAgent.connectCtx, "probe and start sessions must use independent contexts")
}

// TestNgrokStartKeepsSessionAliveAfterConnect is the regression test for the
// "start ngrok tunnel: failed to start tunnel: session closed" failure. The
// previous bounded-connect code used context.WithTimeout and cancelled the
// connect context immediately after a successful agent.Connect. Because the
// ngrok SDK binds the session to the Connect context, that cancel closed the
// session; a subsequent Forward then failed with "session closed" despite the
// agent still reporting itself connected.
//
// This test drives Start directly with a fake agent that models the SDK's
// session-close-on-connect-cancel behavior (see fakeNgrokAgent.Forward). With
// the fix, the connect deadline is released on success and never cancels the
// live session, so Forward succeeds and Start returns nil. A custom domain is
// used so Start takes the pre-assigned-URL branch and does not probe public
// reachability.
func TestNgrokStartKeepsSessionAliveAfterConnect(t *testing.T) {
	ng := &ngrokTunnel{
		domain: "mcp.example.com",
		token:  "test-token",
		agentFactory: func(...ngrok.AgentOption) (ngrok.Agent, error) {
			return &fakeNgrokAgent{
				fwd: &fakeNgrokForwarder{u: &url.URL{Scheme: "https", Host: "mcp.example.com"}},
			}, nil
		},
	}

	err := ng.Start(context.Background(), "127.0.0.1:8893")
	require.NoError(t, err, "Start must not tear down the session after a successful connect")

	// Sanity: the session context was never cancelled, so the forwarder is usable.
	var a *fakeNgrokAgent
	a = ng.agent.(*fakeNgrokAgent)
	require.NoError(t, a.connectCtx.Err(), "the live session context must stay uncancelled")
}
