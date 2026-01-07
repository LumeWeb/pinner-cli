package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.lumeweb.com/configmanager"
	"go.lumeweb.com/configmanager/source"
)

var (
	// ErrNotAuthenticated is returned when an operation requires authentication.
	ErrNotAuthenticated = errors.New("not authenticated: no auth token configured")

	// DefaultConfigPath is the default location for the config file.
	DefaultConfigPath = "~/.config/lume/pinner.yaml"
)

const (
	// ConfigKey is the key used for the main config namespace.
	ConfigKey = "pinner"
)

// Manager extends configmanager.Manager with CLI-specific configuration methods.
type Manager interface {
	configmanager.Manager
	Config() *Config
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

	_, err = os.Stat(configPath)
	if err == nil {
		fileSource := source.NewFileSource(configPath)
		cm.RegisterSource(fileSource)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to check config file: %w", err)
	}

	defaultSource := source.NewDefaultConfigSource(cm)
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
	cfg := &Config{}
	_, _, err := m.Manager.Get(ConfigKey, cfg)
	if err != nil {
		return NewConfig()
	}
	return cfg
}

// Load loads configuration from all registered sources.
func (m *managerImpl) Load() error {
	return m.Manager.Load()
}

// Save persists the current configuration to disk.
func (m *managerImpl) Save() error {
	return m.Manager.Persist(ConfigKey)
}

// SetAuthToken sets the authentication token in the config.
func (m *managerImpl) SetAuthToken(token string) error {
	if err := m.Manager.Set(context.Background(), ConfigKeyAuthToken, token); err != nil {
		return fmt.Errorf("failed to set auth token: %w", err)
	}
	return m.Save()
}

// SetBaseEndpoint sets the base endpoint in the config.
func (m *managerImpl) SetBaseEndpoint(endpoint string) error {
	if err := m.Manager.Set(context.Background(), ConfigKeyBaseEndpoint, endpoint); err != nil {
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
	if err := m.Manager.Set(context.Background(), ConfigKeyMaxRetries, retries); err != nil {
		return fmt.Errorf("failed to set max retries: %w", err)
	}
	return m.Save()
}

// SetSecure sets the secure flag for HTTPS connections.
func (m *managerImpl) SetSecure(secure bool) error {
	if err := m.Manager.Set(context.Background(), ConfigKeySecure, secure); err != nil {
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
