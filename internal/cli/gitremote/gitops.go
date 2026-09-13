package gitremote

import (
	"bytes"
	"fmt"
	"io"
	"regexp"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/revlist"
	"github.com/go-git/go-git/v5/storage"
)

// This file holds the in-process go-git object plumbing for the remote helper:
// pack encoding (incremental set-difference via revlist + packfile.NewEncoder)
// and pack ingestion (packfile.UpdateObjectStorage), plus reference resolution.
// Everything runs against go-git; no git binary is ever spawned.

var hexHashRE = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// resolveRevision resolves a local revision string from a store to a concrete
// object hash. It accepts a full sha1 or a reference name (refs/..., HEAD, or a
// short branch/tag name).
func resolveRevision(s storage.Storer, rev string) (plumbing.Hash, error) {
	if hexHashRE.MatchString(rev) {
		return plumbing.NewHash(rev), nil
	}

	names := candidateRefNames(rev)
	for _, n := range names {
		ref, err := s.Reference(plumbing.ReferenceName(n))
		if err == nil {
			// Follow symbolic references (e.g. HEAD -> refs/heads/master).
			if ref.Type() == plumbing.SymbolicReference && ref.Target() != "" {
				return resolveRevision(s, ref.Target().String())
			}
			return ref.Hash(), nil
		}
	}
	return plumbing.ZeroHash, fmt.Errorf("gitremote: cannot resolve revision %q", rev)
}

// candidateRefNames expands a possibly-short revision into the full reference
// names go-git stores, in lookup order (explicit refs first, then HEAD, then the
// common heads/tags namespaces).
func candidateRefNames(rev string) []string {
	if rev == "HEAD" || rev == "refs/" {
		return []string{rev}
	}
	full := []string{rev}
	if name := plumbing.ReferenceName(rev); !name.IsBranch() && !name.IsRemote() &&
		!name.IsTag() && rev != "HEAD" {
		full = append(full, "refs/heads/"+rev, "refs/tags/"+rev)
	}
	return full
}

// EncodeIncrementalPack builds a raw pack (v2, delta=false) containing exactly
// the objects reachable from wants that are NOT reachable from haves. Both sets
// are read from s (the local pushing/fetching repository's object store).
//
// Passing no haves yields a full pack of everything reachable from wants (used
// for a first push / a fresh clone download). The pack file is written to a
// memory buffer and returned as bytes. When there is nothing new to send the
// returned buffer is empty.
func EncodeIncrementalPack(s storage.Storer, wants, haves []plumbing.Hash) ([]byte, error) {
	objs, err := revlist.Objects(s, wants, haves)
	if err != nil {
		return nil, fmt.Errorf("gitremote: compute object set: %w", err)
	}
	if len(objs) == 0 {
		return nil, nil
	}

	var buf bytes.Buffer
	enc := packfile.NewEncoder(&buf, s, false) // useRefDeltas=false; non-thin packs
	// packWindow 0 turns delta compression off entirely, producing a self
	// contained pack of exactly the given objects.
	if _, err := enc.Encode(objs, 0); err != nil {
		return nil, fmt.Errorf("gitremote: encode pack: %w", err)
	}
	return buf.Bytes(), nil
}

// IngestPack writes the objects from a raw pack into the object store. It is
// the fetch-side counterpart to EncodeIncrementalPack and accepts an empty pack
// (or nil reader) as a no-op — there was nothing new to ingest.
func IngestPack(s storage.Storer, r io.Reader) error {
	if r == nil {
		return nil
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("gitremote: read pack: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := packfile.UpdateObjectStorage(s, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("gitremote: ingest pack: %w", err)
	}
	return nil
}

// SetLocalRef sets refName to hash in the local store (used by the go-git store
// to move refs after upload/delete).
func SetLocalRef(s storage.Storer, refName string, hash plumbing.Hash) error {
	ref := plumbing.NewReferenceFromStrings(refName, hash.String())
	if err := s.SetReference(ref); err != nil {
		return fmt.Errorf("gitremote: set ref %s: %w", refName, err)
	}
	return nil
}

// readCommit loads an ingested commit from a store, used by tests to assert that
// a fetch transferred full history.
func readCommit(s storage.Storer, hash plumbing.Hash) (*object.Commit, error) {
	obj, err := s.EncodedObject(plumbing.CommitObject, hash)
	if err != nil {
		return nil, err
	}
	return object.DecodeCommit(s, obj)
}
