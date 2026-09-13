//go:build !gitarchive_sia

package gitremote

import (
	"context"
	"fmt"
	"io"
)

// These are the default-build implementations of the SessionStore data
// operations. The real Sia-backed implementations live in session_store_sia.go
// guarded by the `gitarchive_sia` build tag; without that tag we cannot know we
// are wired to a live Sia indexer, so the data operations report the
// ErrSiaNotWired sentinel rather than pretending to work. Only List/Download/
// Upload/Delete differ; the struct, constructors and Close (session_store.go)
// are shared.

func (s *SessionStore) List(ctx context.Context, forPush bool) ([]Ref, error) {
	return nil, fmt.Errorf("%w: list", ErrSiaNotWired)
}

func (s *SessionStore) Download(ctx context.Context, refName, targetHash string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("%w: download", ErrSiaNotWired)
}

func (s *SessionStore) Upload(ctx context.Context, dstRef, srcHash string, pack io.Reader) error {
	return fmt.Errorf("%w: upload", ErrSiaNotWired)
}

func (s *SessionStore) Delete(ctx context.Context, dstRef string) error {
	return fmt.Errorf("%w: delete", ErrSiaNotWired)
}
