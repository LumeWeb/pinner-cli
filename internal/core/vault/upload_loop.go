package vault

import (
	"context"
	"log"
	"sync"
	"time"
)

// VaultUploadLoop is a ServiceScheduler TickFunc that continuously drains every
// registered vault profile's staged ("pending") files to durable storage while
// the MCP server runs. It is the background counterpart to the fast staging
// write: vault_put_file buffers bytes locally and returns immediately; this
// loop packs several staged files into shared slabs (UploadPacked), pins them,
// and clears the staged buffers — so a lone 7-byte file does not block a write
// on a full host-set upload, and a burst of small files is uploaded efficiently
// in shared slabs.
//
// It keeps one VaultService per profile across idle ticks (reusing the
// VaultSyncLoop pattern) so a long-running server does not re-open each SQLite
// cache and rebuild each Sia SDK every interval.
type VaultUploadLoop struct {
	cfg SyncLoopConfig

	mu     sync.Mutex
	svcs   map[string]VaultService
	closed bool
}

// NewVaultUploadLoop creates a persistent multi-profile upload loop over cfg.
// Close it when the server shuts down to release every held service.
func NewVaultUploadLoop(cfg SyncLoopConfig) *VaultUploadLoop {
	return &VaultUploadLoop{cfg: cfg, svcs: map[string]VaultService{}}
}

// Tick implements the ServiceScheduler TickFunc contract. It drains every
// accessible profile's pending files via Flush. It returns a Duration(0) when
// any profile flushed at least one file, so the scheduler re-runs immediately
// (the same burst, reusing the open services) until the backlog is drained.
func (l *VaultUploadLoop) Tick(ctx context.Context) *time.Duration {
	if err := ctx.Err(); err != nil {
		return nil
	}
	rerun := false
	for _, p := range l.cfg.Profiles() {
		if err := ctx.Err(); err != nil {
			return nil
		}
		svc, err := l.ensureService(p)
		if err != nil {
			log.Printf("vault flush: skip profile %q (service build: %v)", p, err)
			continue
		}
		n, ferr := svc.Flush(ctx)
		if ferr != nil {
			log.Printf("vault flush: profile %q failed: %v", p, ferr)
			continue
		}
		if n > 0 {
			rerun = true
		}
	}
	if rerun {
		zero := time.Duration(0)
		return &zero
	}
	return nil
}

func (l *VaultUploadLoop) ensureService(profile string) (VaultService, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, ErrVaultClosed
	}
	if svc := l.svcs[profile]; svc != nil {
		return svc, nil
	}
	svc, err := l.cfg.Service(profile)
	if err != nil {
		return nil, err
	}
	l.svcs[profile] = svc
	return svc, nil
}

// Close releases every held VaultService. Safe to call more than once and
// concurrently with a running Tick.
func (l *VaultUploadLoop) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	for p, svc := range l.svcs {
		_ = svc.Close()
		delete(l.svcs, p)
	}
}
