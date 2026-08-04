package vault

import (
	"crypto/rand"
	"encoding/hex"
	"testing"

	"go.sia.tech/core/types"
)

func TestVaultID_DerivationFormat(t *testing.T) {
	// Generate a real ed25519 private key (64 bytes)
	seed := make([]byte, 32)
	rand.Read(seed)
	privKey := types.NewPrivateKeyFromSeed(seed)
	appKeyHex := hex.EncodeToString(privKey)

	id := VaultID(appKeyHex)
	if len(id) <= len("vault:") {
		t.Fatalf("VaultID too short: %q", id)
	}
	if id[:6] != "vault:" {
		t.Fatalf("expected prefix vault:, got %q", id[:6])
	}
	// Should be vault: + 32 hex chars (16 bytes / 128 bits), enough entropy
	// that distinct vaults are not confused or falsely treated as already
	// configured in the restore dedup check.
	if len(id) != len("vault:")+32 {
		t.Fatalf("expected length %d, got %d (id=%q)", len("vault:")+32, len(id), id)
	}
}

func TestVaultID_InvalidKey(t *testing.T) {
	id := VaultID("not-hex")
	if id != "vault:unknown" {
		t.Fatalf("expected vault:unknown, got %q", id)
	}
}

func TestVaultID_ShortKey(t *testing.T) {
	// 32 bytes (16 hex chars) — valid hex but too short for ed25519 private key
	id := VaultID("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4")
	if id != "vault:unknown" {
		t.Fatalf("expected vault:unknown for short key, got %q", id)
	}
}

func TestVaultID_ConsistentForSameKey(t *testing.T) {
	seed := make([]byte, 32)
	rand.Read(seed)
	privKey := types.NewPrivateKeyFromSeed(seed)
	appKeyHex := hex.EncodeToString(privKey)

	id1 := VaultID(appKeyHex)
	id2 := VaultID(appKeyHex)
	if id1 != id2 {
		t.Fatalf("same key produced different IDs: %q vs %q", id1, id2)
	}
}

// TestVaultID_DistinctKeysDistinct verifies two distinct vault keys yield
// distinct VaultIDs. With a truncated 48-bit hash, a collision could falsely
// trigger the restore dedup check ('this vault is already configured').
func TestVaultID_DistinctKeysDistinct(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		seed, err := randomSeed()
		if err != nil {
			t.Fatalf("randomSeed: %v", err)
		}
		privKey := types.NewPrivateKeyFromSeed(seed)
		id := VaultID(hex.EncodeToString(privKey))
		if id == "vault:unknown" {
			t.Fatal("VaultID returned unknown for a valid key")
		}
		if seen[id] {
			t.Fatalf("VaultID collision across 50 distinct keys at %q — truncated ID too weak", id)
		}
		seen[id] = true
	}
}

func randomSeed() ([]byte, error) {
	seed := make([]byte, 32)
	_, err := rand.Read(seed)
	return seed, err
}

// TestProfileVaultID_DerivedFromAppKey verifies ProfileVaultID re-derives the
// current-format VaultID from the profile's stored app key, independent of any
// VaultID string persisted in the registry. This guards the dedup guard against
// a registry written by a pre-widening binary: a stored 48-bit VaultID must not
// let a previously-configured vault escape the 'already configured' check.
func TestProfileVaultID_DerivedFromAppKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("XDG_CACHE_HOME", home)

	seed, err := randomSeed()
	if err != nil {
		t.Fatalf("randomSeed: %v", err)
	}
	privKey := types.NewPrivateKeyFromSeed(seed)
	appKeyHex := hex.EncodeToString(privKey)
	want := VaultID(appKeyHex)

	// Persist the profile state with the app key.
	if err := SaveProfileState("migrated", &ProfileState{AppKey: appKeyHex}); err != nil {
		t.Fatalf("SaveProfileState: %v", err)
	}

	got, ok := ProfileVaultID("migrated")
	if !ok {
		t.Fatal("ProfileVaultID returned ok=false for a profile with a valid app key")
	}
	if got != want {
		t.Errorf("ProfileVaultID = %q, want %q (must be derived from app key, not a persisted string)", got, want)
	}
}

// TestProfileVaultID_NoAppKey verifies ProfileVaultID returns ok=false for a
// profile with no readable app key state (pending/not-yet-restored profile).
func TestProfileVaultID_NoAppKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("XDG_CACHE_HOME", home)

	if _, ok := ProfileVaultID("absent"); ok {
		t.Error("ProfileVaultID for a profile with no state should return ok=false")
	}
}
