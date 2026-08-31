//go:build !no_tunnel

package cli

import (
	"context"
	"errors"
	"os"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/catalog"
)

// TestPinsRmSelectorGateEndToEnd verifies the cids|all SelectionGroup is
// enforced on the real CLI command path. This is the safety contract: pointing
// --all at an explicit CID list must fail closed (never silently unpin
// everything), while a bare --all proceeds past the selector gate.
func TestPinsRmSelectorGateEndToEnd(t *testing.T) {
	home, err := os.MkdirTemp("", "pins-rm-selector")
	if err != nil {
		t.Fatalf("mk temp home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	overrideHome(t, home)

	// Explicit positional CID alongside --all --force is a destructive
	// ambiguity: the shared selector gate must reject it before the handler
	// runs (which would otherwise silently unpin-all), regardless of --force.
	err = Run(context.Background(), []string{
		"pinner", "pins", "rm", "--all", "--force", "QmXyz",
	})
	if err == nil {
		t.Fatalf("pins rm --all --force with an explicit CID: expected selector error, got nil")
	}
	if !errors.Is(err, catalog.ErrSelector) {
		t.Fatalf("pins rm --all --force <cid>: expected ErrSelector, got %v", err)
	}

	// A bare --all --force must NOT trip the selector gate. It should proceed
	// past normalization toward the service (failing later on auth/network, not
	// on the selector contract). A selector-contract error here would be a false
	// rejection of a legitimate unpin-all.
	err = Run(context.Background(), []string{
		"pinner", "pins", "rm", "--all", "--force",
	})
	if errors.Is(err, catalog.ErrSelector) {
		t.Fatalf("bare pins rm --all --force: unexpected selector error %v", err)
	}
	// It may still fail on auth/service, but that must NOT be an ErrSelector.
}
