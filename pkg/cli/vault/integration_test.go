//go:build integration

package vault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestVaultRoundTrip tests a full vault lifecycle against a live Sia indexer.
// Requires SIA_INDEXER_URL and SIA_APP_KEY environment variables.
//
// The test creates a temporary profile, writes the app key to its state.json,
// then exercises Put/List/Stat/Cat/Verify/Share/Remove/Sync.
func TestVaultRoundTrip(t *testing.T) {
	indexerURL := os.Getenv("SIA_INDEXER_URL")
	appKey := os.Getenv("SIA_APP_KEY")
	if indexerURL == "" || appKey == "" {
		t.Skip("SIA_INDEXER_URL and SIA_APP_KEY required for integration test")
	}

	// Use a temp home dir so profile paths don't collide with real config
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpHome, ".cache"))

	profileName := "test-integration"

	// Write profile state so NewVaultServiceForProfile can load it
	state := &ProfileState{
		AppKey:    appKey,
		DeviceID:  "test-device-id",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := SaveProfileState(profileName, state); err != nil {
		t.Fatalf("SaveProfileState failed: %v", err)
	}

	// Register the profile in the registry
	reg := &VaultRegistry{
		Default: profileName,
		Profiles: map[string]ProfileConfig{
			profileName: {
				VaultID:   VaultID(appKey),
				CachePath: ProfileDBPath(profileName),
				AppKeyRef: ProfileStatePath(profileName),
				DeviceName: "test-runner",
			},
		},
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	svc, err := NewVaultServiceForProfile(profileName, indexerURL)
	if err != nil {
		t.Fatalf("NewVaultServiceForProfile failed: %v", err)
	}
	defer svc.Close()

	ctx := context.Background()
	testData := []byte("Hello, vault! This is a test file.")
	testDigest := sha256.Sum256(testData)
	expectedDigest := hex.EncodeToString(testDigest[:])

	// 1. Put
	t.Run("put", func(t *testing.T) {
		reader := bytes.NewReader(testData)
		record, err := svc.Put(ctx, reader, int64(len(testData)), "vault:/test-roundtrip.txt", nil)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
		if record.ContentDigest != expectedDigest {
			t.Errorf("ContentDigest = %q, want %q", record.ContentDigest, expectedDigest)
		}
		if record.ObjectKey == "" {
			t.Error("expected non-empty ObjectKey")
		}
		if record.Size != int64(len(testData)) {
			t.Errorf("Size = %d, want %d", record.Size, len(testData))
		}
	})

	// 2. List
	t.Run("ls", func(t *testing.T) {
		items, err := svc.List(ctx, "vault:/")
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		found := false
		for _, item := range items {
			if item.Name == "test-roundtrip.txt" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected to find test-roundtrip.txt in listing")
		}
	})

	// 3. Stat
	t.Run("stat", func(t *testing.T) {
		result, err := svc.Stat(ctx, "vault:/test-roundtrip.txt")
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if result.Name != "test-roundtrip.txt" {
			t.Errorf("Name = %q, want %q", result.Name, "test-roundtrip.txt")
		}
		if result.Size != int64(len(testData)) {
			t.Errorf("Size = %d, want %d", result.Size, len(testData))
		}
	})

	// 4. Cat
	t.Run("cat", func(t *testing.T) {
		var buf bytes.Buffer
		if err := svc.Cat(ctx, "vault:/test-roundtrip.txt", &buf); err != nil {
			t.Fatalf("Cat failed: %v", err)
		}
		if !bytes.Equal(buf.Bytes(), testData) {
			t.Errorf("content mismatch: got %q, want %q", buf.String(), string(testData))
		}
	})

	// 5. Verify
	t.Run("verify", func(t *testing.T) {
		result, err := svc.Verify(ctx, "vault:/test-roundtrip.txt")
		if err != nil {
			t.Fatalf("Verify failed: %v", err)
		}
		if !result.DigestMatch {
			t.Error("expected DigestMatch=true")
		}
		if !result.ObjectExists {
			t.Error("expected ObjectExists=true")
		}
	})

	// 6. Share
	t.Run("share", func(t *testing.T) {
		validUntil := time.Now().Add(24 * time.Hour)
		shareURL, err := svc.Share(ctx, "vault:/test-roundtrip.txt", validUntil)
		if err != nil {
			t.Fatalf("Share failed: %v", err)
		}
		if shareURL == "" {
			t.Error("expected non-empty share URL")
		}
	})

	// 7. Remove
	t.Run("rm", func(t *testing.T) {
		if err := svc.Remove(ctx, "vault:/test-roundtrip.txt"); err != nil {
			t.Fatalf("Remove failed: %v", err)
		}
		// Verify it's gone
		_, err := svc.Stat(ctx, "vault:/test-roundtrip.txt")
		if err == nil {
			t.Error("expected error after Remove")
		}
	})

	// 8. Sync
	t.Run("sync", func(t *testing.T) {
		count, _, err := svc.Sync(ctx)
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}
		_ = count // may be 0 if nothing changed
	})
}
