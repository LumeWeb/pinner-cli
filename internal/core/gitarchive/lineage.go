package gitarchive

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage"
)

// Lineage is the identity fingerprint of a git repository for archive
// matching: two clones that share history have identical root-commit sets, so
// the (sorted, deduplicated) root commit hashes form a stable key that lets
// `pinner git watch` match a local repo to an existing archive locker.
//
// All Git work here runs in-process via go-git — no git binary, no shell-outs.

// ComputeLineage opens the repository at path (detecting a nested .git) and
// returns its root-commit lineage. path may be the repository root, a
// subdirectory, or any enclosing directory.
func ComputeLineage(path string) ([]plumbing.Hash, error) {
	repo, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return nil, fmt.Errorf("gitarchive: open repo: %w", err)
	}
	return RepoLineage(repo.Storer)
}

// RepoLineage computes the root-commit lineage from a repository's object
// store (see ComputeLineage).
func RepoLineage(st storage.Storer) ([]plumbing.Hash, error) {
	roots := map[plumbing.Hash]bool{}
	iter, err := st.IterReferences()
	if err != nil {
		return nil, fmt.Errorf("gitarchive: list references: %w", err)
	}
	defer iter.Close()
	if err := iter.ForEach(func(r *plumbing.Reference) error {
		if r.Type() == plumbing.SymbolicReference {
			return nil
		}
		if err := collectRoots(st, r.Hash(), roots); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("gitarchive: walk lineage: %w", err)
	}
	hs := make([]plumbing.Hash, 0, len(roots))
	for h := range roots {
		hs = append(hs, h)
	}
	sort.Slice(hs, func(i, j int) bool { return hs[i].String() < hs[j].String() })
	return hs, nil
}

// collectRoots records every root commit (zero-parent commit) reachable from h,
// walking history recursively. Non-commit objects (e.g. a presenting tag object
// under refs/tags/...) are skipped rather than treated as roots.
func collectRoots(st storage.Storer, h plumbing.Hash, roots map[plumbing.Hash]bool) error {
	if roots[h] {
		return nil
	}
	c, err := object.GetCommit(st, h)
	if err != nil {
		// Not a commit (or not present locally) — skip; branches still yield
		// the full root set through their tip history.
		return nil
	}
	if c.NumParents() == 0 {
		roots[h] = true
		return nil
	}
	for _, ph := range c.ParentHashes {
		if err := collectRoots(st, ph, roots); err != nil {
			return err
		}
	}
	return nil
}

// LineageKey renders a lineage as its canonical string form — the sorted root
// hashes joined by commas. Sorting internally makes the key order-independent
// (the same archive fingerprint whichever order the roots were discovered in).
// This is the value stored in git_repos.Lineage and compared during lineage
// matching.
func LineageKey(hashes []plumbing.Hash) string {
	sorted := make([]string, len(hashes))
	for i, h := range hashes {
		sorted[i] = h.String()
	}
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

// DefaultLockerName derives a default locker name from a local repo path (its
// base directory name). Callers override it with an explicit --locker.
func DefaultLockerName(path string) string {
	return filepath.Base(path)
}
