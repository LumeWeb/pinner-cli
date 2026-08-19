package oob

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// pruneRunner is a minimal wizard.RestoreRunner for the prune test; the runner
// is never invoked because pruning operates purely on the retained outcome map.
type pruneRunner struct{}

func (pruneRunner) RestoreProfileName() string { return "default" }
func (pruneRunner) RunRestore(ctx context.Context, profile, mnemonic string, onApproval func(approvalURL string)) (string, error) {
	return "", nil
}

func TestOOBRestorePruneOutcomes(t *testing.T) {
	o := NewOOBRestore(pruneRunner{}, time.Minute)

	staleTerminal := &restoreOutcome{succeeded: true, started: time.Now().Add(-2 * DefaultRestoreTTL)}
	freshTerminal := &restoreOutcome{succeeded: true, started: time.Now()}
	failedTerminal := &restoreOutcome{err: "seed rejected", started: time.Now().Add(-2 * DefaultRestoreTTL)}
	inProgress := &restoreOutcome{started: time.Now()}

	o.mu.Lock()
	o.outcomes["stale"] = staleTerminal
	o.outcomes["fresh"] = freshTerminal
	o.outcomes["failed"] = failedTerminal
	o.outcomes["running"] = inProgress
	o.pruneOutcomesLocked(time.Now().Add(-DefaultRestoreTTL))
	o.mu.Unlock()

	o.mu.Lock()
	defer o.mu.Unlock()
	_, hasStale := o.outcomes["stale"]
	_, hasFresh := o.outcomes["fresh"]
	_, hasFailed := o.outcomes["failed"]
	_, hasRunning := o.outcomes["running"]
	assert.False(t, hasStale, "an aged terminal (succeeded) outcome must be pruned")
	assert.True(t, hasFresh, "a fresh terminal outcome must be retained until swept")
	assert.False(t, hasFailed, "an aged terminal (failed) outcome must be pruned")
	assert.True(t, hasRunning, "an in-progress (unsettled) outcome must never be pruned")
}
