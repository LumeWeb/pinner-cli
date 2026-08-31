//go:build sqlite_fts5

package vault

import (
	"bytes"
	"context"
	"testing"
)

// TestCrossProfileProbe_StagedLocalReadPerProfile demonstrates the
// per-profile-file staging foundation of the cross-profile vault handoff
// redesign: two independent profile services can each stage files locally
// (status staged), read them back locally with no Sia interaction, and only a
// flush (one worker per profile) makes them durable. This is the local half of
// the probe and never sleeps on Sia.
func TestCrossProfileProbe_StagedLocalReadPerProfile(t *testing.T) {
	ctx := context.Background()

	// Two independent profile services (one per profile) — they never share a
	// staged buffer, DB, or worker.
	alphaSvc, alphaSDK := newStagingService(t, t.TempDir())
	betaSvc, betaSDK := newStagingService(t, t.TempDir())

	// Profile "alpha" PUT stages a file: locally readable immediately, status
	// canonical "staged", no object key yet, and no Sia I/O happened.
	alphaContent := []byte("alpha staged bytes")
	alphaFile, err := alphaSvc.Put(ctx, bytes.NewReader(alphaContent), int64(len(alphaContent)), "vault:/docs/alpha.txt", map[string]any{})
	if err != nil {
		t.Fatalf("alpha Put: %v", err)
	}
	if alphaFile.Status != FileStatusStaged {
		t.Fatalf("alpha status = %q, want staged", alphaFile.Status)
	}
	if alphaFile.ObjectKey != "" || alphaSDK.uploadCalled || alphaSDK.pinCalled {
		t.Fatalf("staging must not touch Sia (upload=%v pin=%v)", alphaSDK.uploadCalled, alphaSDK.pinCalled)
	}
	var alphaBuf bytes.Buffer
	if err := alphaSvc.Get(ctx, "vault:/docs/alpha.txt", &alphaBuf); err != nil {
		t.Fatalf("alpha local Get on staged: %v", err)
	}
	if !bytes.Equal(alphaBuf.Bytes(), alphaContent) {
		t.Fatalf("alpha staged Get mismatch")
	}

	// Profile "beta" stages its own independent file; the two profiles share
	// nothing.
	betaContent := []byte("beta staged bytes")
	betaFile, err := betaSvc.Put(ctx, bytes.NewReader(betaContent), int64(len(betaContent)), "vault:/docs/beta.txt", map[string]any{})
	if err != nil {
		t.Fatalf("beta Put: %v", err)
	}
	if betaFile.Status != FileStatusStaged {
		t.Fatalf("beta status = %q, want staged", betaFile.Status)
	}
	if betaSDK.uploadCalled || betaSDK.pinCalled {
		t.Fatalf("beta staging must not touch Sia")
	}
	var betaBuf bytes.Buffer
	if err := betaSvc.Get(ctx, "vault:/docs/beta.txt", &betaBuf); err != nil {
		t.Fatalf("beta local Get on staged: %v", err)
	}
	if !bytes.Equal(betaBuf.Bytes(), betaContent) {
		t.Fatalf("beta staged Get mismatch")
	}

	// A per-profile flush makes alpha's file durable (upload + pin) in one
	// packed pass.
	if n, err := alphaSvc.Flush(ctx); err != nil || n != 1 {
		t.Fatalf("alpha flush = (%d, %v), want (1, nil)", n, err)
	}
	st, err := alphaSvc.Stat(ctx, "vault:/docs/alpha.txt")
	if err != nil {
		t.Fatalf("alpha Stat after flush: %v", err)
	}
	if st.Status != FileStatusDurable {
		t.Fatalf("alpha status after flush = %q, want durable", st.Status)
	}
}
