package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
)

// TestVaultLogout_Locks uses the vaultServiceFactory-independent logout flow:
// it resolves the profile and acknowledges the lock without touching the
// remote. Verifies human output and that the profile/credential survive.
func TestVaultLogout_Locks(t *testing.T) {
	home, err := os.MkdirTemp("", "vault-logout-lock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	overrideHome(t, home)

	if err := vault.SaveRegistry(&vault.VaultRegistry{
		Default: "personal",
		Profiles: map[string]vault.ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
		},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	// Seed a device credential so we can prove logout leaves it intact.
	if err := vault.SaveProfileState("personal", &vault.ProfileState{
		AppKey:    "aabbcc",
		DeviceID:  "dev-1",
		CreatedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed profile state: %v", err)
	}

	root := NewRootCommand()
	var buf bytes.Buffer
	root.Writer = &buf
	if err := root.Run(context.Background(), []string{"pinner", "vault", "logout"}); err != nil {
		t.Fatalf("vault logout failed: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("locked")) {
		t.Fatalf("expected locked acknowledgment, got:\n%s", buf.String())
	}
	// Credentials must survive logout: resolve the same profile and confirm its
	// app key is still intact in profile state.
	state, err := vault.LoadProfileState("personal")
	if err != nil {
		t.Fatalf("profile state must survive logout: %v", err)
	}
	if state.AppKey == "" {
		t.Fatalf("logout must not clear the profile's saved app key")
	}
}

// TestVaultLogout_MissingProfile verifies logout fails loudly for a profile
// that does not exist rather than silently acknowledging anything.
func TestVaultLogout_MissingProfile(t *testing.T) {
	home, err := os.MkdirTemp("", "vault-logout-missing")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	overrideHome(t, home)

	if err := vault.SaveRegistry(&vault.VaultRegistry{
		Profiles: map[string]vault.ProfileConfig{"personal": {VaultID: "vault:aaa"}},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	root := NewRootCommand()
	var buf bytes.Buffer
	root.Writer = &buf
	err = root.Run(context.Background(), []string{"pinner", "vault", "logout", "--profile", "nope"})
	if err == nil {
		t.Fatalf("expected logout to fail for a missing profile")
	}
}

// TestVaultLogout_JSON verifies the JSON form of logout output.
func TestVaultLogout_JSON(t *testing.T) {
	home, err := os.MkdirTemp("", "vault-logout-json")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	overrideHome(t, home)

	if err := vault.SaveRegistry(&vault.VaultRegistry{
		Default: "personal",
		Profiles: map[string]vault.ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
		},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	root := NewRootCommand()
	var buf bytes.Buffer
	root.Writer = &buf
	if err := root.Run(context.Background(), []string{"pinner", "vault", "logout", "--json"}); err != nil {
		t.Fatalf("vault logout --json failed: %v", err)
	}
	var out struct {
		Profile string `json:"profile"`
		State   string `json:"state"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	if out.State != "locked" || out.Profile != "personal" {
		t.Fatalf("unexpected logout JSON: %+v", out)
	}
}
