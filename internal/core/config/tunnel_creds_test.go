package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTunnelCredentialKey(t *testing.T) {
	cases := []struct {
		provider string
		key      string
		want     string
	}{
		{"ngrok", "token", "tunnels.ngrok_token"},
		{"openai", "tunnel_id", "tunnels.openai_tunnel_id"},
		{"openai", "api_key", "tunnels.openai_api_key"},
		{"ngrok", "tunnel_id", ""},
		{"openai", "token", ""},
		{"bogus", "token", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := TunnelCredentialKey(c.provider, c.key); got != c.want {
			t.Errorf("TunnelCredentialKey(%q,%q) = %q, want %q", c.provider, c.key, got, c.want)
		}
	}
}

func TestTunnelCredentialPersists(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")

	mgr, err := NewManager(configPath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Unset reads back "" and Set then reads back the value.
	if got := mgr.TunnelCredential("ngrok", "token"); got != "" {
		t.Fatalf("expected empty ngrok token before set, got %q", got)
	}
	if err := mgr.SetTunnelCredential("ngrok", "token", "tok-abc"); err != nil {
		t.Fatalf("SetTunnelCredential(ngrok,token): %v", err)
	}
	if got := mgr.TunnelCredential("ngrok", "token"); got != "tok-abc" {
		t.Fatalf("TunnelCredential(ngrok,token) = %q, want tok-abc", got)
	}

	// OpenAI pair maps to its own key (independent namespace).
	if err := mgr.SetTunnelCredential("openai", "api_key", "sk-runtime"); err != nil {
		t.Fatalf("SetTunnelCredential(openai,api_key): %v", err)
	}
	if got := mgr.TunnelCredential("openai", "api_key"); got != "sk-runtime" {
		t.Fatalf("TunnelCredential(openai,api_key) = %q, want sk-runtime", got)
	}
	// ngrok token must be unaffected.
	if got := mgr.TunnelCredential("ngrok", "token"); got != "tok-abc" {
		t.Fatalf("ngrok token clobbered: %q", got)
	}

	// Unknown pair fails fast on Set and returns "" on Get.
	if err := mgr.SetTunnelCredential("bogus", "token", "x"); err == nil {
		t.Fatal("expected error for unknown (provider,key) pair")
	}
	if got := mgr.TunnelCredential("openai", "token"); got != "" {
		t.Fatalf("expected empty for unknown key, got %q", got)
	}

	// A fresh manager reloading the same file sees the persisted values.
	newMgr, err := NewManager(configPath)
	if err != nil {
		t.Fatalf("NewManager(reload): %v", err)
	}
	if err := newMgr.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := newMgr.TunnelCredential("ngrok", "token"); got != "tok-abc" {
		t.Fatalf("reloaded ngrok token = %q, want tok-abc", got)
	}
	if got := newMgr.TunnelCredential("openai", "api_key"); got != "sk-runtime" {
		t.Fatalf("reloaded openai key = %q, want sk-runtime", got)
	}
	// Fields are wired into the struct mapping too.
	if got := newMgr.Config().Tunnels.NgrokToken; got != "tok-abc" {
		t.Fatalf("Config().Tunnels.NgrokToken = %q, want tok-abc", got)
	}
}

func TestTunnelCredentialConfigFileNotWorldReadable(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	mgr, err := NewManager(configPath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := mgr.SetTunnelCredential("ngrok", "token", "secret"); err != nil {
		t.Fatalf("SetTunnelCredential: %v", err)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	// Windows has no Unix permission bits: os.Chmod(0600) only toggles the
	// read-only attribute and Stat reports 0666 there. Skip the strict mode
	// assertion on Windows; the 0600 invariant is enforced on Unix platforms.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Errorf("config file %q is group/world readable: %v", configPath, info.Mode().Perm())
	}
}

func TestTunnelCredentialLocksDownPreExistingWorldReadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not meaningful on Windows")
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	// Simulate a pre-existing file created under a broad umask (e.g. by an
	// older pinner version): world-readable and -writable.
	if err := os.WriteFile(configPath, []byte("auth_token: old\n"), 0o666); err != nil {
		t.Fatalf("write world-readable config: %v", err)
	}
	mgr, err := NewManager(configPath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := mgr.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Writing a tunnel credential must lock the file down to 0600.
	if err := mgr.SetTunnelCredential("ngrok", "token", "secret"); err != nil {
		t.Fatalf("SetTunnelCredential: %v", err)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	// The pre-existing auth_token must also survive the rewrite.
	if got := mgr.TunnelCredential("ngrok", "token"); got != "secret" {
		t.Fatalf("ngrok token readback = %q, want secret", got)
	}
	if perm := info.Mode().Perm(); perm&0077 != 0 {
		t.Errorf("config file still group/world accessible after SetTunnelCredential: %v", perm)
	}
	// A subsequent non-credential write (any other setter re-persists the file
	// via Save) must NOT revert perms to the umask default (0644) and re-expose
	// the secret: the 0600 lockdown is centralized in Save().
	if err := mgr.SetAuthToken("authtok"); err != nil {
		t.Fatalf("SetAuthToken: %v", err)
	}
	info, err = os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config after second Save: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0077 != 0 {
		t.Errorf("config file re-exposed group/world after subsequent Save: %v", perm)
	}
	// The stored credential must still read back after the unrelated rewrite.
	if got := mgr.TunnelCredential("ngrok", "token"); got != "secret" {
		t.Errorf("ngrok token readback after second Save = %q, want secret", got)
	}
}
