package gitremote

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage"
)

// This file implements `pinner git show`: it turns the archive into a local,
// renderable object graph using only go-git (no git subprocess). It is split
// into two independent halves so each can be tested in isolation:
//
//   - EnsureArchiveRefs pulls every advertised archive ref's pack into a local
//     store and advances the corresponding reference, so the local store is a
//     ready mirror of the archive;
//   - Render reads refs, the commit log (git log -n 20 style) and the recursive
//     file listing (git ls-tree -r --long style) out of an already-populated
//     store.
//
// The byte-level Store seam keeps this backend-agnostic: production binds the
// Sia-backed session, while tests use a go-git bare-repo store.

// CommitLine is one rendered commit for `pinner git show` (git log -n 20
// style). It carries only the git-side metadata surfaced by the log template —
// never Sia object/slab/pin keys.
type CommitLine struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	When    int64  `json:"when"` // unix seconds
	Message string `json:"message"`
}

// TreeLine is one file entry for `pinner git show` (git ls-tree -r --long
// style). Only blob files are listed (via object.Tree.Files), mirroring the
// recursive file view; mode/hash/size/path are the long-format columns.
type TreeLine struct {
	Mode string `json:"mode"`
	Hash string `json:"hash"`
	Size int64  `json:"size"`
	Path string `json:"path"`
}

// ShowReport is the fully rendered archive view produced by Render.
type ShowReport struct {
	Refs    []Ref        `json:"refs"`
	Commits []CommitLine `json:"commits"`
	Tree    []TreeLine   `json:"tree"`
}

// DefaultCommitLimit is the number of commits surfaced by `pinner git show`
// (mirrors `git log -n 20`).
const DefaultCommitLimit = 20

// EnsureArchiveRefs pulls every non-symbolic ref advertised by store into local
// (downloading its pack and ingesting it, then pointing the ref at the tip) so
// local becomes a ready mirror. It returns the refs that were ensured. A store
// that reports zero refs (empty archive) leaves local untouched.
func EnsureArchiveRefs(ctx context.Context, local storage.Storer, store Store) ([]Ref, error) {
	refs, err := store.List(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("gitremote: list archive refs: %w", err)
	}
	for _, r := range refs {
		if r.Hash == plumbing.ZeroHash {
			continue
		}
		rc, err := store.Download(ctx, r.Name, r.Hash.String())
		if err != nil {
			return nil, fmt.Errorf("gitremote: download pack for %s: %w", r.Name, err)
		}
		ingestErr := IngestPack(local, rc)
		if rc != nil {
			rc.Close()
		}
		if ingestErr != nil {
			return nil, fmt.Errorf("gitremote: ingest pack for %s: %w", r.Name, ingestErr)
		}
		if err := SetLocalRef(local, r.Name, r.Hash); err != nil {
			return nil, fmt.Errorf("gitremote: set ref %s: %w", r.Name, err)
		}
	}
	return refs, nil
}

// Render produces the show report from an already-populated store: the refs,
// the most recent maxCommits commits across all ref tips (deduplicated by hash,
// ordered newest-first like a combined git log), and the recursive file listing
// across every ref tip tree. maxCommits <= 0 falls back to DefaultCommitLimit.
func Render(local storage.Storer, maxCommits int) (*ShowReport, error) {
	if maxCommits <= 0 {
		maxCommits = DefaultCommitLimit
	}
	refs, err := allRefs(local)
	if err != nil {
		return nil, err
	}

	// Collect commits reachable from every ref tip, deduped by hash.
	commits, err := collectCommits(local, refs, maxCommits)
	if err != nil {
		return nil, err
	}

	// Collect files from every ref tip tree, deduped by path.
	tree, err := collectFiles(local, refs)
	if err != nil {
		return nil, err
	}

	return &ShowReport{Refs: refs, Commits: commits, Tree: tree}, nil
}

// allRefs lists every non-symbolic reference in the store, sorted by name.
func allRefs(st storage.Storer) ([]Ref, error) {
	iter, err := st.IterReferences()
	if err != nil {
		return nil, fmt.Errorf("gitremote: list refs: %w", err)
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
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs, nil
}

// collectCommits gathers up to maxCommits distinct commits reachable from the
// given ref tips, ordered newest-first (by author time, ties by hash) to mirror
// a combined `git log -n 20`.
func collectCommits(st storage.Storer, refs []Ref, maxCommits int) ([]CommitLine, error) {
	seen := map[plumbing.Hash]bool{}
	lines := []CommitLine{}
	for _, r := range refs {
		if r.Hash == plumbing.ZeroHash {
			continue
		}
		if err := walkCommits(st, r.Hash, seen, &lines); err != nil {
			return nil, err
		}
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].When != lines[j].When {
			return lines[i].When > lines[j].When // newest first
		}
		return lines[i].Hash < lines[j].Hash
	})
	if len(lines) > maxCommits {
		lines = lines[:maxCommits]
	}
	return lines, nil
}

// walkCommits recursively appends commit lines for the history reachable from h.
func walkCommits(st storage.Storer, h plumbing.Hash, seen map[plumbing.Hash]bool, out *[]CommitLine) error {
	if seen[h] {
		return nil
	}
	seen[h] = true
	c, err := object.GetCommit(st, h)
	if err != nil {
		// Not a commit (e.g. an annotated tag object or a missing object) —
		// skip rather than fail the whole render.
		return nil
	}
	t := c.Author.When
	if t.IsZero() {
		t = c.Committer.When
	}
	*out = append(*out, CommitLine{
		Hash:    h.String(),
		Author:  c.Author.Name,
		When:    t.Unix(),
		Message: c.Message,
	})
	for _, ph := range c.ParentHashes {
		if err := walkCommits(st, ph, seen, out); err != nil {
			return err
		}
	}
	return nil
}

// collectFiles gathers the recursive file listing (git ls-tree -r --long style)
// across every ref tip tree, deduped by path.
func collectFiles(st storage.Storer, refs []Ref) ([]TreeLine, error) {
	seen := map[string]bool{}
	lines := []TreeLine{}
	for _, r := range refs {
		if r.Hash == plumbing.ZeroHash {
			continue
		}
		c, err := object.GetCommit(st, r.Hash)
		if err != nil {
			continue
		}
		tree, err := c.Tree()
		if err != nil {
			return nil, fmt.Errorf("gitremote: read tree for %s: %w", r.Name, err)
		}
		if err := tree.Files().ForEach(func(f *object.File) error {
			if seen[f.Name] {
				return nil
			}
			seen[f.Name] = true
			size := int64(0)
			if blob := f.Blob; blob != (object.Blob{}) {
				size = blob.Size
			}
			lines = append(lines, TreeLine{
				Mode: f.Mode.String(),
				Hash: f.Hash.String(),
				Size: size,
				Path: f.Name,
			})
			return nil
		}); err != nil {
			return nil, fmt.Errorf("gitremote: list files for %s: %w", r.Name, err)
		}
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].Path < lines[j].Path })
	return lines, nil
}
