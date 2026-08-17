//go:build linux

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderSystemdUnit(t *testing.T) {
	unit := renderSystemdUnit(Config{
		Name:        "pinner-mcp",
		Description: "Pinner MCP service",
		ExecPath:    "/opt/bin/pinner",
		Arguments:   []string{"mcp", "--tunnel", "openai", "--tunnel-id", "tunnel_abc"},
		EnvFile:     "/home/alice/.config/pinner/mcp.env",
	})

	require.Contains(t, unit, "Description=\"Pinner MCP service\"")
	require.Contains(t, unit, "After=network-online.target")
	require.Contains(t, unit, "ExecStart=/opt/bin/pinner mcp --tunnel openai --tunnel-id tunnel_abc")
	require.Contains(t, unit, "EnvironmentFile=/home/alice/.config/pinner/mcp.env")
	require.Contains(t, unit, "Restart=on-failure")
	require.Contains(t, unit, "NoNewPrivileges=true")
	require.Contains(t, unit, "WantedBy=default.target")
}

func TestRenderSystemdUnitEmitsEnvVars(t *testing.T) {
	// Config.EnvVars is part of the documented contract and must be emitted as
	// Environment= lines even when no EnvFile is set (callers that pass
	// variables directly must not silently lose them).
	unit := renderSystemdUnit(Config{
		Name:      "pinner-mcp",
		ExecPath:  "/opt/bin/pinner",
		Arguments: []string{"mcp"},
		EnvVars:   map[string]string{"MCP_AUTH_TOKEN": "tok-123", "VAR": "a b"},
	})
	require.Contains(t, unit, "Environment=MCP_AUTH_TOKEN=tok-123")
	require.Contains(t, unit, `Environment=VAR="a b"`)
}

func TestSystemdUnitKeepsSecretsOutOfExecStart(t *testing.T) {
	// Secrets are delivered via EnvironmentFile=, never as ExecStart arguments.
	unit := renderSystemdUnit(Config{
		ExecPath:  "/usr/local/bin/pinner",
		Arguments: []string{"mcp", "--tunnel", "openai"},
		EnvFile:   "/home/user/.config/pinner/mcp.env",
	})
	require.Contains(t, unit, "EnvironmentFile=/home/user/.config/pinner/mcp.env")
	require.NotContains(t, unit, "CONTROL_PLANE_API_KEY")
	require.NotContains(t, unit, "OPENAI_API_KEY")
	require.NotContains(t, unit, "MCP_AUTH_TOKEN")
}

func TestSystemdServiceLifecycleUsesArgumentArrays(t *testing.T) {
	var calls [][]string
	cfg := Config{Name: "pinner-mcp", UserMode: true}
	cfg.Runner = func(_ context.Context, command string, args ...string) error {
		calls = append(calls, append([]string{command}, args...))
		return nil
	}
	cfg.OutputRun = func(_ context.Context, command string, args ...string) (string, error) {
		calls = append(calls, append([]string{command}, args...))
		return "active", nil
	}
	cfg.WriteFile = func(string, []byte, os.FileMode) error { return nil }
	cfg.MkdirAll = func(string, os.FileMode) error { return nil }
	cfg.RemoveFile = func(string) error { return nil }

	svc := newSystemdService(cfg)
	require.NoError(t, svc.Start(context.Background()))
	require.NoError(t, svc.Stop(context.Background()))
	require.NoError(t, svc.Restart(context.Background()))
	status, err := svc.Status(context.Background())
	require.NoError(t, err)
	require.Equal(t, Status{Installed: true, Active: true, Ready: true, Summary: "active (running)"}, status)
	require.Equal(t, [][]string{
		{"systemctl", "--user", "start", "pinner-mcp.service"},
		{"systemctl", "--user", "stop", "pinner-mcp.service"},
		{"systemctl", "--user", "restart", "pinner-mcp.service"},
		{"systemctl", "--user", "is-active", "pinner-mcp.service"},
	}, calls)
}

func TestSystemdServiceInstallAndUninstall(t *testing.T) {
	tmp := t.TempDir()
	unitPath := filepath.Join(tmp, "systemd", "user", "pinner-mcp.service")
	var written []byte
	var modes []os.FileMode
	var calls [][]string
	cfg := Config{Name: "pinner-mcp", UserMode: true, ServiceFile: unitPath}
	cfg.MkdirAll = func(path string, mode os.FileMode) error {
		require.Equal(t, filepath.Dir(unitPath), path)
		require.Equal(t, os.FileMode(0700), mode)
		return nil
	}
	cfg.WriteFile = func(path string, data []byte, mode os.FileMode) error {
		require.Equal(t, unitPath, path)
		written = data
		modes = append(modes, mode)
		return nil
	}
	cfg.RemoveFile = func(path string) error {
		require.Equal(t, unitPath, path)
		return nil
	}
	cfg.Runner = func(_ context.Context, command string, args ...string) error {
		calls = append(calls, append([]string{command}, args...))
		return nil
	}

	svc := newSystemdService(cfg)
	require.NoError(t, svc.Install(context.Background()))
	require.NotEmpty(t, written)
	require.Equal(t, []os.FileMode{0600}, modes)
	require.NoError(t, svc.Uninstall(context.Background()))
	require.Equal(t, [][]string{
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", "pinner-mcp.service"},
		{"systemctl", "--user", "disable", "--now", "pinner-mcp.service"},
		{"systemctl", "--user", "daemon-reload"},
	}, calls)
}

func TestSystemdServiceRejectsSystemMode(t *testing.T) {
	svc := newSystemdService(Config{ServiceFile: "/tmp/unit"})
	require.ErrorContains(t, svc.Install(context.Background()), "only supports user-mode")
}

func TestSystemdServiceStatusNotInstalled(t *testing.T) {
	// A missing unit: `is-active` prints "inactive" and exits non-zero (exit
	// 3), and `list-unit-files --output=json` returns "[]" -> not installed.
	var subcmds []string
	cfg := Config{Name: "pinner-mcp"}
	cfg.OutputRun = func(_ context.Context, _ string, args ...string) (string, error) {
		subcmds = append(subcmds, args[1])
		if slices.Contains(args, "is-active") {
			return "inactive", errors.New("exit status 3")
		}
		return "[]", nil
	}
	svc := newSystemdService(cfg)
	status, err := svc.Status(context.Background())
	require.NoError(t, err)
	require.Equal(t, Status{Summary: "not installed"}, status)
	require.Equal(t, []string{"is-active", "list-unit-files"}, subcmds)
}

func TestSystemdServiceStatusStoppedWhenUnitFileExists(t *testing.T) {
	// Installed but stopped: `is-active` prints "inactive" and exits non-zero,
	// and `list-unit-files --output=json` lists the unit file -> installed, not
	// running.
	cfg := Config{Name: "pinner-mcp"}
	cfg.OutputRun = func(_ context.Context, _ string, args ...string) (string, error) {
		if slices.Contains(args, "is-active") {
			return "inactive", errors.New("exit status 3")
		}
		return `[{"unit_file":"pinner-mcp.service","state":"static"}]`, nil
	}
	svc := newSystemdService(cfg)
	status, err := svc.Status(context.Background())
	require.NoError(t, err)
	require.Equal(t, Status{Installed: true, Summary: "inactive"}, status)
}

func TestSystemdServiceStatusPropagatesBackendError(t *testing.T) {
	// Regression: a genuine backend failure must propagate as an error, NOT be
	// mistaken for "not installed" or masked as an installed service with a
	// garbage summary.
	tests := []struct {
		name   string
		output string
		err    error
	}{
		{name: "empty output", output: "", err: errors.New("Failed to connect to bus")},
		// CombinedOutput coalesces stderr, so a D-Bus failure arrives as
		// non-empty output + error. The business-garbage token must not be
		// treated as a state.
		{name: "stderr coalesced", output: "Failed to connect to bus: no such file or directory", err: errors.New("exit status 4")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Name: "pinner-mcp"}
			cfg.OutputRun = func(context.Context, string, ...string) (string, error) {
				return tt.output, tt.err
			}
			svc := newSystemdService(cfg)
			_, err := svc.Status(context.Background())
			require.Error(t, err)
			require.Contains(t, err.Error(), "query systemd user service")
		})
	}
}

func TestSystemdServiceStatusRunning(t *testing.T) {
	cfg := Config{Name: "pinner-mcp"}
	cfg.OutputRun = func(_ context.Context, _ string, args ...string) (string, error) {
		require.True(t, slices.Contains(args, "is-active"))
		// Active: exit 0 means OutputRun returns nil error.
		return "active", nil
	}
	svc := newSystemdService(cfg)
	status, err := svc.Status(context.Background())
	require.NoError(t, err)
	require.Equal(t, Status{Installed: true, Active: true, Ready: true, Summary: "active (running)"}, status)
}

func TestSystemdServiceStatusFailed(t *testing.T) {
	cfg := Config{Name: "pinner-mcp"}
	cfg.OutputRun = func(_ context.Context, _ string, args ...string) (string, error) {
		if slices.Contains(args, "is-active") {
			// Real systemd prints "failed" and exits non-zero (exit 3); the
			// state token must be honored, not masked as a backend error.
			return "failed", errors.New("exit status 3")
		}
		return `[{"unit_file":"pinner-mcp.service","state":"static"}]`, nil
	}
	svc := newSystemdService(cfg)
	status, err := svc.Status(context.Background())
	require.NoError(t, err)
	require.Equal(t, Status{Installed: true, Summary: "failed"}, status)
}

func TestSystemdServiceStatusTransitionalStates(t *testing.T) {
	// Transitional states stay truthful: activating/reloading are in use but
	// not yet ready; deactivating is heading toward stopped — not reported as
	// fully running or as a dropped service.
	tests := []struct {
		state  string
		status Status
	}{
		{"activating", Status{Installed: true, Active: true, Summary: "activating"}},
		{"reloading", Status{Installed: true, Active: true, Summary: "activating"}},
		{"deactivating", Status{Installed: true, Summary: "deactivating"}},
	}
	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			cfg := Config{Name: "pinner-mcp"}
			cfg.OutputRun = func(_ context.Context, _ string, args ...string) (string, error) {
				if slices.Contains(args, "is-active") {
					return tt.state, errors.New("exit status 3")
				}
				return `[{"unit_file":"pinner-mcp.service","state":"static"}]`, nil
			}
			svc := newSystemdService(cfg)
			got, err := svc.Status(context.Background())
			require.NoError(t, err)
			require.Equal(t, tt.status, got)
		})
	}
}

func TestSystemdServiceStatusNonRunningNotInstalled(t *testing.T) {
	// A state reporting a unit that has no unit file (loaded-but-removed,
	// stale activator) must be reported as not installed even when the active
	// state is failed/transitional.
	cfg := Config{Name: "pinner-mcp"}
	cfg.OutputRun = func(_ context.Context, _ string, args ...string) (string, error) {
		if slices.Contains(args, "is-active") {
			return "inactive", errors.New("exit status 3")
		}
		return "[]", nil
	}
	svc := newSystemdService(cfg)
	status, err := svc.Status(context.Background())
	require.NoError(t, err)
	require.Equal(t, Status{Summary: "not installed"}, status)
}

func TestParseSystemdActiveState(t *testing.T) {
	require.Equal(t, systemdActive, parseSystemdActiveState("active"))
	require.Equal(t, systemdInactive, parseSystemdActiveState("inactive"))
	require.Equal(t, systemdFailed, parseSystemdActiveState("failed\n"))
	// Coalesced stderr must not poison the state token; the first token wins.
	require.Equal(t, systemdInactive, parseSystemdActiveState("inactive\nFailed to get properties: No such file or directory"))
	// A genuine bus error with no state token is unknown.
	require.Equal(t, systemdStateUnknown, parseSystemdActiveState("Failed to connect to bus: no such file or directory"))
	require.Equal(t, systemdStateUnknown, parseSystemdActiveState(""))
}

func TestUnitFileListContains(t *testing.T) {
	require.False(t, unitFileListContains(`[]`, "pinner-mcp.service"))
	require.True(t, unitFileListContains(`[{"unit_file":"pinner-mcp.service","state":"static"}]`, "pinner-mcp.service"))
	require.False(t, unitFileListContains(`[{"unit_file":"other.service","state":"static"}]`, "pinner-mcp.service"))
	// Unknown JSON shape falls back to substring detection.
	require.True(t, unitFileListContains(`pinner-mcp.service`, "pinner-mcp.service"))
}
