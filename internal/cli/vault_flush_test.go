//go:build !no_tunnel

package cli

import (
	"bytes"
	"context"
	"os"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// flushCmdStub is a scripted VaultService for exercising the CLI `vault flush`
// single-path branch. stat returns the next status from statSeq per call;
// flushNoop controls whether FlushPath performs no durable work (the crashed
// "uploaded"-with-gone-staged-buffer case). Everything else inherits the
// NopVaultService no-ops.
type flushCmdStub struct {
	NopVaultService
	statSeq    []string // statuses returned by successive Stat calls
	flushNoop  bool     // FlushPath does no work
	flushPings int
	statCalls  int
}

func (f *flushCmdStub) Stat(_ context.Context, _ string) (*vault.StatResult, error) {
	status := "pending"
	if f.statCalls < len(f.statSeq) {
		status = f.statSeq[f.statCalls]
	}
	f.statCalls++
	return &vault.StatResult{Status: status}, nil
}

func (f *flushCmdStub) FlushPath(context.Context, string) error {
	f.flushPings++
	return nil
}

// TestVaultFlush_PathDoesNotClaimFlushOnNoop locks in that a single-path
// `vault flush` never reports flushed:1 / "Flushed <path>" for a flush that
// performed no durable work: an already-durable file (pre-check) and a file
// whose FlushPath no-ops (empty staged buffer despite a non-ok status) must
// both report "nothing to flush", not a fake flushed count.
func TestVaultFlush_PathDoesNotClaimFlushOnNoop(t *testing.T) {
	cases := []struct {
		name      string
		statSeq   []string
		flushNoop bool
		wantFlush bool // whether the run should report a real flush
	}{
		{name: "already durable", statSeq: []string{vault.FileStatusDurable}, wantFlush: false},
		{name: "flush no-ops (empty staged buffer)", statSeq: []string{vault.FileStatusStaged, vault.FileStatusStaged}, flushNoop: true, wantFlush: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home, err := os.MkdirTemp("", "vault-flush-path")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(home) })
			overrideHome(t, home)

			if err := vault.SaveRegistry(&vault.VaultRegistry{
				Default:  "personal",
				Profiles: map[string]vault.ProfileConfig{"personal": {VaultID: "vault:aaa"}},
			}); err != nil {
				t.Fatalf("seed registry: %v", err)
			}

			orig := vaultServiceFactory
			t.Cleanup(func() { vaultServiceFactory = orig })
			stub := &flushCmdStub{statSeq: tc.statSeq, flushNoop: tc.flushNoop}
			vaultServiceFactory = func(profileName, indexerURL string) (vault.VaultService, error) {
				return stub, nil
			}

			root := NewRootCommand()
			var buf bytes.Buffer
			root.Writer = &buf
			if err := root.Run(context.Background(), []string{"pinner", "vault", "flush", "vault:/a.txt"}); err != nil {
				t.Fatalf("vault flush failed: %v", err)
			}

			out := buf.String()
			if tc.wantFlush {
				if !bytes.Contains([]byte(out), []byte("Flushed")) {
					t.Fatalf("expected a flush report, got:\n%s", out)
				}
				return
			}
			if bytes.Contains([]byte(out), []byte("Flushed")) {
				t.Fatalf("claimed a flush that did not happen, got:\n%s", out)
			}
			if !bytes.Contains([]byte(out), []byte("nothing to flush")) {
				t.Fatalf("expected 'nothing to flush', got:\n%s", out)
			}
		})
	}
}

// TestVaultFlush_PathReportsRealFlush verifies the happy path: a genuinely
// staged file that FlushPath makes durable reports "Flushed <path>" (flushed:1).
func TestVaultFlush_PathReportsRealFlush(t *testing.T) {
	home, err := os.MkdirTemp("", "vault-flush-path-ok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	overrideHome(t, home)

	if err := vault.SaveRegistry(&vault.VaultRegistry{
		Default:  "personal",
		Profiles: map[string]vault.ProfileConfig{"personal": {VaultID: "vault:aaa"}},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	orig := vaultServiceFactory
	t.Cleanup(func() { vaultServiceFactory = orig })
	// pre-check status=staged, then after a real flush status=durable.
	stub := &flushCmdStub{statSeq: []string{vault.FileStatusStaged, vault.FileStatusDurable}}
	vaultServiceFactory = func(profileName, indexerURL string) (vault.VaultService, error) {
		return stub, nil
	}

	root := NewRootCommand()
	var buf bytes.Buffer
	root.Writer = &buf
	if err := root.Run(context.Background(), []string{"pinner", "vault", "flush", "vault:/a.txt"}); err != nil {
		t.Fatalf("vault flush failed: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("Flushed")) {
		t.Fatalf("expected a real flush report, got:\n%s", buf.String())
	}
	if stub.flushPings != 1 {
		t.Fatalf("FlushPath called %d times, want 1", stub.flushPings)
	}
}
