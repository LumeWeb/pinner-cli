//go:build darwin

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderLaunchdPlist(t *testing.T) {
	cfg := Config{
		Name:        "pinner-mcp",
		Description: "Pinner MCP service",
		ExecPath:    "/usr/local/bin/pinner",
		Arguments:   []string{"mcp", "--http"},
		EnvVars:     map[string]string{"MCP_AUTH_TOKEN": "secret"},
	}
	plist := renderLaunchdPlist(cfg, "/Users/alice/Library/LaunchAgents/pinner-mcp.plist")

	require.Contains(t, plist, "<key>Label</key>")
	require.Contains(t, plist, "<string>pinner-mcp</string>")
	require.Contains(t, plist, "<string>/usr/local/bin/pinner</string>")
	require.Contains(t, plist, "<string>mcp</string>")
	require.Contains(t, plist, "<string>--http</string>")
	require.Contains(t, plist, "<key>EnvironmentVariables</key>")
	require.Contains(t, plist, "<key>MCP_AUTH_TOKEN</key>")
	require.Contains(t, plist, "<string>secret</string>")
	require.Contains(t, plist, "<key>KeepAlive</key>")
	require.Contains(t, plist, "<true/>")
	require.Contains(t, plist, "<key>RunAtLoad</key>")
}

func TestRenderLaunchdPlistEscapesXML(t *testing.T) {
	cfg := Config{
		Name:     "a&b",
		ExecPath: "/tmp/bin<weird>",
	}
	plist := renderLaunchdPlist(cfg, "/x/pinner-mcp.plist")
	require.NotContains(t, plist, "<string>a&b</string>")
	require.Contains(t, plist, "a&amp;b")
	require.Contains(t, plist, "bin&lt;weird&gt;")
}

func TestRenderLaunchdPlistUsesEnvironmentVariables(t *testing.T) {
	// launchd has no native EnvironmentFile and shell-sourcing a KEY=VALUE file
	// is unsafe (a secret containing $, `, or ; could be interpreted/executed),
	// so credentials are delivered via the EnvironmentVariables dict, parsed
	// literally by launchd. There must be no shell wrapper in ProgramArguments.
	cfg := Config{
		Name:      "pinner-mcp",
		ExecPath:  "/usr/local/bin/pinner",
		Arguments: []string{"mcp", "--http"},
		EnvVars:   map[string]string{"MCP_TUNNEL_TOKEN": "$2a$10$super;secret", "PLAIN": "value"},
	}
	plist := renderLaunchdPlist(cfg, "/Users/alice/Library/LaunchAgents/pinner-mcp.plist")
	// Direct exec, no shell.
	require.NotContains(t, plist, "/bin/sh")
	require.NotContains(t, plist, "<string>-c</string>")
	require.Contains(t, plist, "<string>/usr/local/bin/pinner</string>")
	require.Contains(t, plist, "<string>mcp</string>")
	// Values inlined literally (launchd parses them, no shell eval).
	require.Contains(t, plist, "<key>EnvironmentVariables</key>")
	require.Contains(t, plist, "<key>MCP_TUNNEL_TOKEN</key>")
	require.Contains(t, plist, "<string>$2a$10$super;secret</string>")
	require.Contains(t, plist, "<key>PLAIN</key>")
}

func TestLaunchdServiceLifecycleUsesLaunchctl(t *testing.T) {
	var calls [][]string
	runningState := false
	loadCount := 0
	// The OuptRun mock drives Status only; Start/Stop no longer probe launchd
	// state — they just load/unload (the kardianos/service pattern).
	cfg := Config{Name: "pinner-mcp", UserMode: true}
	cfg.Runner = func(_ context.Context, command string, args ...string) error {
		calls = append(calls, append([]string{command}, args...))
		if command == "load" {
			loadCount++
			// A job that is already registered reports "service already
			// loaded"; Start tolerates it as an idempotent no-op.
			if loadCount == 2 {
				return errors.New("LaunchAgent is already loaded")
			}
		}
		return nil
	}
	cfg.OutputRun = func(_ context.Context, command string, args ...string) (string, error) {
		calls = append(calls, append([]string{command}, args...))
		if runningState {
			return "	\"PID\" = 1234;\n", nil
		}
		return "	\"PID\" = -;\n", nil
	}
	cfg.WriteFile = func(string, []byte, os.FileMode) error { return nil }
	cfg.MkdirAll = func(string, os.FileMode) error { return nil }
	cfg.RemoveFile = func(string) error { return nil }

	plist := filepath.Join(homeDir(t), "Library", "LaunchAgents", "pinner-mcp.plist")

	svc := newLaunchdService(cfg)

	// 1. Start -> load.
	require.NoError(t, svc.Start(context.Background()))

	// 2. Start again -> load tolerates "already loaded" (idempotent).
	require.NoError(t, svc.Start(context.Background()))

	// 3. Stop -> unload.
	require.NoError(t, svc.Stop(context.Background()))

	// 4. Restart -> unload then load.
	require.NoError(t, svc.Restart(context.Background()))

	// 5. Status: running -> active.
	runningState = true
	status, err := svc.Status(context.Background())
	require.NoError(t, err)
	require.Equal(t, Status{Installed: true, Active: true, Ready: true, Summary: "active (running)"}, status)

	// 6. Status: not running but plist exists -> inactive.
	runningState = false
	status, err = svc.Status(context.Background())
	require.NoError(t, err)
	require.Equal(t, Status{Installed: true, Summary: "inactive"}, status)

	require.Equal(t, [][]string{
		{"launchctl", "load", plist},        // 1 Start
		{"launchctl", "load", plist},        // 2 Start, already-loaded tolerated
		{"launchctl", "unload", plist},      // 3 Stop
		{"launchctl", "unload", plist},      // 4 Restart -> Stop
		{"launchctl", "load", plist},        // 4 Restart -> Start
		{"launchctl", "list", "pinner-mcp"}, // 5 Status running
		{"launchctl", "list", "pinner-mcp"}, // 6 Status inactive
	}, calls)
}

func TestLaunchdStartRefreshesEnvFileAndLoads(t *testing.T) {
	// Start must re-read the env file into the plist before loading so
	// credential rotations are picked up (systemd re-reads EnvironmentFile=).
	tmp := t.TempDir()
	envFile := filepath.Join(tmp, "mcp.env")
	require.NoError(t, os.WriteFile(envFile, []byte("TOKEN=first\n"), 0600))

	var written []byte
	cfg := Config{Name: "pinner-mcp", UserMode: true, EnvFile: envFile}
	cfg.WriteFile = func(p string, b []byte, m os.FileMode) error { written = b; return nil }
	cfg.MkdirAll = func(string, os.FileMode) error { return nil }
	cfg.RemoveFile = func(string) error { return nil }
	cfg.Runner = func(_ context.Context, command string, args ...string) error {
		// Rotate the env file in the "window" before load so a stale plist
		// would silently carry the old token.
		if command == "load" {
			require.NoError(t, os.WriteFile(envFile, []byte("TOKEN=rotated\n"), 0600))
		}
		return nil
	}
	svc := newLaunchdService(cfg)
	require.NoError(t, svc.Start(context.Background()))

	// The plist rewritten by Start must carry the CURRENT value ("first" —
	// loaded before the rotation in Runner), not a stale snapshot.
	require.Contains(t, string(written), "<string>first</string>")
	require.Contains(t, string(written), "<key>TOKEN</key>")
}

func TestLaunchdServiceInstallAndUninstall(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "LaunchAgents", "pinner-mcp.plist")
	var written []byte
	var modes []os.FileMode
	var calls [][]string
	var mkdirs []string
	cfg := Config{Name: "pinner-mcp", UserMode: true, ServiceFile: plistPath}
	cfg.MkdirAll = func(path string, mode os.FileMode) error {
		require.Equal(t, os.FileMode(0700), mode)
		mkdirs = append(mkdirs, path)
		return nil
	}
	cfg.WriteFile = func(path string, data []byte, mode os.FileMode) error {
		require.Equal(t, plistPath, path)
		written = data
		modes = append(modes, mode)
		return nil
	}
	cfg.RemoveFile = func(path string) error {
		require.Equal(t, plistPath, path)
		return nil
	}
	cfg.Runner = func(_ context.Context, command string, args ...string) error {
		calls = append(calls, append([]string{command}, args...))
		return nil
	}

	svc := newLaunchdService(cfg)
	require.NoError(t, svc.Install(context.Background()))
	require.NotEmpty(t, written)
	require.Equal(t, []os.FileMode{0600}, modes)
	// Install must ensure both the LaunchAgents dir and the co-located Logs
	// dir (where StandardOutPath/StandardErrorPath point) exist.
	require.Equal(t, []string{filepath.Dir(plistPath), filepath.Dir(logPathFor(plistPath, "out"))}, mkdirs)
	require.NoError(t, svc.Uninstall(context.Background()))
	require.Equal(t, [][]string{
		{"launchctl", "load", plistPath},
		{"launchctl", "unload", plistPath},
	}, calls)
}

func TestLaunchdServiceStatusNotInstalled(t *testing.T) {
	cfg := Config{Name: "pinner-mcp"}
	cfg.OutputRun = func(context.Context, string, ...string) (string, error) {
		// A recognized absent-job message, as launchctl actually emits.
		return "", errors.New("Could not find specified service")
	}
	cfg.RemoveFile = func(string) error { return nil }
	svc := newLaunchdService(cfg)
	status, err := svc.Status(context.Background())
	require.NoError(t, err)
	require.Equal(t, Status{Summary: "not installed"}, status)
}

func TestLaunchdServiceStatusStoppedWhenPlistExists(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "pinner-mcp.plist")
	require.NoError(t, os.WriteFile(plistPath, []byte("x"), 0600))
	cfg := Config{Name: "pinner-mcp", ServiceFile: plistPath}
	cfg.OutputRun = func(context.Context, string, ...string) (string, error) {
		// A recognized absent-job message, as launchctl actually emits.
		return "", errors.New("launchctl failed: Could not find specified service")
	}
	svc := newLaunchdService(cfg)
	status, err := svc.Status(context.Background())
	require.NoError(t, err)
	require.Equal(t, Status{Installed: true, Summary: "inactive"}, status)
}

func TestLaunchdServiceStatusPropagatesBackendError(t *testing.T) {
	// A genuine launchctl backend failure (e.g. launchctl missing, broken
	// session, permission denied) must be reported, not misread as "not
	// installed" — mirroring the systemd Status guard. It must also not cause
	// Start to attempt a stale load.
	backendErr := errors.New("launchctl: could not connect to launchd - boostrap failed: 5: Input/output error")
	cfg := Config{Name: "pinner-mcp"}
	cfg.OutputRun = func(context.Context, string, ...string) (string, error) {
		return "launchctl: Could not connect to launchd\n", backendErr
	}
	svc := newLaunchdService(cfg)

	_, err := svc.Status(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "query launchctl list")
	require.ErrorIs(t, err, backendErr)
}

func TestLaunchdServiceStartPropagatesBackendError(t *testing.T) {
	// A genuine launchctl failure during `launchctl load` (e.g. "Could not
	// connect to launchd") must surface, not be misread as an already-loaded
	// no-op.
	backendErr := errors.New("launchctl: Could not connect to launchd")
	cfg := Config{Name: "pinner-mcp"}
	cfg.WriteFile = func(string, []byte, os.FileMode) error { return nil }
	cfg.MkdirAll = func(string, os.FileMode) error { return nil }
	cfg.RemoveFile = func(string) error { return nil }
	cfg.Runner = func(context.Context, string, ...string) error {
		return backendErr
	}
	svc := newLaunchdService(cfg)
	err := svc.Start(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "load LaunchAgent")
	require.ErrorIs(t, err, backendErr)
}

func homeDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	return home
}
