package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.lumeweb.com/configmanager"
	"go.lumeweb.com/configmanager/source"
)

var (
	// ErrNotAuthenticated is returned when an operation requires authentication.
	ErrNotAuthenticated = errors.New("not authenticated: no auth token configured")

	// DefaultConfigPath is the default location for the config file.
	// Resolved at init time via resolveDefaultConfigPath() to be cross-platform:
	//   Linux:   ~/.config/pinner/config.yaml
	//   macOS:   ~/Library/Application Support/pinner/config.yaml
	//   Windows: %AppData%\pinner\config.yaml
	//
	// BC: If ~/.config/pinner/config.yaml exists on a non-Linux platform, it
	// is still used (legacy path takes priority).
	DefaultConfigPath = resolveDefaultConfigPath()
)

const (
	// configDirName is the application sub-directory under the user config dir.
	configDirName = "pinner"
	// configFileName is the config file name within the config directory.
	configFileName = "config.yaml"
	// legacyConfigSubdir is the legacy ~/.config path segment kept for BC.
	legacyConfigSubdir = ".config"
)

// resolveDefaultConfigPath determines the config file path using the OS-native
// config directory, with backward compatibility for the legacy ~/.config path.
// It returns an empty string when no user config or home directory is
// resolvable, letting callers fail fast instead of silently using a
// CWD-relative path.
func resolveDefaultConfigPath() string {
	// BC: check legacy Linux path first on all platforms
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		legacy := filepath.Join(home, legacyConfigSubdir, configDirName, configFileName)
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	// Cross-platform: use OS-native config dir
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		home, herr := os.UserHomeDir()
		if herr != nil || home == "" {
			// No home/config dir resolvable — return empty so callers can
			// fail fast instead of silently using a CWD-relative path.
			return ""
		}
		dir = filepath.Join(home, legacyConfigSubdir)
	}
	return filepath.Join(dir, configDirName, configFileName)
}

// Manager extends configmanager.Manager with CLI-specific configuration methods.
type Manager interface {
	configmanager.Manager
	Config() *Config
	ConfigPath() string
	Load() error
	Save() error
	SetAuthToken(token string) error
	SetBaseEndpoint(endpoint string) error
	SetAPIEndpoint(endpoint string) error
	SetMaxRetries(retries int) error
	SetSecure(secure bool) error
	RequireAuthenticated() error
	Reset() error
}

// managerImpl implements Manager.
type managerImpl struct {
	configmanager.Manager
	configPath string
}

// NewManager creates a new Manager instance with the specified config file path.
func NewManager(configPath string) (Manager, error) {
	if configPath == "" {
		configPath = DefaultConfigPath
	}
	if configPath == "" {
		return nil, fmt.Errorf("cannot determine config path: no user config or home directory resolvable")
	}

	configPath = expandPath(configPath)

	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	cm, err := configmanager.NewConfigManager([]source.ConfigSource{})
	if err != nil {
		return nil, fmt.Errorf("failed to create config manager: %w", err)
	}

	_, err = os.Stat(configPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to check config file: %w", err)
	}

	// Always register the file source - it will create the file when Persist is called
	fileSource := source.NewFileSource(configPath)
	cm.RegisterSource(fileSource)
	cm.RegisterNamespace("", fileSource)

	defaultSource := source.NewDefaultConfigSource(cm, source.WithDefaultSourceGlobal())
	cm.RegisterSource(defaultSource)

	if err = cm.RegisterStruct("", &Config{}); err != nil {
		return nil, fmt.Errorf("failed to register config struct: %w", err)
	}

	return &managerImpl{
		Manager:    cm,
		configPath: configPath,
	}, nil
}

// Config returns the current Config instance by reading from the config manager.
func (m *managerImpl) Config() *Config {
	_, decoded, err := m.Manager.Get("") //nolint:staticcheck // explicit to avoid recursion via Manager interface
	if err != nil {
		return NewConfig()
	}
	if decoded != nil {
		if cfg, ok := decoded.(*Config); ok {
			return cfg
		}
	}
	return NewConfig()
}

// Load loads configuration from all registered sources.
// If the config file doesn't exist, it continues with defaults (no error).
func (m *managerImpl) Load() error {
	err := m.Manager.Load()
	// If the file doesn't exist, just use defaults - not an error
	if err != nil && isFileNotFoundError(err) {
		return nil
	}
	return err
}

// isFileNotFoundError checks if an error is a "file not found" error.
func isFileNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// errors.Is walks the full Unwrap chain, which os.IsNotExist does not
	// do reliably through fmt.Errorf("%w") wrapping from the configmanager
	// library. This fixes file-not-found handling on Windows where the
	// underlying error string is locale-specific.
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	// Fallback: match OS-specific error strings for errors that don't
	// implement Unwrap (e.g. from third-party libraries that use
	// fmt.Errorf without %w).
	msg := err.Error()
	return strings.Contains(msg, "no such file or directory") ||
		strings.Contains(msg, "The system cannot find the file specified")
}

// Save persists the current configuration to disk.
func (m *managerImpl) Save() error {
	return m.Manager.Persist() //nolint:staticcheck // explicit to avoid recursion
}

// SetAuthToken sets the authentication token in the config.
func (m *managerImpl) SetAuthToken(token string) error {
	if err := m.Manager.Set(context.Background(), ConfigKeyAuthToken, token); err != nil { //nolint:staticcheck // explicit to avoid recursion
		return fmt.Errorf("failed to set auth token: %w", err)
	}
	return m.Save()
}

// ConfigPath returns the actual (expanded) path to the config file.
func (m *managerImpl) ConfigPath() string {
	return m.configPath
}

// SetBaseEndpoint sets the base endpoint in the config.
func (m *managerImpl) SetBaseEndpoint(endpoint string) error {
	if err := m.Manager.Set(context.Background(), ConfigKeyBaseEndpoint, endpoint); err != nil { //nolint:staticcheck // explicit to avoid recursion
		return fmt.Errorf("failed to set base endpoint: %w", err)
	}
	return m.Save()
}

// SetAPIEndpoint sets the API endpoint in the config (deprecated: use SetBaseEndpoint).
// Maintained for backward compatibility - maps to base endpoint.
func (m *managerImpl) SetAPIEndpoint(endpoint string) error {
	return m.SetBaseEndpoint(endpoint)
}

// SetMaxRetries sets the maximum retry count.
func (m *managerImpl) SetMaxRetries(retries int) error {
	if err := m.Manager.Set(context.Background(), ConfigKeyMaxRetries, retries); err != nil { //nolint:staticcheck // explicit to avoid recursion
		return fmt.Errorf("failed to set max retries: %w", err)
	}
	return m.Save()
}

// SetSecure sets the secure flag for HTTPS connections.
func (m *managerImpl) SetSecure(secure bool) error {
	if err := m.Manager.Set(context.Background(), ConfigKeySecure, secure); err != nil { //nolint:staticcheck // explicit to avoid recursion
		return fmt.Errorf("failed to set secure: %w", err)
	}
	return m.Save()
}

// RequireAuthenticated returns an error if not authenticated, otherwise nil.
func (m *managerImpl) RequireAuthenticated() error {
	if !m.Config().IsAuthenticated() {
		return ErrNotAuthenticated
	}
	return nil
}

func (m *managerImpl) Reset() error {
	if err := os.Remove(m.configPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to reset config: %w", err)
	}
	return nil
}

// expandPath expands ~ to the user's home directory.
func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}
