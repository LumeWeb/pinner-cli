package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const defaultMCPServiceName = "pinner-mcp.service"

// SystemdUserService manages a rootless systemd user unit.
type SystemdUserService struct {
	spec       ServiceSpec
	runner     serviceCommandRunner
	runOutput  serviceCommandOutputRunner
	writeFile  func(string, []byte, os.FileMode) error
	removeFile func(string) error
	mkdirAll   func(string, os.FileMode) error
}

// NewSystemdUserService creates a systemd user backend using the host system.
func NewSystemdUserService(spec ServiceSpec) *SystemdUserService {
	return &SystemdUserService{
		spec:       spec,
		runner:     runServiceCommand,
		runOutput:  runServiceCommandOutput,
		writeFile:  os.WriteFile,
		removeFile: os.Remove,
		mkdirAll:   os.MkdirAll,
	}
}

func (s *SystemdUserService) Install(ctx context.Context) error {
	if !s.spec.UserMode {
		return errors.New("systemd backend only supports user-mode services")
	}
	if s.spec.UnitPath == "" {
		return errors.New("systemd service unit path is required")
	}
	if err := s.mkdirAll(filepath.Dir(s.spec.UnitPath), 0700); err != nil {
		return fmt.Errorf("create systemd user unit directory: %w", err)
	}
	if err := s.writeFile(s.spec.UnitPath, []byte(renderSystemdUserUnit(s.spec)), 0600); err != nil {
		return fmt.Errorf("write systemd user unit: %w", err)
	}
	if err := s.run(ctx, "daemon-reload"); err != nil {
		return fmt.Errorf("reload systemd user manager: %w", err)
	}
	if err := s.run(ctx, "enable", s.unitName()); err != nil {
		return fmt.Errorf("enable systemd user service: %w", err)
	}
	return nil
}

func (s *SystemdUserService) Uninstall(ctx context.Context) error {
	if err := s.run(ctx, "disable", "--now", s.unitName()); err != nil {
		return fmt.Errorf("disable systemd user service: %w", err)
	}
	if err := s.removeFile(s.spec.UnitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove systemd user unit: %w", err)
	}
	if err := s.run(ctx, "daemon-reload"); err != nil {
		return fmt.Errorf("reload systemd user manager: %w", err)
	}
	return nil
}

func (s *SystemdUserService) Start(ctx context.Context) error {
	return s.run(ctx, "start", s.unitName())
}

func (s *SystemdUserService) Stop(ctx context.Context) error {
	return s.run(ctx, "stop", s.unitName())
}

func (s *SystemdUserService) Restart(ctx context.Context) error {
	return s.run(ctx, "restart", s.unitName())
}

func (s *SystemdUserService) Status(ctx context.Context) (ServiceStatus, error) {
	output, err := s.runOutput(ctx, "systemctl", systemdUserCommand("show", s.unitName(), "--property=LoadState,ActiveState,SubState", "--no-pager")...)
	if err != nil {
		if strings.Contains(output, "LoadState=not-found") {
			return ServiceStatus{Summary: "not installed"}, nil
		}
		return ServiceStatus{}, fmt.Errorf("query systemd user service: %w", err)
	}
	status := ServiceStatus{Installed: !strings.Contains(output, "LoadState=not-found")}
	status.Active = strings.Contains(output, "ActiveState=active")
	status.Ready = status.Active && strings.Contains(output, "SubState=running")
	if status.Ready {
		status.Summary = "active (running)"
	} else if status.Active {
		status.Summary = "active"
	} else {
		status.Summary = "inactive"
	}
	return status, nil
}

func (s *SystemdUserService) Logs(ctx context.Context, follow bool) error {
	args := []string{"--user", "-u", s.unitName()}
	if follow {
		args = append(args, "--follow")
	}
	return s.runRaw(ctx, "journalctl", args...)
}

func (s *SystemdUserService) unitName() string {
	if s.spec.Name != "" {
		return s.spec.Name
	}
	return defaultMCPServiceName
}

func (s *SystemdUserService) run(ctx context.Context, args ...string) error {
	return s.runRaw(ctx, "systemctl", systemdUserCommand(args...)...)
}

func (s *SystemdUserService) runRaw(ctx context.Context, command string, args ...string) error {
	if s.runner == nil {
		return errors.New("service command runner is not configured")
	}
	return s.runner(ctx, command, args...)
}

func renderSystemdUserUnit(spec ServiceSpec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Unit]\nDescription=%s\nAfter=network-online.target\nWants=network-online.target\n\n", systemdEscape(spec.Description))
	b.WriteString("[Service]\nType=simple\n")
	fmt.Fprintf(&b, "ExecStart=%s", systemdEscape(spec.ExecPath))
	for _, arg := range spec.Arguments {
		fmt.Fprintf(&b, " %s", systemdEscape(arg))
	}
	b.WriteString("\nRestart=on-failure\nRestartSec=5\nNoNewPrivileges=true\nPrivateTmp=true\nUMask=0077\n")
	if spec.EnvFile != "" {
		fmt.Fprintf(&b, "EnvironmentFile=%s\n", systemdEscape(spec.EnvFile))
	}
	b.WriteString("\n[Install]\nWantedBy=default.target\n")
	return b.String()
}

func systemdEscape(value string) string {
	if value == "" {
		return "\"\""
	}
	if strings.IndexFunc(value, func(r rune) bool { return strings.ContainsRune(" \t\"'\\", r) }) == -1 {
		return value
	}
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}

func runServiceCommand(ctx context.Context, command string, args ...string) error {
	cmd := exec.CommandContext(ctx, command, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %w: %s", command, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func runServiceCommandOutput(ctx context.Context, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s failed: %w: %s", command, err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
