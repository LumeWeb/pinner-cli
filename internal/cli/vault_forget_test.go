//go:build !no_tunnel

package cli

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// TestVaultForget_RequiresProfile verifies forget fails loudly when no explicit
// --profile is given, even with exactly one profile configured; it must never
// auto-delete.
func TestVaultForget_RequiresProfile(t *testing.T) {
	home, err := os.MkdirTemp("", "vault-forget-noprofile")
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
	err = root.Run(context.Background(), []string{"pinner", "vault", "forget"})
	if err == nil {
		t.Fatalf("expected forget to require --profile")
	}
	// Nothing must have been removed.
	reg, lerr := vault.LoadRegistry()
	if lerr != nil {
		t.Fatalf("load registry: %v", lerr)
	}
	if _, ok := reg.Profiles["personal"]; !ok {
		t.Fatalf("profile must not be forgotten without an explicit --profile")
	}
}

// TestVaultForget_EndToEnd wires the real command and asserts the profile is
// removed from the registry and its data dir deleted, and JSON output reports
// the forgotten profile.
func TestVaultForget_EndToEnd(t *testing.T) {
	home, err := os.MkdirTemp("", "vault-forget-e2e")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	overrideHome(t, home)

	if err := vault.SaveRegistry(&vault.VaultRegistry{
		Default: "work",
		Profiles: map[string]vault.ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
			"work":     {VaultID: "vault:bbb"},
		},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	// Seed data for the profile being forgotten. The app key is a derived
	// test placeholder (hex of a non-secret string), not a hard-coded secret.
	if err := vault.SaveProfileState("work", &vault.ProfileState{
		AppKey:   hex.EncodeToString([]byte("test-work-not-a-secret")),
		DeviceID: "dev-1", CreatedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed profile state: %v", err)
	}

	root := NewRootCommand()
	var buf bytes.Buffer
	root.Writer = &buf
	if err := root.Run(context.Background(), []string{"pinner", "vault", "forget", "--profile", "work", "--force", "--json"}); err != nil {
		t.Fatalf("vault forget failed: %v", err)
	}

	var out struct {
		Profile string `json:"profile"`
		State   string `json:"state"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	if out.State != "forgotten" || out.Profile != "work" {
		t.Fatalf("unexpected forget JSON: %+v", out)
	}

	reg, lerr := vault.LoadRegistry()
	if lerr != nil {
		t.Fatalf("load registry: %v", lerr)
	}
	if _, ok := reg.Profiles["work"]; ok {
		t.Fatalf("profile 'work' still present after forget")
	}
	if _, ok := reg.Profiles["personal"]; !ok {
		t.Fatalf("profile 'personal' should be untouched")
	}
	// Default pointed at 'work'; must have been cleared.
	if reg.Default != "" {
		t.Fatalf("Default = %q after forgetting default, want empty", reg.Default)
	}
}
