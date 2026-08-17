//go:build windows

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows/svc"
)

func TestWindowsServiceNameDerivation(t *testing.T) {
	if got := newWindowsService(Config{Name: "pinner-mcp"}).name; got != "pinner-mcp" {
		t.Fatalf("name = %q, want pinner-mcp", got)
	}
	if got := newWindowsService(Config{}).name; got != defaultServiceName {
		t.Fatalf("default name = %q, want %q", got, defaultServiceName)
	}
}

func TestWindowsSystemDetects(t *testing.T) {
	sys := windowsSystem{}
	require.True(t, sys.Detect())
	svc := sys.New(Config{Name: "pinner-mcp"})
	_, ok := svc.(*windowsService)
	require.True(t, ok, "New should return a *windowsService")
}

func TestSCMStateToStatus(t *testing.T) {
	running := Status{Installed: true, Active: true, Ready: true, Summary: "active (running)"}
	inactive := Status{Installed: true, Summary: "inactive"}

	require.Equal(t, running, scmStateToStatus(svc.Running))
	require.Equal(t, running, scmStateToStatus(svc.StartPending))
	require.Equal(t, inactive, scmStateToStatus(svc.Stopped))
	require.Equal(t, inactive, scmStateToStatus(svc.StopPending))
	require.Equal(t, inactive, scmStateToStatus(svc.Paused))
	require.Equal(t, inactive, scmStateToStatus(svc.PausePending))
	require.Equal(t, inactive, scmStateToStatus(svc.ContinuePending))
}

func TestWindowsLogsXPathEscapesSourceName(t *testing.T) {
	// The service name is embedded in the /q: XPath [@Name='...'] literal.
	// A name containing a single quote must be escaped (doubled) so the query
	// stays a valid literal and still matches the real source; other characters
	// pass through verbatim (never mangled).
	cases := []struct {
		name, wantQuery string
	}{
		{"pinner-mcp", `/q:*[System[Provider[@Name='pinner-mcp']]]`},
		{"a]b'c", `/q:*[System[Provider[@Name='a]b''c']]]`},
		{"pin'er", `/q:*[System[Provider[@Name='pin''er']]]`},
	}
	for _, tc := range cases {
		var gotQuery string
		cfg := Config{Name: tc.name}
		cfg.OutputRun = func(_ context.Context, _ string, args ...string) (string, error) {
			for _, a := range args {
				if strings.HasPrefix(a, "/q:") {
					gotQuery = a
				}
			}
			return "", nil
		}
		require.NoError(t, newWindowsService(cfg).Logs(context.Background(), false))
		require.Equal(t, tc.wantQuery, gotQuery, "name %q", tc.name)
	}
}

func TestWindowsSetEnvInRegistryFailsOnCorruptEnvFile(t *testing.T) {
	// SCM has no runtime env-file fallback, so a corrupt env file must make
	// install fail before any registry write, rather than silently installing a
	// service with no credentials.
	dir := t.TempDir()
	envFile := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(envFile, []byte("NOT A KEY VALUE LINE\n"), 0600))
	svc := newWindowsService(Config{Name: "pinner-mcp", EnvFile: envFile})
	err := svc.setEnvInRegistry()
	require.Error(t, err)
	require.Contains(t, err.Error(), "load service environment")
}

func TestWindowsSetEnvInRegistryToleratesMissingEnvFile(t *testing.T) {
	// A fresh install may legitimately have no env file yet; that must not
	// error (nothing is written to the registry).
	svc := newWindowsService(Config{Name: "pinner-mcp", EnvFile: filepath.Join(t.TempDir(), "gone.env")})
	require.NoError(t, svc.setEnvInRegistry())
}

func TestWindowsNewServiceDefaultsCommandSeam(t *testing.T) {
	// newWindowsService must default the OutputRun seam so Logs (the only
	// Windows command path) never invokes a nil function in production.
	svc := newWindowsService(Config{Name: "pinner-mcp"})
	require.NotNil(t, svc.cfg.OutputRun)
	// A caller-provided seam is preserved, not overwritten: exercising it
	// proves it is the same function (function values can't be == compared).
	sentinel := "custom-output"
	want := func(context.Context, string, ...string) (string, error) { return sentinel, nil }
	svc2 := newWindowsService(Config{Name: "pinner-mcp", OutputRun: want})
	out, err := svc2.cfg.OutputRun(context.Background(), "wevtutil", "qe")
	require.NoError(t, err)
	require.Equal(t, sentinel, out)
}

func TestWindowsMergedEnvVars(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(envFile, []byte("SHARED=file-value\nONLY_FILE=fs\n"), 0600))
	svc := newWindowsService(Config{
		Name:    "pinner-mcp",
		EnvVars: map[string]string{"SHARED": "vars-value", "ONLY_VARS": "vs"},
		EnvFile: envFile,
	})
	env, err := svc.mergedEnvVars()
	require.NoError(t, err)
	// Env file wins on collision; both sources contribute.
	require.Equal(t, "file-value", env["SHARED"])
	require.Equal(t, "fs", env["ONLY_FILE"])
	require.Equal(t, "vs", env["ONLY_VARS"])
}

func TestWindowsMergedEnvVarsRejectsCorruptEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "mcp.env")
	require.NoError(t, os.WriteFile(envFile, []byte("NOT A KEY VALUE LINE\n"), 0600))
	svc := newWindowsService(Config{Name: "pinner-mcp", EnvFile: envFile})
	_, err := svc.mergedEnvVars()
	require.ErrorContains(t, err, "load service environment")
}

func TestWindowsLogsFollowBailsOnPersistentFailure(t *testing.T) {
	// A permanently-failing wevtutil poll (missing wevtutil, access denied,
	// nonexistent source) must not spin on the 2s loop forever looking like an
	// idle tail. After maxEventPollFailures consecutive errors, Logs must
	// return the error instead of silent-looping.
	oldInterval, oldMax := eventPollInterval, maxEventPollFailures
	eventPollInterval, maxEventPollFailures = time.Millisecond, 3
	defer func() { eventPollInterval, maxEventPollFailures = oldInterval, oldMax }()

	cfg := Config{Name: "pinner-mcp"}
	polls := 0
	cfg.OutputRun = func(_ context.Context, _ string, _ ...string) (string, error) {
		polls++
		return "", errors.New("missing wevtutil")
	}

	err := newWindowsService(cfg).Logs(context.Background(), true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing wevtutil")
	require.Equal(t, maxEventPollFailures, polls, "must fail fast after the capped consecutive errors")
}

func TestWindowsLogsRoutesThroughOutputRun(t *testing.T) {
	// Logs must invoke wevtutil through the OutputRun seam (so a caller can
	// intercept/test it), with the /q: switch (wevtutil rejects a bare
	// positional XPath), a filter to the service's event source, newest-first,
	// bounded to the recent window.
	var calls [][]string
	cfg := Config{Name: "pinner-mcp"}
	cfg.OutputRun = func(_ context.Context, command string, args ...string) (string, error) {
		calls = append(calls, append([]string{command}, args...))
		return "", nil
	}
	require.NoError(t, newWindowsService(cfg).Logs(context.Background(), false))
	require.Len(t, calls, 1)
	a := calls[0]
	require.Equal(t, "wevtutil", a[0])
	require.Equal(t, "qe", a[1])
	require.Equal(t, "Application", a[2])
	require.Equal(t, `/q:*[System[Provider[@Name='pinner-mcp']]]`, a[3])
	require.Contains(t, a, "/f:text")
	require.Contains(t, a, "/rd:true")
	require.Contains(t, a, "/c:50")
}

func TestPrintNewEventsAdvancesCursorByID(t *testing.T) {
	// Two identical-message events with different record ids must both be
	// printed (dedup is by record id, never by content) and the cursor must
	// advance to the newest id — so a follow poll never re-prints and always
	// moves forward.
	out := `<Event><System><EventRecordID>100</EventRecordID></System><EventData><Data>heartbeat</Data></EventData></Event>` +
		`<Event><System><EventRecordID>101</EventRecordID></System><EventData><Data>heartbeat</Data></EventData></Event>`
	require.Equal(t, 101, printNewEvents(out))
	// Newer id with a multi-data payload advances further.
	out = `<Event xmlns='x'><System><EventRecordID> 300 </EventRecordID></System><EventData><Data>err</Data><Data>detail</Data></EventData></Event>`
	require.Equal(t, 300, printNewEvents(out))
	// Empty output keeps the cursor where it is.
	require.Equal(t, 0, printNewEvents(""))
}
