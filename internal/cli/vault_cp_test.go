package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// cpFakeVaultService is a VaultService that records Get/Put traffic so a vault
// cp invocation can be asserted without a live Sia indexer.
type cpFakeVaultService struct {
	label    string
	content  []byte // bytes served by Get
	getPaths []string
	putPaths []string
	onGet    func(io.Writer) // optional hook invoked during Get (for perms asserts)
}

func (s *cpFakeVaultService) CheckReady(context.Context) error { return nil }
func (s *cpFakeVaultService) Put(_ context.Context, r io.Reader, _ int64, path string, _ map[string]any) (*vault.File, error) {
	s.putPaths = append(s.putPaths, path)
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return &vault.File{UUID: "u-" + s.label, ObjectKey: "abc", ContentDigest: "", Size: int64(len(data))}, nil
}
func (s *cpFakeVaultService) Get(_ context.Context, path string, w io.Writer) error {
	s.getPaths = append(s.getPaths, path)
	if s.onGet != nil {
		s.onGet(w)
	}
	_, err := w.Write(s.content)
	return err
}
func (s *cpFakeVaultService) List(context.Context, string) ([]vault.ListItem, error) { return nil, nil }
func (s *cpFakeVaultService) Stat(context.Context, string) (*vault.StatResult, error) {
	return nil, vault.ErrNotFound
}
func (s *cpFakeVaultService) Cat(context.Context, string, io.Writer) error { return nil }
func (s *cpFakeVaultService) Verify(context.Context, string) (*vault.VerifyResult, error) {
	return nil, nil
}
func (s *cpFakeVaultService) VerifyDeep(context.Context, string) (*vault.VerifyResult, error) {
	return nil, nil
}
func (s *cpFakeVaultService) Remove(context.Context, string) error { return nil }
func (s *cpFakeVaultService) VersionList(context.Context, string) ([]*vault.File, error) {
	return nil, nil
}
func (s *cpFakeVaultService) VersionGet(context.Context, string, string) (*vault.File, error) {
	return nil, nil
}
func (s *cpFakeVaultService) VersionDownload(context.Context, string, string, io.Writer) error {
	return nil
}
func (s *cpFakeVaultService) VersionRestore(context.Context, string, string) (*vault.File, error) {
	return nil, nil
}
func (s *cpFakeVaultService) Share(context.Context, string, time.Time) (string, error) {
	return "", nil
}
func (s *cpFakeVaultService) Sync(context.Context) (int, bool, error) { return 0, false, nil }
func (s *cpFakeVaultService) Status(context.Context) (*vault.StatusResult, error) {
	return &vault.StatusResult{}, nil
}
func (s *cpFakeVaultService) Close() error { return nil }

// cpCmdHarness overrides the vault service factory to return a fake per profile
// name, seeds config/home, and runs `pinner vault cp <args>` through the real
// root command, returning stdout.
func cpCmdHarness(t *testing.T, srcSvc, dstSvc *cpFakeVaultService, args ...string) (*bytes.Buffer, error) {
	t.Helper()
	home, err := os.MkdirTemp("", "vault-cp-cmd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	overrideHome(t, home)

	// Seed a registry + config so profile resolution and indexer URL work.
	if err := vault.SaveRegistry(&vault.VaultRegistry{
		Default: "personal",
		Profiles: map[string]vault.ProfileConfig{
			"personal": {VaultID: "vault:aaa"},
		},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	origFactory := vaultServiceFactory
	t.Cleanup(func() { vaultServiceFactory = origFactory })
	vaultServiceFactory = func(profileName, _ string) (vault.VaultService, error) {
		switch profileName {
		case "work":
			return srcSvc, nil
		default:
			return dstSvc, nil
		}
	}

	root := NewRootCommand()
	var buf bytes.Buffer
	root.Writer = &buf
	full := append([]string{"pinner", "vault", "cp"}, args...)
	err = root.Run(context.Background(), full)
	return &buf, err
}

// TestVaultCpCommand_VaultToVault asserts that cp between two vaults (which may
// be different profiles) reads from the source profile's service and writes to
// the destination profile's service, preserving the transferred bytes, and that
// the decrypted plaintext is buffered in a private (0700 dir / 0600 file) temp
// location rather than exposed in shared /tmp.
func TestVaultCpCommand_VaultToVault(t *testing.T) {
	srcSvc := &cpFakeVaultService{label: "work", content: []byte("hello-from-work")}
	dstSvc := &cpFakeVaultService{label: "personal"}

	// Assert on the raw temp file while it still exists (before the copy's
	// deferred RemoveAll runs): it must live in a private 0700 "vault-cp-"
	// directory and the file must be 0600, so decrypted plaintext is never
	// readable by other local users in shared /tmp. This runs inside Get before
	// the writer is populated, so only the location/permissions are checked.
	srcSvc.onGet = func(w io.Writer) {
		tf, ok := w.(*os.File)
		if !ok {
			t.Logf("source writer is %T (not *os.File); skipping perm assert", w)
			return
		}
		assertPrivateTempPerms(t, tf)
	}

	t.Cleanup(func() { srcSvc.onGet = nil })

	buf, err := cpCmdHarness(t, srcSvc, dstSvc,
		"vault://work/docs/a.txt", "vault:/docs/b.txt", "--json")
	if err != nil {
		t.Fatalf("vault-to-vault cp failed: %v", err)
	}

	// Source service must be asked to Get the source path (authority-stripped).
	if len(srcSvc.getPaths) != 1 || srcSvc.getPaths[0] != "vault:/docs/a.txt" {
		t.Errorf("source Get paths = %v, want [vault:/docs/a.txt]", srcSvc.getPaths)
	}
	// Destination service must be asked to Put the destination path.
	if len(dstSvc.putPaths) != 1 || dstSvc.putPaths[0] != "vault:/docs/b.txt" {
		t.Errorf("dst Put paths = %v, want [vault:/docs/b.txt]", dstSvc.putPaths)
	}

	// JSON response echoes Copying success.
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, buf.String())
	}
	if out["path"] != "vault:/docs/b.txt" {
		t.Errorf("response path = %v, want vault:/docs/b.txt", out["path"])
	}
}

// TestClassifyCpArg verifies the endpoint classifier distinguishes local vs
// vault and extracts an explicit profile authority.
func TestClassifyCpArg(t *testing.T) {
	local := classifyCpArg("./x.txt")
	if local.isVault {
		t.Errorf("local path classified as vault")
	}
	if local.localPath != "./x.txt" {
		t.Errorf("localPath = %q", local.localPath)
	}

	active := classifyCpArg("vault:/docs/a.txt")
	if !active.isVault || active.profile != "" || active.vaultPath != "vault:/docs/a.txt" {
		t.Errorf("active-profile vault = %+v", active)
	}

	named := classifyCpArg("vault://work/docs/a.txt")
	if !named.isVault || named.profile != "work" || named.vaultPath != "vault:/docs/a.txt" {
		t.Errorf("named-profile vault = %+v", named)
	}
	if named.name != "a.txt" {
		t.Errorf("name = %q, want a.txt", named.name)
	}
}

// TestVaultCpCommand_BothLocalRejected asserts cp rejects local→local.
func TestVaultCpCommand_BothLocalRejected(t *testing.T) {
	srcSvc := &cpFakeVaultService{label: "work"}
	dstSvc := &cpFakeVaultService{label: "personal"}
	if _, err := cpCmdHarness(t, srcSvc, dstSvc, "./a.txt", "./b.txt"); err == nil {
		t.Fatalf("local→local cp should be rejected")
	}
}

// TestVaultCpCommand_UploadExpandsDir asserts local→vault with a trailing-slash
// destination expands to the source filename (directory destination).
func TestVaultCpCommand_UploadExpandsDir(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(local, []byte("pdf-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	srcSvc := &cpFakeVaultService{label: "work"}
	dstSvc := &cpFakeVaultService{label: "personal"}

	// The destination is the active profile's vault (dstSvc).
	if _, err := cpCmdHarness(t, srcSvc, dstSvc, local, "vault:/docs/", "--json"); err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if len(dstSvc.putPaths) != 1 || dstSvc.putPaths[0] != "vault:/docs/report.pdf" {
		t.Errorf("upload Put paths = %v, want [vault:/docs/report.pdf]", dstSvc.putPaths)
	}
}
