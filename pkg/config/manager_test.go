package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewManager_AlwaysRegistersFileSource(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Verify config file does NOT exist initially
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("Config file should not exist initially")
	}

	// Create a manager when the config file does NOT exist
	mgr, err := NewManager(configPath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Set an auth token
	testToken := "test-auth-token-12345"
	if err := mgr.SetAuthToken(testToken); err != nil {
		t.Fatalf("Failed to set auth token: %v", err)
	}

	// Verify the file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Config file was not created after SetAuthToken - file source was not registered")
	}

	// Create a NEW manager instance to test loading from file
	newMgr, err := NewManager(configPath)
	if err != nil {
		t.Fatalf("Failed to create new manager: %v", err)
	}

	// Load the config from file
	if err := newMgr.Load(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if newMgr.Config().AuthToken != testToken {
		t.Errorf("Expected auth token %q, got %q - token was not persisted", testToken, newMgr.Config().AuthToken)
	}
}

func TestNewManager_RespectsCustomConfigPath(t *testing.T) {
	// Test that the new config path format works: ~/.config/pinner/config.yaml
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "pinner", "config.yaml")

	mgr, err := NewManager(configPath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Verify the directory structure is created
	if _, err := os.Stat(filepath.Dir(configPath)); os.IsNotExist(err) {
		t.Error("Config directory was not created")
	}

	// Set and verify token
	testToken := "custom-path-token"
	if err := mgr.SetAuthToken(testToken); err != nil {
		t.Fatalf("Failed to set auth token: %v", err)
	}

	// Verify persistence
	newMgr, err := NewManager(configPath)
	if err != nil {
		t.Fatalf("Failed to create new manager: %v", err)
	}

	// Load the config from file
	if err := newMgr.Load(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if newMgr.Config().AuthToken != testToken {
		t.Errorf("Expected auth token %q, got %q", testToken, newMgr.Config().AuthToken)
	}
}

// TestLoad_MissingConfigFile_NoError verifies that Load() does NOT return an
// error when the config file doesn't exist yet. This is the first-run path
// that "pinner setup" hits. On Linux the file-not-found error string is
// "no such file or directory"; on Windows it is "The system cannot find the
// file specified." The old implementation only matched the Linux string and
// used os.IsNotExist which doesn't work through fmt.Errorf("%w") wrapping.
func TestLoad_MissingConfigFile_NoError(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "subdir", "config.yaml")

	mgr, err := NewManager(configPath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// File should NOT exist at this point
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("Config file should not exist before Load")
	}

	// Load must succeed with defaults; this is what pinner setup hits
	if err := mgr.Load(); err != nil {
		t.Fatalf("Load should not fail when config file is missing, got: %v", err)
	}

	// Config should be usable with defaults
	cfg := mgr.Config()
	if cfg == nil {
		t.Fatal("Config should not be nil after Load with missing file")
	}
}

// TestIsFileNotFoundError_DetectsWrappedAndPlatformErrors ensures the helper
// correctly identifies file-not-found errors through the full wrapping chain
// produced by the configmanager library, and for both Linux and Windows
// error strings.
func TestIsFileNotFoundError_DetectsWrappedAndPlatformErrors(t *testing.T) {
	raw, err := os.Open("/nonexistent/file/that/does/not/exist.yaml")
	if err == nil {
		_ = raw.Close()
		t.Fatal("Expected error opening non-existent file")
	}

	// The configmanager library wraps the error twice with %w:
	//   "failed to load from source: <err>"
	//   "failed to load config from source *source.fileSource: <err>"
	wrappedOnce := fmt.Errorf("failed to load from source: %w", err)
	wrappedTwice := fmt.Errorf("failed to load config from source *source.fileSource: %w", wrappedOnce)

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"raw os error", err, true},
		{"wrapped once", wrappedOnce, true},
		{"wrapped twice (configmanager chain)", wrappedTwice, true},
		{"unrelated error", fmt.Errorf("something else entirely"), false},
		{"windows-style string", fmt.Errorf("open C:/Users/Michal/.config/pinner/config.yaml: The system cannot find the file specified."), true},
		{"linux-style string", fmt.Errorf("open /home/test/.config/pinner/config.yaml: no such file or directory"), true},
		{"windows-style string unwrapped", fmt.Errorf("The system cannot find the file specified"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isFileNotFoundError(tt.err)
			if got != tt.want {
				t.Errorf("isFileNotFoundError() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestResolveDefaultConfigPath verifies the cross-platform config path resolution.
func TestResolveDefaultConfigPath(t *testing.T) {
	t.Run("returns non-empty path", func(t *testing.T) {
		p := resolveDefaultConfigPath()
		if p == "" {
			t.Fatal("expected non-empty config path")
		}
		if filepath.Base(p) != "config.yaml" {
			t.Fatalf("expected base name config.yaml, got %q", filepath.Base(p))
		}
	})

	t.Run("path contains pinner segment", func(t *testing.T) {
		p := resolveDefaultConfigPath()
		dir := filepath.Dir(p)
		if filepath.Base(dir) != "pinner" {
			t.Fatalf("expected parent dir to be 'pinner', got %q", filepath.Base(dir))
		}
	})

	t.Run("expandPath handles ~ prefix", func(t *testing.T) {
		// expandPath is still used for --config overrides with ~ prefix
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("cannot determine home dir")
		}
		got := expandPath("~/test/path")
		if got != filepath.Join(home, "/test/path") {
			t.Errorf("expandPath(~/test/path) = %q, want %q", got, filepath.Join(home, "/test/path"))
		}
	})

	t.Run("expandPath passes through non-tilde paths", func(t *testing.T) {
		got := expandPath("/absolute/path")
		if got != "/absolute/path" {
			t.Errorf("expandPath(/absolute/path) = %q, want /absolute/path", got)
		}
	})
}

// TestDefaultConfigPath_NoTilde verifies that DefaultConfigPath is already
// resolved (no ~ prefix) since it's computed at init time.
func TestDefaultConfigPath_NoTilde(t *testing.T) {
	if strings.HasPrefix(DefaultConfigPath, "~") {
		t.Fatalf("DefaultConfigPath should be resolved, got %q (starts with ~)", DefaultConfigPath)
	}
}
