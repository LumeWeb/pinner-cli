//go:build darwin

package service

// launchd macOS backend. The plist layout (user LaunchAgents under
// ~/Library/LaunchAgents, launchctl load/unload lifecycle, EnvironmentVariables
// and KeepAlive/RunAtLoad keys) follows the conventions of
// github.com/kardianos/service's LaunchAgent support.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func init() {
	register(&launchdSystem{})
}

// launchdSystem implements System for macOS launchd.
type launchdSystem struct{}

func (launchdSystem) String() string { return "launchd" }

// Detect reports whether launchd applies on this host. It is the only target on
// macOS, so it always applies; keeping the probe lets a future alternative
// macOS service manager slot in and be preferred first.
func (launchdSystem) Detect() bool {
	return true
}

func (launchdSystem) New(cfg Config) Service {
	return newLaunchdService(cfg)
}

// launchdService manages a launchd job via the launchctl CLI.
type launchdService struct {
	cfg Config
}

// newLaunchdService builds a launchd backend for the given config.
func newLaunchdService(cfg Config) *launchdService {
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
	return &launchdService{cfg: cfg}
}

func (s *launchdService) label() string {
	if s.cfg.Name != "" {
		return s.cfg.Name
	}
	return defaultServiceName
}

func (s *launchdService) plistPath() (string, error) {
	if s.cfg.ServiceFile != "" {
		return s.cfg.ServiceFile, nil
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(dir, "Library", "LaunchAgents", s.label()+".plist"), nil
}

// Install writes the LaunchAgent plist and loads it so it takes effect.
func (s *launchdService) Install(ctx context.Context) error {
	if !s.cfg.UserMode {
		return errors.New("launchd backend only supports user-mode LaunchAgents")
	}
	plistPath, err := s.plistPath()
	if err != nil {
		return err
	}
	if err := s.cfg.MkdirAll(filepath.Dir(plistPath), 0700); err != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", err)
	}
	// The plist redirects stdout/stderr to a co-located ~/Library/Logs file
	// (StandardOutPath/StandardErrorPath); ensure that directory exists too, on
	// the off chance it has not been created yet, so the job can write its logs.
	logDir := filepath.Dir(logPathFor(plistPath, "out"))
	if logDir != filepath.Dir(plistPath) {
		if err := s.cfg.MkdirAll(logDir, 0700); err != nil {
			return fmt.Errorf("create launchd log directory: %w", err)
		}
	}
	// Load the env file so its values can be inlined into the plist's
	// EnvironmentVariables dict. launchd has no native EnvironmentFile, and
	// shell-sourcing a KEY=VALUE file is unsafe (secrets may contain $, ` or
	// ;), so the values are written into the 0600 plist and parsed literally
	// by launchd at runtime.
	if err := s.refreshPlist(); err != nil {
		return err
	}
	// Tolerate an already-loaded job (re-install, or a retry after a partial
	// failure elsewhere) so Install stays idempotent, mirroring Start.
	if err := s.run(ctx, "load", plistPath); err != nil && !isAlreadyLoadedRun(err) {
		return fmt.Errorf("load LaunchAgent: %w", err)
	}
	return nil
}

// refreshPlist re-renders the LaunchAgent plist from the current env file, so
// credential rotations are reflected before the next `launchctl load`. launchd
// has no native EnvironmentFile, so the (0600) plist's EnvironmentVariables
// dict is inlined from the env source and parsed literally by launchd.
func (s *launchdService) refreshPlist() error {
	plistPath, err := s.plistPath()
	if err != nil {
		return err
	}
	cfg := s.cfg
	if cfg.EnvFile != "" {
		env, lerr := LoadEnvironment(cfg.EnvFile)
		if lerr != nil {
			return fmt.Errorf("load service environment %q: %w", cfg.EnvFile, lerr)
		}
		cfg.EnvVars = env
	}
	if err := s.cfg.WriteFile(plistPath, []byte(renderLaunchdPlist(cfg, plistPath)), 0600); err != nil {
		return fmt.Errorf("write LaunchAgent plist: %w", err)
	}
	return nil
}

// Uninstall unloads the job and removes the plist.
func (s *launchdService) Uninstall(ctx context.Context) error {
	plistPath, err := s.plistPath()
	// Unload the job if a plist exists; ignore a missing file so a second
	// uninstall is idempotent.
	if err == nil {
		if perr := s.run(ctx, "unload", plistPath); perr != nil && !isNotInstalledRun(perr) {
			return fmt.Errorf("unload LaunchAgent: %w", perr)
		}
		if rerr := s.cfg.RemoveFile(plistPath); rerr != nil && !os.IsNotExist(rerr) {
			return fmt.Errorf("remove LaunchAgent plist: %w", rerr)
		}
	}
	return nil
}

// launchctlListState probes whether the job is registered with launchd and, if
// so, whether it is currently running. running is true only when the job holds
// a numeric PID. A backend failure (missing launchctl, broken session,
// permission denied) is returned as an error rather than misread as "not
// registered", mirroring the systemd Status guard that distinguishes a real
// failure from an absent job.
func (s *launchdService) launchctlListState(ctx context.Context) (registered, running bool, err error) {
	out, lerr := s.cfg.OutputRun(ctx, "launchctl", "list", s.label())
	if lerr != nil {
		// Only a recognized absent-job token counts as not-registered; a
		// genuine backend failure must propagate so Status reports it instead
		// of "not installed" and Start does not attempt a stale load. An error
		// with empty output is NOT proof of absence (a broken session or
		// missing launchctl can also produce no stdout), so it propagates too.
		if isNotInstalledRun(lerr) {
			return false, false, nil
		}
		return false, false, lerr
	}
	return true, launchdPIDRe.MatchString(out), nil
}

func (s *launchdService) Start(ctx context.Context) error {
	plistPath, err := s.plistPath()
	if err != nil {
		return err
	}
	// Reflect the current env file in the plist before loading so credential
	// rotations are picked up on start/restart — `launchctl load` reads the
	// plist fresh each time, matching how systemd re-reads EnvironmentFile=
	// on every start.
	if err := s.refreshPlist(); err != nil {
		return err
	}
	// The kardianos/service pattern: `launchctl load` reads the (refreshed)
	// plist and loads/starts the job. An already-registered job reports
	// "service already loaded"; tolerate it as a no-op so start/restart stay
	// idempotent.
	if err := s.run(ctx, "load", plistPath); err != nil && !isAlreadyLoadedRun(err) {
		return fmt.Errorf("load LaunchAgent: %w", err)
	}
	return nil
}

func (s *launchdService) Stop(ctx context.Context) error {
	plistPath, err := s.plistPath()
	if err != nil {
		return err
	}
	// `launchctl unload` of a never-loaded job errors with "Could not find
	// specified service"; treat that the same as Uninstall does so stop/restart
	// stay idempotent.
	if err := s.run(ctx, "unload", plistPath); err != nil && !isNotInstalledRun(err) {
		return fmt.Errorf("unload LaunchAgent: %w", err)
	}
	return nil
}

func (s *launchdService) Restart(ctx context.Context) error {
	if err := s.Stop(ctx); err != nil {
		return err
	}
	return s.Start(ctx)
}

var launchdPIDRe = regexp.MustCompile(`\s*"PID"\s*=\s*([0-9]+);`)

func (s *launchdService) Status(ctx context.Context) (Status, error) {
	registered, running, lerr := s.launchctlListState(ctx)
	if lerr != nil {
		// A genuine backend probe failure (missing launchctl, broken session,
		// permission denied) must be reported, not misread as "not installed".
		return Status{}, fmt.Errorf("query launchctl list: %w", lerr)
	}
	if !registered {
		// `launchctl list <label>` prints nothing and exits non-zero when the
		// job is not loaded. If the plist file exists the job is merely stopped;
		// otherwise treat it as not installed.
		plistPath, perr := s.plistPath()
		if perr == nil {
			if _, serr := os.Stat(plistPath); serr == nil {
				return Status{Installed: true, Summary: "inactive"}, nil
			}
		}
		return Status{Summary: "not installed"}, nil
	}
	if running {
		// A present PID means the job is running.
		return Status{Installed: true, Active: true, Ready: true, Summary: "active (running)"}, nil
	}
	// Loaded but not currently running (e.g. KeepAlive-adjacent state).
	return Status{Installed: true, Summary: "inactive"}, nil
}

func (s *launchdService) Logs(ctx context.Context, follow bool) error {
	// The generated plist redirects the service's stdout/stderr to co-located
	// log files (StandardOutPath/StandardErrorPath), so the output never lands
	// in the macOS unified log — read those files instead. Failing to resolve
	// the plist path or a missing file yields the nil/stale case cleanly.
	plistPath, err := s.plistPath()
	if err != nil {
		return err
	}
	// Prefer stdout; if no program has written yet, the file may not exist,
	// which is reported by cat/tail rather than silently succeeding.
	out := logPathFor(plistPath, "out")
	if follow {
		cmd := execCommandContext(ctx, "tail", "-f", out)
		return cmd.Run()
	}
	cmd := execCommandContext(ctx, "cat", out)
	return cmd.Run()
}

func (s *launchdService) run(ctx context.Context, args ...string) error {
	return s.cfg.Runner(ctx, "launchctl", args...)
}

// renderLaunchdPlist renders a launchd LaunchAgent plist for the given config.
// The plist references the env file path via EnvironmentVariables (loaded at
// install time from Config.EnvVars) so the running process sees the tunnel
// credentials. Secrets are written into the 0600 plist file.
func renderLaunchdPlist(cfg Config, plistPath string) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	label := cfg.Name
	if label == "" {
		label = defaultServiceName
	}
	fmt.Fprintf(&b, "	<key>Label</key>\n	<string>%s</string>\n", plistEscape(label))
	// Run the binary directly (no shell). Tunnel credentials are delivered via
	// the EnvironmentVariables block below, populated at install time from the
	// env file; launchd parses those literally (no shell string), so a secret
	// containing $, backtick, or ; is never interpreted or executed. The plist
	// file itself is written 0600.
	fmt.Fprintf(&b, "	<key>ProgramArguments</key>\n	<array>\n		<string>%s</string>\n", plistEscape(cfg.ExecPath))
	for _, arg := range cfg.Arguments {
		fmt.Fprintf(&b, "		<string>%s</string>\n", plistEscape(arg))
	}
	b.WriteString("	</array>\n")
	// launchd has no native EnvironmentFile; inline the values into the plist's
	// EnvironmentVariables dict (populated from the env file at install time).
	if len(cfg.EnvVars) > 0 {
		b.WriteString("	<key>EnvironmentVariables</key>\n	<dict>\n")
		for k, v := range cfg.EnvVars {
			fmt.Fprintf(&b, "		<key>%s</key>\n		<string>%s</string>\n", plistEscape(k), plistEscape(v))
		}
		b.WriteString("	</dict>\n")
	}
	b.WriteString("\t<key>KeepAlive</key>\n\t<true/>\n")
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("\t<key>StandardOutPath</key>\n\t<string>" + plistEscape(logPathFor(plistPath, "out")) + "</string>\n")
	b.WriteString("\t<key>StandardErrorPath</key>\n\t<string>" + plistEscape(logPathFor(plistPath, "err")) + "</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

// logPathFor returns the stdout/stderr log file path for a plist, co-located
// in the same LaunchAgents-ish directory under a logs/ folder. The base label
// is derived from the plist filename.
func logPathFor(plistPath, kind string) string {
	dir := filepath.Dir(plistPath)
	base := strings.TrimSuffix(filepath.Base(plistPath), ".plist")
	return filepath.Join(dir, "..", "Logs", base+"."+kind+".log")
}

// plistEscape escapes a string for inclusion inside XML string elements.
func plistEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

// isNotInstalledRun reports whether a launchctl run error reflects a job that
// is not loaded (so unload/stop can be treated as a successful no-op).
func isNotInstalledRun(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "could not find") || strings.Contains(msg, "not loaded")
}

// isAlreadyLoadedRun reports whether a launchctl run error reflects a job that
// is already registered (so a load can be treated as a successful no-op).
func isAlreadyLoadedRun(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already loaded") || strings.Contains(msg, "already bootstrapped")
}
