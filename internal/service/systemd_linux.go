//go:build linux

package service

// systemd Linux backend. The unit layout (systemd user unit under
// ~/.config/systemd/user, systemctl --user lifecycle, UserService option)
// follows the conventions of github.com/kardianos/service's systemd support.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	register(&systemdSystem{})
}

// systemdSystem implements System for Linux init systems.
type systemdSystem struct{}

func (systemdSystem) String() string { return "systemd" }

// Detect reports whether the systemd backend applies on this host. Pinner's
// service feature installs a rootless systemd *user* unit, so on Linux it is
// the backend (there is no alternative init system we target); System.Detect
// exists so a future sysv/upstart fallback can slot in and be preferred first
// when present. Returning true keeps service.New deterministic and
// host-independent per platform, so command logic is unit-testable anywhere.
func (systemdSystem) Detect() bool {
	return true
}

func (systemdSystem) New(cfg Config) Service {
	return newSystemdService(cfg)
}

// systemdService manages a systemd unit via the systemctl CLI.
type systemdService struct {
	cfg Config
}

// newSystemdService builds a systemd backend for the given config.
func newSystemdService(cfg Config) *systemdService {
	if cfg.Runner == nil {
		cfg.Runner = runCommand
	}
	if cfg.OutputRun == nil {
		cfg.OutputRun = runCommandOutput
	}
	if cfg.WriteFile == nil {
		cfg.WriteFile = os.WriteFile
	}
	if cfg.RemoveFile == nil {
		cfg.RemoveFile = os.Remove
	}
	if cfg.MkdirAll == nil {
		cfg.MkdirAll = os.MkdirAll
	}
	return &systemdService{cfg: cfg}
}

func (s *systemdService) unitName() string {
	if s.cfg.Name != "" {
		return s.cfg.Name + ".service"
	}
	return defaultServiceName + ".service"
}

func (s *systemdService) unitPath() string {
	if s.cfg.ServiceFile != "" {
		return s.cfg.ServiceFile
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "systemd", "user", s.unitName())
}

func (s *systemdService) Install(ctx context.Context) error {
	if !s.cfg.UserMode {
		return errors.New("systemd backend only supports user-mode services")
	}
	unitPath := s.unitPath()
	if unitPath == "" {
		return errors.New("systemd service unit path is required")
	}
	if err := s.cfg.MkdirAll(filepath.Dir(unitPath), 0700); err != nil {
		return fmt.Errorf("create systemd user unit directory: %w", err)
	}
	if err := s.cfg.WriteFile(unitPath, []byte(renderSystemdUnit(s.cfg)), 0600); err != nil {
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

func (s *systemdService) Uninstall(ctx context.Context) error {
	if err := s.run(ctx, "disable", "--now", s.unitName()); err != nil {
		return fmt.Errorf("disable systemd user service: %w", err)
	}
	unitPath := s.unitPath()
	if unitPath != "" {
		if err := s.cfg.RemoveFile(unitPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove systemd user unit: %w", err)
		}
	}
	if err := s.run(ctx, "daemon-reload"); err != nil {
		return fmt.Errorf("reload systemd user manager: %w", err)
	}
	return nil
}

func (s *systemdService) Start(ctx context.Context) error {
	return s.run(ctx, "start", s.unitName())
}

func (s *systemdService) Stop(ctx context.Context) error {
	return s.run(ctx, "stop", s.unitName())
}

func (s *systemdService) Restart(ctx context.Context) error {
	return s.run(ctx, "restart", s.unitName())
}

func (s *systemdService) Status(ctx context.Context) (Status, error) {
	// The running state comes from `systemctl is-active`, the documented
	// machine-readable probe. It prints one of active/activating/reloading/
	// deactivating/inactive/failed and exits non-zero when the unit is not
	// running (exit 3 for inactive/failed).
	raw, err := s.cfg.OutputRun(ctx, "systemctl", "--user", "is-active", s.unitName())
	state := parseSystemdActiveState(raw)

	// CombinedOutput surfaces a non-zero `is-active` exit (inactive/failed) as
	// an error even though stdout carries a real state token — that is not a
	// failure. But a genuine backend failure (no D-Bus session, systemctl
	// missing, permission denied) also comes back as error with non-empty
	// stderr. Only treat output as a state token when it is a known systemd
	// state word; otherwise the error is real and must propagate, not be
	// masked as an installed service with a garbage summary.
	if err != nil && state == systemdStateUnknown {
		return Status{}, fmt.Errorf("query systemd user service: %w", err)
	}

	switch state {
	case systemdActive:
		// A running unit is proof it is installed.
		return Status{Installed: true, Active: true, Ready: true, Summary: "active (running)"}, nil
	case systemdActivating, systemdReloading, systemdDeactivating, systemdFailed, systemdInactive, systemdStateUnknown:
		// Every non-running state must still confirm the unit file exists before
		// reporting installed: a transient/loaded-but-removed unit (file
		// deleted after load, stale activator) is not installable. Only
		// systemdActive is proof of an installed unit.
		installed, ierr := s.isInstalled(ctx)
		if ierr != nil {
			return Status{}, ierr
		}
		if !installed {
			return Status{Summary: "not installed"}, nil
		}
		switch state {
		case systemdFailed:
			return Status{Installed: true, Summary: "failed"}, nil
		case systemdDeactivating:
			return Status{Installed: true, Summary: "deactivating"}, nil
		case systemdActivating, systemdReloading:
			return Status{Installed: true, Active: true, Summary: "activating"}, nil
		default:
			return Status{Installed: true, Summary: "inactive"}, nil
		}
	default:
		return Status{Installed: true, Summary: string(state)}, nil
	}
}

// isInstalled reports whether a unit file exists for this service by querying
// `systemctl --user list-unit-files --output=json <unit>` and matching the
// returned unit files. list-unit-files is authoritative for installed unit
// files (unlike `show`, whose empty output on a missing unit is
// indistinguishable from a backend failure).
func (s *systemdService) isInstalled(ctx context.Context) (bool, error) {
	output, err := s.cfg.OutputRun(ctx, "systemctl", "--user", "list-unit-files", "--output=json", s.unitName())
	if err != nil {
		// A missing unit yields an empty JSON array `[]`, which parses without
		// error; a non-zero exit without parseable output is a real failure.
		if !strings.Contains(output, "[") {
			return false, fmt.Errorf("query systemd unit files: %w", err)
		}
	}
	return unitFileListContains(output, s.unitName()), nil
}

// unitFileListContains reports whether the JSON array produced by
// `systemctl list-unit-files --output=json` contains the given unit name.
// Entries are {"unit_file": "...", "state": "..."}. A missing unit yields "[]".
func unitFileListContains(jsonOut, unitName string) bool {
	var files []struct {
		UnitFile string `json:"unit_file"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &files); err != nil {
		// Fall back to a substring check if a systemd version changes the JSON
		// shape, rather than misreporting an installed unit as missing.
		return strings.Contains(jsonOut, unitName)
	}
	for _, f := range files {
		if f.UnitFile == unitName {
			return true
		}
	}
	return false
}

// systemdActiveState is the set of unit activation states reported by
// `systemctl is-active` (systemd.unit(5): active, activating, reloading,
// deactivating, inactive, failed). Typed so Status can distinguish a known
// state token from arbitrary stderr text on a failed call.
type systemdActiveState string

const (
	systemdActive       systemdActiveState = "active"
	systemdActivating   systemdActiveState = "activating"
	systemdReloading    systemdActiveState = "reloading"
	systemdDeactivating systemdActiveState = "deactivating"
	systemdInactive     systemdActiveState = "inactive"
	systemdFailed       systemdActiveState = "failed"
	systemdStateUnknown systemdActiveState = ""
)

// parseSystemdActiveState maps the raw `systemctl is-active` combined output to
// the typed state. The state is the first whitespace-delimited token on stdout;
// stderr may be coalesced onto the same line by CombinedOutput (e.g.
// "inactive\nFailed to get properties: ..."), so only the first token is
// matched. Anything that is not a known state word is reported as
// systemdStateUnknown (e.g. a bus error with no state token).
func parseSystemdActiveState(output string) systemdActiveState {
	fields := strings.Fields(output)
	tok := ""
	if len(fields) > 0 {
		tok = fields[0]
	}
	switch systemdActiveState(tok) {
	case systemdActive, systemdActivating, systemdReloading,
		systemdDeactivating, systemdInactive, systemdFailed:
		return systemdActiveState(tok)
	default:
		return systemdStateUnknown
	}
}

func (s *systemdService) Logs(ctx context.Context, follow bool) error {
	args := []string{"--user", "-u", s.unitName()}
	if follow {
		args = append(args, "--follow")
	}
	cmd := execCommandContext(ctx, "journalctl", args...)
	return cmd.Run()
}

func (s *systemdService) run(ctx context.Context, args ...string) error {
	full := append([]string{"--user"}, args...)
	return s.cfg.Runner(ctx, "systemctl", full...)
}

// renderSystemdUnit renders a hardened systemd user unit for the given config.
// Secrets are never placed in ExecStart arguments; they live in the
// Environment= block (from Config.EnvVars) or a referenced env file.
func renderSystemdUnit(cfg Config) string {
	var b strings.Builder
	desc := cfg.Description
	if desc == "" {
		desc = cfg.Name
	}
	fmt.Fprintf(&b, "[Unit]\nDescription=%s\nAfter=network-online.target\nWants=network-online.target\n\n", systemdEscape(desc))
	b.WriteString("[Service]\nType=simple\n")
	fmt.Fprintf(&b, "ExecStart=%s", systemdEscape(cfg.ExecPath))
	for _, arg := range cfg.Arguments {
		fmt.Fprintf(&b, " %s", systemdEscape(arg))
	}
	b.WriteString("\nRestart=on-failure\nRestartSec=5\nNoNewPrivileges=true\nPrivateTmp=true\nUMask=0077\n")
	if cfg.EnvFile != "" {
		fmt.Fprintf(&b, "EnvironmentFile=%s\n", systemdEscape(cfg.EnvFile))
	}
	for k, v := range cfg.EnvVars {
		fmt.Fprintf(&b, "Environment=%s=%s\n", k, systemdEscape(v))
	}
	b.WriteString("\n[Install]\nWantedBy=default.target\n")
	return b.String()
}

func systemdEscape(value string) string {
	if value == "" {
		return `""`
	}
	if strings.IndexFunc(value, func(r rune) bool { return strings.ContainsRune(" \t\"'\\", r) }) == -1 {
		return value
	}
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}
