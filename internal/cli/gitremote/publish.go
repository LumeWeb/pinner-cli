package gitremote

import (
	"bytes"
	"context"
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/revlist"
	"github.com/go-git/go-git/v5/storage"
)

// LocalBranches lists the local branch refs (refs/heads/*) with their concrete
// tip hashes, skipping symbolic references.
func LocalBranches(st storage.Storer) ([]Ref, error) {
	iter, err := st.IterReferences()
	if err != nil {
		return nil, fmt.Errorf("gitremote: list local branches: %w", err)
	}
	defer iter.Close()
	var refs []Ref
	_ = iter.ForEach(func(r *plumbing.Reference) error {
		if r.Type() == plumbing.SymbolicReference || !r.Name().IsBranch() {
			return nil
		}
		refs = append(refs, Ref{Name: r.Name().String(), Hash: r.Hash()})
		return nil
	})
	return refs, nil
}

// PublishAll performs a first publish (or a refresh) of every local branch tip
// to the archive: for each branch it encodes a full pack of everything
// reachable from the tip (the archive is treated as initially empty) and
// Uploads it under the same branch ref name. It returns the refs published. The
// pack encode and the Store upload both run in-process via go-git — no git
// binary is spawned.
func PublishAll(ctx context.Context, local storage.Storer, store Store) ([]Ref, error) {
	refs, err := LocalBranches(local)
	if err != nil {
		return nil, err
	}
	for _, r := range refs {
		pack, err := EncodeIncrementalPack(local, []plumbing.Hash{r.Hash}, nil)
		if err != nil {
			return nil, fmt.Errorf("gitremote: encode first pack for %s: %w", r.Name, err)
		}
		if err := store.Upload(ctx, r.Name, r.Hash.String(), bytes.NewReader(pack)); err != nil {
			return nil, fmt.Errorf("gitremote: publish %s: %w", r.Name, err)
		}
	}
	return refs, nil
}

// Divergence computes how far ahead (commits present only on the local side)
// and behind (commits present only on the remote/archived side) localHash is
// relative to remoteHash. It counts commit objects only, ignoring trees, blobs
// and tags reachable from either tip.
func Divergence(st storage.Storer, localHash, remoteHash plumbing.Hash) (ahead, behind int, err error) {
	loc, err := commitSet(st, localHash)
	if err != nil {
		return 0, 0, fmt.Errorf("gitremote: walk local commits: %w", err)
	}
	rem, err := commitSet(st, remoteHash)
	if err != nil {
		return 0, 0, fmt.Errorf("gitremote: walk remote commits: %w", err)
	}
	for h := range loc {
		if !rem[h] {
			ahead++
		}
	}
	for h := range rem {
		if !loc[h] {
			behind++
		}
	}
	return ahead, behind, nil
}

// commitSet returns the set of commit hashes reachable from h in st.
func commitSet(st storage.Storer, h plumbing.Hash) (map[plumbing.Hash]bool, error) {
	objs, err := revlist.Objects(st, []plumbing.Hash{h}, nil)
	if err != nil {
		return nil, err
	}
	set := make(map[plumbing.Hash]bool, len(objs))
	for _, oh := range objs {
		o, err := st.EncodedObject(plumbing.AnyObject, oh)
		if err != nil {
			continue
		}
		if o.Type() == plumbing.CommitObject {
			set[oh] = true
		}
	}
	return set, nil
}
