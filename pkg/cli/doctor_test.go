package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

func TestDoctor(t *testing.T) {
	tests := []struct {
		name        string
		jsonOutput  bool
		setupMocks  func(*configmocks.MockManager)
		wantErr     bool
		errContains string
	}{
		{
			name:       "successful doctor command",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "pinner.xyz",
					AuthToken:    "test-token",
					MaxRetries:   3,
					MemoryLimit:  256,
				})
			},
			wantErr: false,
		},
		{
			name:       "successful doctor command with JSON output",
			jsonOutput: true,
			setupMocks: func(cfgMgr *configmocks.MockManager) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       false,
					BaseEndpoint: "api.example.com",
					AuthToken:    "",
					MaxRetries:   5,
					MemoryLimit:  512,
				})
			},
			wantErr: false,
		},
		{
			name:       "doctor with default endpoint",
			jsonOutput: false,
			setupMocks: func(cfgMgr *configmocks.MockManager) {
				cfgMgr.EXPECT().Config().Return(&config.Config{
					Secure:       true,
					BaseEndpoint: "",
					AuthToken:    "",
					MaxRetries:   3,
					MemoryLimit:  0,
				})
			},
			wantErr: false,
		},
		{
			name:        "config manager factory fails",
			jsonOutput:  false,
			setupMocks:  func(cfgMgr *configmocks.MockManager) {},
			wantErr:     true,
			errContains: "failed to create config manager",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr := configmocks.NewMockManager(t)
			output := NewOutputFormatter(tt.jsonOutput, false, false, false)

			var cfgMgrFactory ConfigManagerFactory
			if tt.name == "config manager factory fails" {
				cfgMgrFactory = func() (config.Manager, error) {
					return nil, errors.New("config error")
				}
			} else {
				cfgMgrFactory = func() (config.Manager, error) {
					return cfgMgr, nil
				}
			}

			if tt.setupMocks != nil {
				tt.setupMocks(cfgMgr)
			}

			cmd := &cli.Command{}
			if tt.jsonOutput {
				cmd.Flags = []cli.Flag{
					&cli.BoolFlag{
						Name:  "json",
						Value: true,
					},
				}
			}

			err := doctor(context.Background(), cmd, output, cfgMgrFactory)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetEndpointDisplay(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected string
	}{
		{
			name: "custom endpoint with HTTPS",
			cfg: &config.Config{
				Secure:       true,
				BaseEndpoint: "api.example.com",
			},
			expected: "https://api.example.com",
		},
		{
			name: "custom endpoint with HTTP",
			cfg: &config.Config{
				Secure:       false,
				BaseEndpoint: "api.example.com",
			},
			expected: "http://api.example.com",
		},
		{
			name: "default endpoint with HTTPS",
			cfg: &config.Config{
				Secure:       true,
				BaseEndpoint: "",
			},
			expected: "https://pinner.xyz",
		},
		{
			name: "default endpoint with HTTP",
			cfg: &config.Config{
				Secure:       false,
				BaseEndpoint: "",
			},
			expected: "http://pinner.xyz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEndpointDisplay(tt.cfg)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatMemoryLimit(t *testing.T) {
	tests := []struct {
		name     string
		limit    uint64
		expected string
	}{
		{
			name:     "custom memory limit",
			limit:    256,
			expected: "256 MB",
		},
		{
			name:     "zero memory limit (default)",
			limit:    0,
			expected: "100 (default)",
		},
		{
			name:     "large memory limit",
			limit:    1024,
			expected: "1024 MB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatMemoryLimit(tt.limit)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestExpandPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantErr  bool
		contains string
	}{
		{
			name:     "expand tilde",
			path:     "~/test",
			wantErr:  false,
			contains: "test",
		},
		{
			name:     "absolute path",
			path:     "/tmp/test",
			wantErr:  false,
			contains: "/tmp/test",
		},
		{
			name:     "relative path",
			path:     "test/path",
			wantErr:  false,
			contains: "test/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := expandPath(tt.path)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.contains != "" {
					require.Contains(t, result, tt.contains)
				}
			}
		})
	}
}

func TestBashCompletionDetector(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		skipIfNotUnix  bool
		wantConfigured bool
	}{
		{
			name:           "no completion",
			content:        "# .bashrc\nexport PATH=$PATH:/usr/bin",
			skipIfNotUnix:  true,
			wantConfigured: false,
		},
		{
			name:           "bash completion with source",
			content:        "# .bashrc\nsource <(pinner completion bash)",
			skipIfNotUnix:  true,
			wantConfigured: true,
		},
		{
			name:           "bash completion with generic source",
			content:        "# .bashrc\nsource <(pinner completion)",
			skipIfNotUnix:  true,
			wantConfigured: true,
		},
		{
			name:           "bash completion direct call",
			content:        "# .bashrc\npinner completion bash",
			skipIfNotUnix:  true,
			wantConfigured: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip shell tests that don't match the current OS
			if tt.skipIfNotUnix && runtime.GOOS == "windows" {
				t.Skip("Skipping Unix-specific test on Windows")
			}

			tmpDir := t.TempDir()
			bashrcPath := filepath.Join(tmpDir, ".bashrc")
			err := os.WriteFile(bashrcPath, []byte(tt.content), 0644)
			require.NoError(t, err)

			detector := &BashCompletionDetector{homeDir: tmpDir}
			configured, err := detector.IsConfigured()
			require.NoError(t, err)
			require.Equal(t, tt.wantConfigured, configured)
		})
	}
}

func TestZshCompletionDetector(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		skipIfNotUnix  bool
		wantConfigured bool
	}{
		{
			name:           "no completion",
			content:        "# .zshrc\nexport PATH=$PATH:/usr/bin",
			skipIfNotUnix:  true,
			wantConfigured: false,
		},
		{
			name:           "zsh completion with source",
			content:        "# .zshrc\nsource <(pinner completion zsh)",
			skipIfNotUnix:  true,
			wantConfigured: true,
		},
		{
			name:           "zsh completion direct call",
			content:        "# .zshrc\npinner completion zsh",
			skipIfNotUnix:  true,
			wantConfigured: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip shell tests that don't match the current OS
			if tt.skipIfNotUnix && runtime.GOOS == "windows" {
				t.Skip("Skipping Unix-specific test on Windows")
			}

			tmpDir := t.TempDir()
			zshrcPath := filepath.Join(tmpDir, ".zshrc")
			err := os.WriteFile(zshrcPath, []byte(tt.content), 0644)
			require.NoError(t, err)

			detector := &ZshCompletionDetector{homeDir: tmpDir}
			configured, err := detector.IsConfigured()
			require.NoError(t, err)
			require.Equal(t, tt.wantConfigured, configured)
		})
	}
}

func TestFishCompletionDetector(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		skipIfNotUnix  bool
		wantConfigured bool
	}{
		{
			name:           "no completion file",
			skipIfNotUnix:  true,
			wantConfigured: false,
		},
		{
			name:           "fish completion file exists with pinner",
			content:        "# pinner completion\ncomplete -c pinner -f",
			skipIfNotUnix:  true,
			wantConfigured: true,
		},
		{
			name:           "fish completion file exists without pinner",
			content:        "# other completion\ncomplete -c git -f",
			skipIfNotUnix:  true,
			wantConfigured: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip shell tests that don't match the current OS
			if tt.skipIfNotUnix && runtime.GOOS == "windows" {
				t.Skip("Skipping Unix-specific test on Windows")
			}

			tmpDir := t.TempDir()
			fishDir := filepath.Join(tmpDir, ".config", "fish", "completions")
			err := os.MkdirAll(fishDir, 0755)
			require.NoError(t, err)

			if tt.content != "" {
				fishPath := filepath.Join(fishDir, "pinner.fish")
				err := os.WriteFile(fishPath, []byte(tt.content), 0644)
				require.NoError(t, err)
			}

			detector := &FishCompletionDetector{homeDir: tmpDir}
			configured, err := detector.IsConfigured()
			require.NoError(t, err)
			require.Equal(t, tt.wantConfigured, configured)
		})
	}
}

func TestPowerShellCompletionDetector(t *testing.T) {
	tests := []struct {
		name             string
		content          string
		profile          string
		skipIfNotWindows bool
		wantConfigured   bool
	}{
		{
			name:             "not on windows",
			skipIfNotWindows: true,
			wantConfigured:   false,
		},
		{
			name:             "no completion profile",
			skipIfNotWindows: true,
			wantConfigured:   false,
		},
		{
			name:             "profile with completion",
			profile:          "/tmp/profile.ps1",
			content:          "# PowerShell profile\npinner completion pwsh | Out-File -Append $PROFILE",
			skipIfNotWindows: true,
			wantConfigured:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip tests that require Windows if not on Windows
			if tt.skipIfNotWindows {
				t.Skip("Skipping Windows-specific test on non-Windows platform")
			}

			// Clean up PROFILE env
			oldProfile := os.Getenv("PROFILE")
			defer os.Setenv("PROFILE", oldProfile)

			if tt.profile != "" {
				os.Setenv("PROFILE", tt.profile)
				err := os.WriteFile(tt.profile, []byte(tt.content), 0644)
				require.NoError(t, err)
				defer os.Remove(tt.profile)
			} else {
				os.Unsetenv("PROFILE")
			}

			detector := &PowerShellCompletionDetector{}
			configured, err := detector.IsConfigured()
			require.NoError(t, err)
			require.Equal(t, tt.wantConfigured, configured)
		})
	}
}

func TestCompletionDetectorFactory(t *testing.T) {
	t.Run("get detectors on non-windows", func(t *testing.T) {
		factory, err := NewCompletionDetectorFactory()
		require.NoError(t, err)

		detectors := factory.GetDetectors()

		if runtime.GOOS == "windows" {
			// Windows: bash, zsh, fish, pwsh
			require.Len(t, detectors, 4)
		} else {
			// Unix: bash, zsh, fish (no pwsh)
			require.Len(t, detectors, 3)
		}

		names := make([]string, len(detectors))
		for i, d := range detectors {
			names[i] = d.Name()
		}
		require.Contains(t, names, "bash")
		require.Contains(t, names, "zsh")
		require.Contains(t, names, "fish")

		if runtime.GOOS == "windows" {
			require.Contains(t, names, "pwsh")
		} else {
			require.NotContains(t, names, "pwsh")
		}
	})
}

func TestCheckCompletion(t *testing.T) {
	t.Run("no completion configured", func(t *testing.T) {
		// Skip Unix-specific tests on Windows
		if runtime.GOOS == "windows" {
			t.Skip("Skipping Unix-specific test on Windows")
		}

		tmpDir := t.TempDir()

		// Set home to temp dir
		oldHome := os.Getenv("HOME")
		defer os.Setenv("HOME", oldHome)
		os.Setenv("HOME", tmpDir)

		info := checkCompletion()
		require.False(t, info.Enabled)
		require.Empty(t, info.Configured)
	})

	t.Run("bash completion configured", func(t *testing.T) {
		// Skip Unix-specific tests on Windows
		if runtime.GOOS == "windows" {
			t.Skip("Skipping Unix-specific test on Windows")
		}

		tmpDir := t.TempDir()

		// Create bashrc with completion
		bashrcPath := filepath.Join(tmpDir, ".bashrc")
		err := os.WriteFile(bashrcPath, []byte("source <(pinner completion bash)"), 0644)
		require.NoError(t, err)

		// Set home to temp dir
		oldHome := os.Getenv("HOME")
		defer os.Setenv("HOME", oldHome)
		os.Setenv("HOME", tmpDir)

		info := checkCompletion()
		require.True(t, info.Enabled)
		require.Contains(t, info.Configured, "bash")
	})

	t.Run("multiple completions configured", func(t *testing.T) {
		// Skip Unix-specific tests on Windows
		if runtime.GOOS == "windows" {
			t.Skip("Skipping Unix-specific test on Windows")
		}

		tmpDir := t.TempDir()

		// Create bashrc with completion
		bashrcPath := filepath.Join(tmpDir, ".bashrc")
		err := os.WriteFile(bashrcPath, []byte("source <(pinner completion bash)"), 0644)
		require.NoError(t, err)

		// Create zshrc with completion
		zshrcPath := filepath.Join(tmpDir, ".zshrc")
		err = os.WriteFile(zshrcPath, []byte("source <(pinner completion zsh)"), 0644)
		require.NoError(t, err)

		// Set home to temp dir
		oldHome := os.Getenv("HOME")
		defer os.Setenv("HOME", oldHome)
		os.Setenv("HOME", tmpDir)

		info := checkCompletion()
		require.True(t, info.Enabled)
		require.Contains(t, info.Configured, "bash")
		require.Contains(t, info.Configured, "zsh")
	})
}
