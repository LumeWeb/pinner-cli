//go:build windows

package service

// Windows SCM backend. The service lifecycle (SCM connect, CreateService,
// OpenService, ControlService, Query, Delete) and the environment-via-registry
// injection follow github.com/kardianos/service's Windows implementation.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	wreg "golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func init() {
	register(&windowsSystem{})
}

// windowsSystem implements System for the Windows Service Control Manager.
type windowsSystem struct{}

func (windowsSystem) String() string { return "windows" }

// Detect reports whether the SCM applies. It is the only target on Windows, so
// it always applies; keeping the probe mirrors the other backends.
func (windowsSystem) Detect() bool { return true }

func (windowsSystem) New(cfg Config) Service {
	return newWindowsService(cfg)
}

// windowsService manages a Windows service through the Service Control Manager.
type windowsService struct {
	cfg  Config
	name string
}

// newWindowsService builds a Windows SCM backend for the given config. It only
// constructs the handle; connecting to the SCM happens per-operation so a
// lightweight build never requires admin rights. The command seam is defaulted
// (like the other backends) so Logs never invokes a nil function.
func newWindowsService(cfg Config) *windowsService {
	if cfg.OutputRun == nil {
		cfg.OutputRun = runCommandOutput
	}
	name := cfg.Name
	if name == "" {
		name = defaultServiceName
	}
	return &windowsService{cfg: cfg, name: name}
}

// Install registers the service with the SCM, injects its environment, and
// enables auto-start.
func (s *windowsService) Install(ctx context.Context) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service control manager: %w", err)
	}
	defer m.Disconnect()

	if existing, err := m.OpenService(s.name); err == nil {
		existing.Close()
		return fmt.Errorf("service %s already exists", s.name)
	}

	// mgr.CreateService internally quotes the binary path (via appendCmdLine)
	// when it contains spaces, so pass ExecPath unquoted; manually quoting here
	// would double-quote and double-escape backslashes, breaking the ImagePath.
	srv, err := m.CreateService(s.name, s.cfg.ExecPath, mgr.Config{
		DisplayName: s.name,
		Description: s.cfg.Description,
		StartType:   mgr.StartAutomatic,
	}, s.cfg.Arguments...)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer srv.Close()

	// Write the environment only after the service exists, so a failed
	// CreateService leaves no orphan Environment value on a key with no
	// registered service.
	if err := s.setEnvInRegistry(); err != nil {
		_ = srv.Delete()
		// DeleteService does not remove the per-service registry key under
		// HKLM\SYSTEM\CurrentControlSet\Services\<name>, so scrub the
		// Environment value we may have just written (it can hold tokens) to
		// avoid orphaning it on a key with no registered service.
		s.removeEnvInRegistry()
		return err
	}
	return nil
}

// Uninstall removes the service from the SCM and its environment registry key.
func (s *windowsService) Uninstall(ctx context.Context) error {
	// Best-effort stop so the service is not left running without a definition.
	_ = s.Stop(ctx)

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service control manager: %w", err)
	}
	defer m.Disconnect()

	srv, err := m.OpenService(s.name)
	if err != nil {
		return fmt.Errorf("service %s is not installed", s.name)
	}
	defer srv.Close()

	if err := srv.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	s.removeEnvInRegistry()
	return nil
}

func (s *windowsService) Start(ctx context.Context) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	srv, err := m.OpenService(s.name)
	if err != nil {
		return fmt.Errorf("service %s is not installed", s.name)
	}
	defer srv.Close()
	if err := srv.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	return nil
}

func (s *windowsService) Stop(ctx context.Context) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	srv, err := m.OpenService(s.name)
	if err != nil {
		return fmt.Errorf("service %s is not installed", s.name)
	}
	defer srv.Close()
	if err := stopAndWait(ctx, srv); err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	return nil
}

func (s *windowsService) Restart(ctx context.Context) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	srv, err := m.OpenService(s.name)
	if err != nil {
		return fmt.Errorf("service %s is not installed", s.name)
	}
	defer srv.Close()
	if err := stopAndWait(ctx, srv); err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	if err := srv.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	return nil
}

func (s *windowsService) Status(ctx context.Context) (Status, error) {
	srv, err := openServiceForStatus(s.name)
	if err != nil {
		if errors.Is(err, errServiceNotInstalled) {
			return Status{Summary: "not installed"}, nil
		}
		return Status{}, err
	}
	defer srv.Close()

	st, err := srv.Query()
	if err != nil {
		return Status{}, fmt.Errorf("query service status: %w", err)
	}
	return scmStateToStatus(st.State), nil
}

func (s *windowsService) Logs(ctx context.Context, follow bool) error {
	// Windows services write to the Application event log. Query it for the
	// service's source with wevtutil via the command seam (so callers can
	// intercept/test the invocation). wevtutil has no native tail, so follow
	// advances a cursor of the newest EventRecordID: each poll re-queries with
	// an XPath filter for events strictly newer than the cursor, so every tick
	// emits only new events (never a re-print).
	if !follow {
		out, err := s.cfg.OutputRun(ctx, "wevtutil", s.logsArgs()...)
		fmt.Print(out)
		return err
	}
	ticker := time.NewTicker(eventPollInterval)
	defer ticker.Stop()
	cursor := 0
	consecFailures := 0
	for {
		// The first poll begins at record 0 and would otherwise dump the whole
		// log; cap that first view to the most recent 50 (matching --logs
		// one-shot), then cursor advances to the newest id seen. Subsequent
		// polls only fetch events strictly newer than the cursor.
		capArg := ""
		if cursor == 0 {
			capArg = "/c:50"
		}
		// /f:xml exposes the stable per-event EventRecordID used to advance the
		// cursor; /rd:true returns newest-first so the newest id sets the next
		// cursor in the returned batch.
		args := []string{"qe", "Application",
			fmt.Sprintf(`/q:*[System[Provider[@Name='%s'] and EventRecordID > %d]]`, xpathEscape(s.name), cursor),
			"/f:xml", "/rd:true"}
		if capArg != "" {
			args = append(args, capArg)
		}
		out, err := s.cfg.OutputRun(ctx, "wevtutil", args...)
		if err != nil {
			// A transient query error should just stall the tick, but a persistent
			// one (missing wevtutil, access denied, nonexistent source) can never
			// recover — keep silent forever and the tail looks like an idle
			// service while the poll burns CPU. After consecutiveFailures the tail
			// gives up and surfaces the error.
			consecFailures++
			if consecFailures >= maxEventPollFailures {
				return fmt.Errorf("logs follow: %w after %d consecutive polls", err, consecFailures)
			}
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
			continue
		}
		consecFailures = 0
		if newCursor := printNewEvents(out); newCursor > cursor {
			cursor = newCursor
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// maxEventPollFailures is how many consecutive failing wevtutil polls in follow
// mode before Logs gives up and returns the error, rather than silently polling
// forever on a failure that can never recover (e.g. missing wevtutil, access
// denied, nonexistent log source). The threshold keeps a single transient
// hiccup from killing the tail while still bounding a permanently dead one.
// It is a var (not a const) so the regression test can bound wall-clock time.
var (
	eventPollInterval    = 2 * time.Second
	maxEventPollFailures = 5
)

// logsArgs builds the wevtutil invocation. The query must carry the /q: switch
// (wevtutil rejects a bare positional XPath) and filter to the service's event
// source, newest-first, bounded to the most recent events. The source name is
// XPath-escaped (embedded single quotes doubled) so a name containing a quote
// still produces a valid [@Name='...'] literal, while other characters are
// passed verbatim as value bytes.
func (s *windowsService) logsArgs() []string {
	return []string{"qe", "Application", `/q:*[System[Provider[@Name='` + xpathEscape(s.name) + `']]]`, "/f:text", "/rd:true", "/c:50"}
}

// xpathEscape prepares a service name for embedding inside an XPath string
// literal in the wevtutil /q: filter. A literal single quote inside the value
// would terminate the [@Name='...'] string early and break the query, so it is
// doubled (” is XPath's escape for a literal quote inside a quoted string).
// Other XPath-significant characters ([ ] * etc.) are left intact: they are
// value bytes, not syntax, once inside the quoted literal.
func xpathEscape(name string) string {
	return strings.ReplaceAll(name, "'", "''")
}

// event block / record-id / data text extractors for wevtutil /f:xml output.
// These are the minimal pieces needed to advance a follow cursor: every event's
// stable EventRecordID and its human-readable Data payload.
var (
	winEventBlockRe = regexp.MustCompile(`(?s)<Event[ >].*?</Event>`)
	winEventIDRe    = regexp.MustCompile(`<EventRecordID>\s*(\d+)\s*</EventRecordID>`)
	winEventDataRe  = regexp.MustCompile(`<Data>(.*?)</Data>`)
)

// printNewEvents prints each event in a wevtutil /f:xml batch as "[id] data"
// and returns the highest EventRecordID seen (0 if none). Repeated message
// text is printed once per distinct event because events are keyed by record
// id, not by content.
func printNewEvents(out string) int {
	maxID := 0
	for _, blk := range winEventBlockRe.FindAllString(out, -1) {
		idMatch := winEventIDRe.FindStringSubmatch(blk)
		if len(idMatch) != 2 {
			continue
		}
		id, _ := strconv.Atoi(idMatch[1])
		if id > maxID {
			maxID = id
		}
		var data []string
		for _, d := range winEventDataRe.FindAllStringSubmatch(blk, -1) {
			if len(d) == 2 {
				data = append(data, d[1])
			}
		}
		line := strings.Join(data, " ")
		if line == "" {
			line = "(no message)"
		}
		fmt.Printf("[%d] %s\n", id, line)
	}
	return maxID
}

// mergedEnvVars returns the union of Config.EnvVars and the EnvFile's values,
// mirroring how the systemd renderer emits both Environment= and
// EnvironmentFile=. The env file wins on a key collision (it is the secret
// source), matching the other backends. An absent env file is tolerated.
func (s *windowsService) mergedEnvVars() (map[string]string, error) {
	envVars := make(map[string]string, len(s.cfg.EnvVars))
	for k, v := range s.cfg.EnvVars {
		envVars[k] = v
	}
	if s.cfg.EnvFile != "" {
		env, err := LoadEnvironment(s.cfg.EnvFile)
		if err != nil {
			return nil, fmt.Errorf("load service environment %q: %w", s.cfg.EnvFile, err)
		}
		for k, v := range env {
			envVars[k] = v
		}
	}
	return envVars, nil
}

// setEnvInRegistry loads the service env file and writes its KEY=*** pairs as
// the service's Environment multi-string under the SCM registry key. SCM has no
// runtime env-file fallback, so a corrupt/unreadable env file is fatal here:
// silently installing with no credentials would leave the service unable to
// authenticate at runtime. An absent env file is tolerated (a fresh, unprovisioned
// install legitimately has no credentials yet).
//
// SECURITY NOTE (root cause / final position): SCM is the only runtime env
// mechanism on Windows and it is registry-resident, so secret values
// (MCP_AUTH_TOKEN, NGROK_AUTHTOKEN, ...) are stored as plaintext at rest under
// HKLM\SYSTEM\CurrentControlSet\Services\<name>\Environment, which standard
// local users can read by default. This is not a pinner-cli deviation —
// kardianos/service (the reference SCM implementation) writes the same
// Config.EnvVars to the same Environment value on install — and there is no
// 0600-file equivalent on Windows that SCM auto-loads (unlike systemd
// EnvironmentFile= or launchd plist inlining). The frequently-suggested
// alternatives cannot work for an SCM service: DPAPI (CryptProtectData) and
// Credential Manager are both per-user secrets, bound to the installing
// admin's Windows account, but the service runs as LocalSystem under SCM and
// would be unable to decrypt/read them at start (only CRYPTPROTECT_LOCAL_MACHINE
// spans users, and that is decryptable by every local user — strictly worse).
// ACL-restricting the key still leaks via the service's own reads and diverges
// from the reference. The exposure is therefore an inherent, unavoidable
// platform property, owned by the operator at install time: it is populated
// only if `install` is given an env file / EnvVars containing these tokens
// (explicit opt-in), and it is announced with a loud install-time WARNING.
// Operators who require stronger at-rest protection must implement it in the
// service binary (e.g. fetch credentials from a keystore at startup), which is
// out of scope for the SCM backend.
func (s *windowsService) setEnvInRegistry() error {
	envVars, err := s.mergedEnvVars()
	if err != nil {
		return err
	}
	if len(envVars) == 0 {
		return nil
	}
	keyPath := `SYSTEM\CurrentControlSet\Services\` + s.name
	k, _, err := wreg.CreateKey(wreg.LOCAL_MACHINE, keyPath,
		wreg.QUERY_VALUE|wreg.SET_VALUE|wreg.CREATE_SUB_KEY)
	if err != nil {
		return fmt.Errorf("open service registry key: %w", err)
	}
	defer k.Close()

	envStrings := make([]string, 0, len(envVars))
	for key, val := range envVars {
		envStrings = append(envStrings, key+"="+val)
	}
	if err := k.SetStringsValue("Environment", envStrings); err != nil {
		return fmt.Errorf("write service environment: %w", err)
	}
	// Warn loudly at install time that the tokens just written are readable by
	// any standard local user (see the SECURITY NOTE above) — the at-rest
	// exposure surfaces exactly here, so the operator should see it now rather
	// than discover it later. This is the accepted platform model (kardianos
	// does the same); hardening the ACL / moving to DPAPI is deliberately out
	// of scope.
	fmt.Fprintln(os.Stderr, "WARNING: credentials were written in plaintext to the service registry key "+
		`HKLM\SYSTEM\CurrentControlSet\Services\`+s.name+`\Environment, `+
		`readable by any local user. This is the Windows SCM env mechanism; `+
		`prefer the official service-account env restrictions or a DPAPI/CredMan `+
		"store in the service binary if at-rest protection is required.")
	return nil
}

func (s *windowsService) removeEnvInRegistry() {
	k, err := wreg.OpenKey(wreg.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Services\`+s.name, wreg.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.DeleteValue("Environment")
}

// scmStateToStatus maps an SCM service state to the backend-independent Status.
func scmStateToStatus(state svc.State) Status {
	switch state {
	case svc.StartPending, svc.Running:
		return Status{Installed: true, Active: true, Ready: true, Summary: "active (running)"}
	case svc.StopPending, svc.Stopped, svc.PausePending, svc.Paused, svc.ContinuePending:
		return Status{Installed: true, Summary: "inactive"}
	default:
		return Status{Installed: true, Summary: "unknown"}
	}
}

// stopAndWait sends a stop control and waits for the service to reach Stopped.
// It is idempotent: an already-stopped service returns immediately, and a
// service already stopping (StopPending) is waited on rather than re-issued a
// redundant control (the SCM returns ERROR_SERVICE_NOT_ACTIVE for a stop on a
// service that is not running). The wait aborts on the context.
func stopAndWait(ctx context.Context, srv *mgr.Service) error {
	st, err := srv.Query()
	if err != nil {
		return err
	}
	if st.State == svc.Stopped {
		return nil
	}
	// A paused service never reaches Stopped without a prior Continue, so
	// stopping it would block the wait loop for the full timeout and then
	// fail. Surface it immediately instead of waiting on an unreachable state.
	if st.State == svc.Paused {
		return errors.New("service is paused; continue it before stopping")
	}
	// If another actor is already stopping the service, don't issue a second
	// stop — just wait for it to reach Stopped.
	if st.State != svc.StopPending {
		if _, err := srv.Control(svc.Stop); err != nil && !isNotActive(err) {
			return err
		}
	}

	const tick = 50 * time.Millisecond
	timeout := time.After(30 * time.Second)
	for {
		st, err := srv.Query()
		if err != nil {
			return err
		}
		if st.State == svc.Stopped {
			return nil
		}
		select {
		case <-time.After(tick):
		case <-timeout:
			return fmt.Errorf("timed out waiting for service to stop")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// isNotActive reports whether an SCM control error means the service is not
// running (ERROR_SERVICE_NOT_ACTIVE), i.e. a stop is a no-op.
func isNotActive(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE)
}

// errServiceNotInstalled marks an OpenService failure for a missing service.
var errServiceNotInstalled = errors.New("service not installed")

// openServiceForStatus opens the service for a status query, mapping a missing
// service to errServiceNotInstalled. The SCManager handle (from mgr.Connect) is
// released here on every path; the caller closes the returned *mgr.Service. Any
// OpenService failure other than "service does not exist" (e.g. access denied,
// transient SCM error) is propagated so Status does not misreport a real
// failure as "not installed".
func openServiceForStatus(name string) (*mgr.Service, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, fmt.Errorf("connect to service control manager: %w", err)
	}
	defer m.Disconnect()
	srv, err := m.OpenService(name)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil, errServiceNotInstalled
		}
		return nil, fmt.Errorf("open service: %w", err)
	}
	return srv, nil
}
