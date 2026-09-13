package gitremote

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/revlist"
)

// GoGitStore is a Store backed by an in-process go-git repository (typically a
// bare repository acting as the archive twin). It exists so the full fetch/push
// protocol can be exercised end-to-end with only go-git — no Sia, no network —
// and doubles as a local mirror backend for git-only development/testing.
//
// Upload ingests the incoming pack into the repo's object store and advances the
// reference; Download re-encodes a full pack of the history reachable from the
// requested tip; List/Delete read/remove references directly.
type GoGitStore struct {
	// Repo is the bare/archive repository that acts as the remote.
	Repo *git.Repository
}

var _ Store = (*GoGitStore)(nil)

func (s *GoGitStore) List(ctx context.Context, forPush bool) ([]Ref, error) {
	iter, err := s.Repo.Storer.IterReferences()
	if err != nil {
		return nil, fmt.Errorf("gogit store: list refs: %w", err)
	}
	defer iter.Close()
	var refs []Ref
	_ = iter.ForEach(func(r *plumbing.Reference) error {
		if r.Type() == plumbing.SymbolicReference {
			return nil
		}
		refs = append(refs, Ref{Name: r.Name().String(), Hash: r.Hash()})
		return nil
	})
	return refs, nil
}

// Download re-encodes a full pack of every object reachable from targetHash.
func (s *GoGitStore) Download(ctx context.Context, refName, targetHash string) (io.ReadCloser, error) {
	hash := plumbing.NewHash(targetHash)
	objs, err := revlist.Objects(s.Repo.Storer, []plumbing.Hash{hash}, nil)
	if err != nil {
		return nil, fmt.Errorf("gogit store: resolve download set: %w", err)
	}
	var buf bytes.Buffer
	enc := packfile.NewEncoder(&buf, s.Repo.Storer, false) // delta=false
	if _, err := enc.Encode(objs, 0); err != nil {
		return nil, fmt.Errorf("gogit store: encode download pack: %w", err)
	}
	return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

func (s *GoGitStore) Upload(ctx context.Context, dstRef, srcHash string, pack io.Reader) error {
	st := s.Repo.Storer
	if err := IngestPack(st, pack); err != nil {
		return err
	}
	return SetLocalRef(st, dstRef, plumbing.NewHash(srcHash))
}

func (s *GoGitStore) Delete(ctx context.Context, dstRef string) error {
	return s.Repo.Storer.RemoveReference(plumbing.ReferenceName(dstRef))
}

func (s *GoGitStore) Close() error { return nil }
