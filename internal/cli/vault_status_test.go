package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"testing"
	"time"

	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// statusStubVaultService is a minimal VaultService whose Status returns a
// canned result, used to drive `vault status` command rendering without a
// live Sia indexer.
type statusStubVaultService struct {
	res *vault.StatusResult
}

func (s *statusStubVaultService) CheckReady(context.Context) error { return nil }
func (s *statusStubVaultService) Put(context.Context, io.Reader, int64, string, map[string]any) (*vault.File, error) {
	return nil, nil
}
func (s *statusStubVaultService) Get(context.Context, string, io.Writer) error { return nil }
func (s *statusStubVaultService) List(context.Context, string) ([]vault.ListItem, error) {
	return nil, nil
}
func (s *statusStubVaultService) Stat(context.Context, string) (*vault.StatResult, error) {
	return nil, nil
}
func (s *statusStubVaultService) Cat(context.Context, string, io.Writer) error { return nil }
func (s *statusStubVaultService) Verify(context.Context, string) (*vault.VerifyResult, error) {
	return nil, nil
}
func (s *statusStubVaultService) VerifyDeep(context.Context, string) (*vault.VerifyResult, error) {
	return nil, nil
}
func (s *statusStubVaultService) Remove(context.Context, string) error { return nil }
func (s *statusStubVaultService) VersionList(context.Context, string) ([]*vault.File, error) {
	return nil, nil
}
func (s *statusStubVaultService) VersionGet(context.Context, string, string) (*vault.File, error) {
	return nil, nil
}
func (s *statusStubVaultService) VersionDownload(context.Context, string, string, io.Writer) error {
	return nil
}
func (s *statusStubVaultService) VersionRestore(context.Context, string, string) (*vault.File, error) {
	return nil, nil
}
func (s *statusStubVaultService) Share(context.Context, string, time.Time) (string, error) {
	return "", nil
}
func (s *statusStubVaultService) Sync(context.Context) (int, bool, error) { return 0, false, nil }
func (s *statusStubVaultService) Status(context.Context) (*vault.StatusResult, error) {
	return s.res, nil
}
func (s *statusStubVaultService) Close() error { return nil }

// statusCmdHarness seeds an empty registry (so profile resolution succeeds),
// overrides the vault service factory with a stub, and runs the vault status
// command through the real root command, capturing output into buf.
func statusCmdHarness(t *testing.T, res *vault.StatusResult, args ...string) (*bytes.Buffer, error) {
	t.Helper()
	home, err := os.MkdirTemp("", "vault-status-cmd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	overrideHome(t, home)

	// Seed a registry with one profile set as default so ResolveProfile("")
	// succeeds (the stub service ignores the actual profile).
	if err := vault.SaveRegistry(&vault.VaultRegistry{
		Default: "personal",
		Profiles: map[string]vault.ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
		},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	orig := vaultServiceFactory
	t.Cleanup(func() { vaultServiceFactory = orig })
	vaultServiceFactory = func(profileName, indexerURL string) (vault.VaultService, error) {
		return &statusStubVaultService{res: res}, nil
	}

	root := NewRootCommand()
	var buf bytes.Buffer
	root.Writer = &buf
	full := append([]string{"pinner", "vault", "status"}, args...)
	err = root.Run(context.Background(), full)
	return &buf, err
}

// TestVaultStatusCommand_JSONRendering asserts JSON output carries live remote
// + local fields (never a fabricated reachable value).
func TestVaultStatusCommand_JSONRendering(t *testing.T) {
	buf, err := statusCmdHarness(t, &vault.StatusResult{
		Unlocked:         true,
		RemoteReachable:  true,
		RemoteReady:      true,
		StorageUsed:      4096,
		StorageLimit:     1 << 30,
		RemainingStorage: 1<<30 - 4096,
		CacheState:       "healthy",
		ObjectsIndexed:   3,
		TotalBytes:       300,
		LastSyncTime:     "2026-08-05T12:00:00Z",
	}, "--json")
	if err != nil {
		t.Fatalf("vault status --json failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, buf.String())
	}
	if out["remote_reachable"] != true {
		t.Fatalf("remote_reachable = %v, want true", out["remote_reachable"])
	}
	if out["storage_used"] != float64(4096) {
		t.Fatalf("storage_used = %v, want 4096", out["storage_used"])
	}
	if out["cache_state"] != "healthy" {
		t.Fatalf("cache_state = %v, want healthy", out["cache_state"])
	}
	if out["objects_indexed"] != float64(3) {
		t.Fatalf("objects_indexed = %v, want 3", out["objects_indexed"])
	}
}

// TestVaultStatusCommand_HumanUnreachable asserts an unreachable remote is
// rendered honestly (not derived from local state).
func TestVaultStatusCommand_HumanUnreachable(t *testing.T) {
	buf, err := statusCmdHarness(t, &vault.StatusResult{
		Unlocked:    true,
		CacheState:  "missing",
		RemoteError: "connection refused",
	})
	if err != nil {
		t.Fatalf("vault status failed: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("unreachable")) {
		t.Fatalf("human output should report unreachable:\n%s", buf.String())
	}
	// The remote value must be "unreachable[: ...]", never a bare fabricated
	// "reachable" (which would appear as a value not preceded by "un").
	if regexp.MustCompile(`Remote\s+reachable\s`).MatchString(buf.String()) {
		t.Fatalf("output must not fabricate a reachable value:\n%s", buf.String())
	}
}
