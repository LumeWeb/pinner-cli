// Package gitremote implements the shared Git remote-helper protocol engine
// for the single `pinner` executable.
//
// A Git remote helper is a program named `git-remote-<transport>` that Git
// spawns and talks to over stdin/stdout. Exactly one binary is built: `cmd/pinner`.
// When that binary is invoked through a `git-remote-pinner` symlink, main detects
// the argv-0 basename and runs THIS package's protocol engine instead of the
// normal CLI tree. The helper never enters urfave command parsing and the normal
// CLI never reads Git helper stdin — dispatch is isolated at the entry point.
//
// The protocol engine is deliberately decoupled from any particular backend. The
// remote-helper protocol (see gitremote-helpers(7)) reduces to four concerns:
//
//   - capabilities: what the helper supports (option / fetch / push);
//   - list [for-push]: advertise the remote refs;
//   - fetch <sha1> <name>: write objects reachable from sha1 into the local DB;
//   - push +<src>:<dst>: read objects from the local repo, persist them and move
//     the remote ref.
//
// The Go object transfer — pack encode / pack ingest / reference handling — all
// happens in-process via go-git (no git subprocess, no shell-outs). The Store
// interface is the injected seam between the protocol engine and whatever backs
// an archive: production binds the Sia-backed session (Steps 5+ wire the actual
// pack/tip publish + download), while tests and the git-only integration mode use
// a go-git bare repository (see GoGitStore).
package gitremote

import (
	"context"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
)

// Ref is one advertised remote reference produced by Store.List.
type Ref struct {
	// Name is the full reference name, e.g. "refs/heads/master".
	Name string
	// Hash is the 40-hex sha1 of the ref tip. ZeroHash advertises an unknown
	// value ("?" in protocol terms) — used sparingly; most helpers advertise a
	// concrete sha1.
	Hash plumbing.Hash
}

// Store is the persistence seam for the remote-helper protocol engine.
//
// Fetch/push are described in terms of raw packfiles and references:
//
//   - Download returns a reader over the pack that, when ingested into a local
//     object store, provides all objects reachable from targetHash for refName.
//   - Upload persists a raw pack (the set of new objects for srcHash) and moves
//     dstRef to srcHash. If the pack is empty the store only has to move the ref.
//   - Delete removes dstRef (the empty-source delete case: `push :dst`).
//
// The signature deliberately stays at the byte/ref level: all go-git encode and
// ingest (see gitops.go) happens on the helper side against the *local* repo,
// while the Store is the remote that consumes/produces packs. This is what keeps
// the engine backend-agnostic and lets identical tests run against a go-git
// bare-repo store and, later, the Sia-backed store.
type Store interface {
	// List advertises the remote refs. forPush=true is the `list for-push`
	// form used to prepare a push batch.
	List(ctx context.Context, forPush bool) ([]Ref, error)

	// Download returns the raw pack of objects reachable from targetHash for
	// refName. For a non-incremental archive this is the full history pack; the
	// caller ingests it into the local object store via IngestPack.
	Download(ctx context.Context, refName, targetHash string) (io.ReadCloser, error)

	// Upload persists the given raw pack (objects needed for srcHash) so that
	// the archive can serve them, then moves dstRef to srcHash. An empty pack
	// means no new objects were needed and only the reference is updated.
	Upload(ctx context.Context, dstRef, srcHash string, pack io.Reader) error

	// Delete removes dstRef from the remote (delete ref push).
	Delete(ctx context.Context, dstRef string) error

	// Close releases any backend-held resources (e.g. a Sia SDK, an open DB).
	Close() error
}
