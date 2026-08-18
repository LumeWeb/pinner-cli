package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/build"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// DoctorReport contains diagnostic information about the pinner CLI environment
type DoctorReport struct {
	Version        string         `json:"version"`
	OS             string         `json:"os"`
	Arch           string         `json:"arch"`
	Config         ConfigInfo     `json:"config"`
	Limits         LimitsInfo     `json:"limits"`
	Authentication AuthInfo       `json:"authentication"`
	Completion     CompletionInfo `json:"completion"`
}

// ConfigInfo contains configuration-related diagnostic information
type ConfigInfo struct {
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Secure   bool   `json:"secure"`
	Endpoint string `json:"endpoint"`
}

// LimitsInfo contains resource limits information
type LimitsInfo struct {
	Memory     string `json:"memory"`
	MaxRetries int    `json:"maxRetries"`
}

// AuthInfo contains authentication status information
type AuthInfo struct {
	Authenticated bool `json:"authenticated"`
}

// CompletionInfo contains shell completion status information
type CompletionInfo struct {
	Enabled    bool     `json:"enabled"`
	Shells     []string `json:"shells"`
	Configured []string `json:"configured"`
}

func newDoctorCommand() *cli.Command {
	return &cli.Command{
		Name:     "doctor",
		Category: "System",
		Usage:    "Display diagnostic information for troubleshooting",
		Description: `Show diagnostic information about your pinner CLI environment,
including version, OS details, configuration location, and limits.

This is useful when reporting issues or troubleshooting problems.

Examples:
  pinner doctor
  pinner doctor --json

Use this command when:
  - Reporting bugs or issues
  - Troubleshooting connection problems
  - Verifying your configuration
  - Checking authentication status`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "json",
				Aliases: []string{"j"},
				Usage:   "Output in JSON format",
			},
		},
		Metadata: WithTutorial(6, "Show diagnostic info", "pinner doctor"),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			jsonOutput := cmd.Bool(FlagJSON)
			output := NewOutputFormatter(jsonOutput, false, false, false)
			output.SetWriter(cmd.Root().Writer)
			return doctor(ctx, newCLICommandWrapper(cmd), output, defaultConfigManagerFactory)
		},
	}
}

func doctor(ctx context.Context, cmd flagGetter, output Output, cfgMgrFactory ConfigManagerFactory) error {
	cfgMgr, err := cfgMgrFactory()
	if err != nil {
		return fmt.Errorf("failed to create config manager: %w", err)
	}

	cfg := cfgMgr.Config()

	// Gather diagnostic information
	report := DoctorReport{
		Version: build.String(),
		OS:      build.GetInfo().Platform,
		Arch:    build.GetInfo().Architecture,
		Config: ConfigInfo{
			Path:     config.DefaultConfigPath,
			Exists:   configExists(),
			Secure:   cfg.Secure,
			Endpoint: getEndpointDisplay(cfg),
		},
		Limits: LimitsInfo{
			Memory:     formatMemoryLimit(cfg.MemoryLimit),
			MaxRetries: cfg.MaxRetries,
		},
		Authentication: AuthInfo{
			Authenticated: cfg.AuthToken != "",
		},
		Completion: checkCompletion(),
	}

	if cmd.Bool("json") {
		if err := output.PrintJSON(report); err != nil {
			return err
		}
		return nil
	}

	// Display formatted output
	printDoctorReport(report, output)
	return nil
}

func configExists() bool {
	expandedPath, err := expandPath(config.DefaultConfigPath)
	if err != nil {
		return false
	}
	_, err = os.Stat(expandedPath)
	return err == nil
}

func getEndpointDisplay(cfg *config.Config) string {
	if cfg.BaseEndpoint != "" {
		if cfg.Secure {
			return "https://" + cfg.BaseEndpoint
		}
		return "http://" + cfg.BaseEndpoint
	}
	if cfg.Secure {
		return "https://" + config.DefaultBaseDomain
	}
	return "http://" + config.DefaultBaseDomain
}

func formatMemoryLimit(limit uint64) string {
	if limit == 0 {
		return fmt.Sprintf("%d (default)", config.DefaultMemoryLimitMB)
	}
	return fmt.Sprintf("%d MB", limit)
}

func printDoctorReport(report DoctorReport, output Output) {
	configStatus := "Not found (will use defaults)"
	if report.Config.Exists {
		configStatus = "Found"
	}

	authStatus := "Not authenticated (run 'pinner auth')"
	if report.Authentication.Authenticated {
		authStatus = "Authenticated"
	}

	completionStatus := "Not configured (run 'pinner completion <shell>')"
	if len(report.Completion.Configured) > 0 {
		completionStatus = fmt.Sprintf("Enabled (%s)", strings.Join(report.Completion.Configured, ", "))
	}

	output.PrintFields(FieldGroup{
		Fields: []Field{
			{Label: "Version", Value: report.Version},
			{Label: "OS", Value: fmt.Sprintf("%s/%s", report.OS, report.Arch)},
			{Label: "Config", Value: report.Config.Path},
			{Label: "Config Status", Value: configStatus},
			{Label: "Endpoint", Value: report.Config.Endpoint},
			{Label: "Memory limit", Value: report.Limits.Memory},
			{Label: "Max retries", Value: strconv.FormatInt(int64(report.Limits.MaxRetries), 10)},
			{Label: "Authentication", Value: authStatus},
			{Label: "Shell completion", Value: completionStatus},
		},
	})
}

func expandPath(path string) (string, error) {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home + path[1:], nil
	}
	return path, nil
}

// CompletionDetector defines the interface for checking shell completion configuration.
type CompletionDetector interface {
	// Name returns the shell name.
	Name() string

	// IsConfigured returns true if completion is configured for this shell.
	IsConfigured() (bool, error)

	// InstallCommand returns the command to install completion.
	InstallCommand() string

	// ConfigPath returns the path to the shell config file.
	ConfigPath() string
}

// BashCompletionDetector checks for bash completion configuration.
type BashCompletionDetector struct {
	homeDir string
}

func (d *BashCompletionDetector) Name() string {
	return "bash"
}

func (d *BashCompletionDetector) IsConfigured() (bool, error) {
	bashrc, err := os.ReadFile(d.homeDir + "/.bashrc")
	if err != nil {
		return false, nil
	}
	content := string(bashrc)
	return strings.Contains(content, "pinner completion bash") ||
		strings.Contains(content, "source <(pinner completion"), nil
}

func (d *BashCompletionDetector) InstallCommand() string {
	return "source <(pinner completion bash)"
}

func (d *BashCompletionDetector) ConfigPath() string {
	return d.homeDir + "/.bashrc"
}

// ZshCompletionDetector checks for zsh completion configuration.
type ZshCompletionDetector struct {
	homeDir string
}

func (d *ZshCompletionDetector) Name() string {
	return "zsh"
}

func (d *ZshCompletionDetector) IsConfigured() (bool, error) {
	zshrc, err := os.ReadFile(d.homeDir + "/.zshrc")
	if err != nil {
		return false, nil
	}
	content := string(zshrc)
	return strings.Contains(content, "pinner completion zsh") ||
		strings.Contains(content, "source <(pinner completion"), nil
}

func (d *ZshCompletionDetector) InstallCommand() string {
	return "source <(pinner completion zsh)"
}

func (d *ZshCompletionDetector) ConfigPath() string {
	return d.homeDir + "/.zshrc"
}

// FishCompletionDetector checks for fish completion configuration.
type FishCompletionDetector struct {
	homeDir string
}

func (d *FishCompletionDetector) Name() string {
	return "fish"
}

func (d *FishCompletionDetector) IsConfigured() (bool, error) {
	fishComp, err := os.ReadFile(d.homeDir + "/.config/fish/completions/pinner.fish")
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(fishComp), "pinner"), nil
}

func (d *FishCompletionDetector) InstallCommand() string {
	return "pinner completion fish > " + d.homeDir + "/.config/fish/completions/pinner.fish"
}

func (d *FishCompletionDetector) ConfigPath() string {
	return d.homeDir + "/.config/fish/completions/pinner.fish"
}

// PowerShellCompletionDetector checks for PowerShell completion configuration.
type PowerShellCompletionDetector struct{}

func (d *PowerShellCompletionDetector) Name() string {
	return "pwsh"
}

func (d *PowerShellCompletionDetector) IsConfigured() (bool, error) {
	// Only check on Windows
	if runtime.GOOS != "windows" {
		return false, nil
	}

	profile := os.Getenv("PROFILE")
	if profile == "" {
		return false, nil
	}
	ps1, err := os.ReadFile(profile)
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(ps1), "pinner completion pwsh"), nil
}

func (d *PowerShellCompletionDetector) InstallCommand() string {
	return "pinner completion pwsh | Out-File -Append $PROFILE"
}

func (d *PowerShellCompletionDetector) ConfigPath() string {
	return "$PROFILE"
}

// CompletionDetectorFactory creates detectors for available shells.
type CompletionDetectorFactory struct {
	homeDir string
}

// NewCompletionDetectorFactory creates a new detector factory.
func NewCompletionDetectorFactory() (*CompletionDetectorFactory, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &CompletionDetectorFactory{
		homeDir: home,
	}, nil
}

// GetDetectors returns all available completion detectors for the current OS.
func (f *CompletionDetectorFactory) GetDetectors() []CompletionDetector {
	detectors := []CompletionDetector{
		&BashCompletionDetector{homeDir: f.homeDir},
		&ZshCompletionDetector{homeDir: f.homeDir},
		&FishCompletionDetector{homeDir: f.homeDir},
	}

	// Only add PowerShell detector on Windows
	if runtime.GOOS == "windows" {
		detectors = append(detectors, &PowerShellCompletionDetector{})
	}

	return detectors
}

// checkCompletion checks which shells have pinner completion configured.
func checkCompletion() CompletionInfo {
	info := CompletionInfo{
		Enabled:    false,
		Shells:     []string{"bash", "zsh", "fish"},
		Configured: []string{},
	}

	// Add PowerShell to available shells on Windows
	if runtime.GOOS == "windows" {
		info.Shells = append(info.Shells, "pwsh")
	}

	factory, err := NewCompletionDetectorFactory()
	if err != nil {
		return info
	}

	for _, detector := range factory.GetDetectors() {
		configured, err := detector.IsConfigured()
		if err != nil {
			continue
		}
		if configured {
			info.Configured = append(info.Configured, detector.Name())
		}
	}

	info.Enabled = len(info.Configured) > 0
	return info
}
