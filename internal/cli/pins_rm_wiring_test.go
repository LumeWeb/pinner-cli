package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPinsRmRejectsPositionalWithAll pins the destructive-selector contract at
// the CLI boundary: `pins rm <cid> --all --force` must error (never silently
// drop the typed CIDs and unpin everything), while a bare `--all` must not trip
// the selector error.
func TestPinsRmRejectsPositionalWithAll(t *testing.T) {
	home, err := os.MkdirTemp("", "pinsrm-all")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	overrideHome(t, home)

	root := NewRootCommand()
	var buf bytes.Buffer
	root.Writer = &buf

	// positional CIDs + --all => explicit destructive ambiguity => rejected.
	err = root.Run(context.Background(), []string{
		"pinner", "pins", "rm", "bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e",
		"--all", "--force",
	})
	if err == nil {
		t.Fatal("pins rm <cid> --all must error (destructive ambiguity)")
	}
	if !strings.Contains(err.Error(), "not both") {
		t.Fatalf("error must explain the cids/--all conflict, got: %v", err)
	}

	// bare --all (no positional, no stdin) must NOT trip the selector error;
	// it proceeds toward unpin-all (and fails on auth/network, not the guard).
	err = root.Run(context.Background(), []string{
		"pinner", "pins", "rm", "--all", "--force",
	})
	if err != nil && strings.Contains(err.Error(), "not both") {
		t.Fatalf("bare --all must not trip the cids/--all selector error, got: %v", err)
	}

	// The generated --cids flag is also explicit selector intent: --cids + --all
	// must be rejected at the CLI boundary, not only by the handler.
	err = root.Run(context.Background(), []string{
		"pinner", "pins", "rm", "--cids", "bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e",
		"--all", "--force",
	})
	if err == nil {
		t.Fatal("pins rm --cids <cid> --all must error (destructive ambiguity)")
	}
	if !strings.Contains(err.Error(), "not both") {
		t.Fatalf("--cids + --all error must explain the conflict, got: %v", err)
	}

	// An --file of CIDs alongside --all is a destructive ambiguity too: the
	// operator pointed --all at a real CID list, so reject (never silently drop
	// the file's CIDs and unpin everything).
	cidsFile := filepath.Join(home, "cids.txt")
	if werr := os.WriteFile(cidsFile, []byte("bafybeigqaforwjgcx45jnh7dgyfgqqm2lei4hurrrnsizrpgyxz3egtd7e\n"), 0o600); werr != nil {
		t.Fatal(werr)
	}
	err = root.Run(context.Background(), []string{
		"pinner", "pins", "rm", "--file", cidsFile, "--all", "--force",
	})
	if err == nil {
		t.Fatal("pins rm --file <cids> --all must error (destructive ambiguity)")
	}
	if !strings.Contains(err.Error(), "not both") {
		t.Fatalf("--file + --all error must explain the conflict, got: %v", err)
	}
}
