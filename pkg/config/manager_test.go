package config

import (
	"os"
	"path/filepath"
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
