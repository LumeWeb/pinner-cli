package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderSystemdUserUnit(t *testing.T) {
	unit := renderSystemdUserUnit(ServiceSpec{
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

func TestSystemdUserServiceLifecycleUsesArgumentArrays(t *testing.T) {
	var calls [][]string
	svc := NewSystemdUserService(ServiceSpec{Name: "pinner-mcp.service", UnitPath: "/tmp/pinner-mcp.service", UserMode: true})
	svc.runner = func(_ context.Context, command string, args ...string) error {
		calls = append(calls, append([]string{command}, args...))
		return nil
	}
	svc.runOutput = func(_ context.Context, command string, args ...string) (string, error) {
		calls = append(calls, append([]string{command}, args...))
		return "LoadState=loaded\nActiveState=active\nSubState=running\n", nil
	}
	svc.writeFile = func(string, []byte, os.FileMode) error { return nil }
	svc.mkdirAll = func(string, os.FileMode) error { return nil }
	svc.removeFile = func(string) error { return nil }

	require.NoError(t, svc.Start(context.Background()))
	require.NoError(t, svc.Stop(context.Background()))
	require.NoError(t, svc.Restart(context.Background()))
	status, err := svc.Status(context.Background())
	require.NoError(t, err)
	require.Equal(t, ServiceStatus{Installed: true, Active: true, Ready: true, Summary: "active (running)"}, status)
	require.Equal(t, [][]string{
		{"systemctl", "--user", "start", "pinner-mcp.service"},
		{"systemctl", "--user", "stop", "pinner-mcp.service"},
		{"systemctl", "--user", "restart", "pinner-mcp.service"},
		{"systemctl", "--user", "show", "pinner-mcp.service", "--property=LoadState,ActiveState,SubState", "--no-pager"},
	}, calls)
}

func TestSystemdUserServiceInstallAndUninstall(t *testing.T) {
	tmp := t.TempDir()
	unitPath := filepath.Join(tmp, "systemd", "user", "pinner-mcp.service")
	var written []byte
	var modes []os.FileMode
	var calls [][]string
	svc := NewSystemdUserService(ServiceSpec{
		Name:     "pinner-mcp.service",
		UnitPath: unitPath,
		UserMode: true,
	})
	svc.mkdirAll = func(path string, mode os.FileMode) error {
		require.Equal(t, filepath.Dir(unitPath), path)
		require.Equal(t, os.FileMode(0700), mode)
		return nil
	}
	svc.writeFile = func(path string, data []byte, mode os.FileMode) error {
		require.Equal(t, unitPath, path)
		written = data
		modes = append(modes, mode)
		return nil
	}
	svc.removeFile = func(path string) error {
		require.Equal(t, unitPath, path)
		return nil
	}
	svc.runner = func(_ context.Context, command string, args ...string) error {
		calls = append(calls, append([]string{command}, args...))
		return nil
	}

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

func TestSystemdUserServiceRejectsSystemMode(t *testing.T) {
	svc := NewSystemdUserService(ServiceSpec{UnitPath: "/tmp/unit"})
	require.ErrorContains(t, svc.Install(context.Background()), "only supports user-mode")
}

func TestSystemdUserServiceStatusNotInstalled(t *testing.T) {
	svc := NewSystemdUserService(ServiceSpec{Name: "pinner-mcp.service"})
	svc.runOutput = func(context.Context, string, ...string) (string, error) {
		return "LoadState=not-found\n", errors.New("systemctl failed")
	}
	status, err := svc.Status(context.Background())
	require.NoError(t, err)
	require.Equal(t, ServiceStatus{Summary: "not installed"}, status)
}

func TestSystemdUserCommand(t *testing.T) {
	require.True(t, reflect.DeepEqual([]string{"--user", "status", "unit"}, systemdUserCommand("status", "unit")))
}
