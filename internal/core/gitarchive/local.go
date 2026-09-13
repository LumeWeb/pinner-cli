package gitarchive

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"

	"go.lumeweb.com/pinner/core/vault"
)

// This file holds the local-only helpers shared by `pinner git show`, `pinner
// git unwatch`, and `pinner git doctor` that operate on the profile's private
// bare mirror and the watched worktree config — all through go-git (no git
// subprocess). Sia object read/write stays in the Store/session layer; these
// helpers only manage the local filesystem bookkeeping.

// PrivateRepoDir returns the on-disk directory holding the private bare git
// mirror for a locker, under the profile dir:
//
//	<ProfileDir>/git/<locker>.git
//
// This mirror is where `pinner git show` ingests archived packs so the user can
// browse refs, the commit log, and the tree with go-git rendered locally.
func PrivateRepoDir(profile, locker string) string {
	return filepath.Join(vault.ProfileDir(profile), "git", locker+".git")
}

// OpenPrivateBare opens (creating if needed) the private bare mirror repository
// for a locker in the given profile dir. The returned repository's Storer holds
// whatever packs have been ingested and the refs that were set from archived
// tips; it is not a worktree.
func OpenPrivateBare(repoDir string) (*git.Repository, error) {
	if _, err := os.Stat(filepath.Join(repoDir, "HEAD")); os.IsNotExist(err) {
		// The mirror does not exist yet: initialize an empty bare repo so show
		// can later ingest packs into a valid object database.
		if err := os.MkdirAll(repoDir, 0o700); err != nil {
			return nil, fmt.Errorf("gitarchive: create private mirror dir: %w", err)
		}
		repo, err := git.PlainInit(repoDir, true)
		if err != nil {
			return nil, fmt.Errorf("gitarchive: init private mirror: %w", err)
		}
		return repo, nil
	}
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		return nil, fmt.Errorf("gitarchive: open private mirror: %w", err)
	}
	return repo, nil
}

// RemoveArchiveRemote removes the "archive" remote from the work repo at repoPath
// IF that remote points at this locker's pinner URL (i.e. it is ours to remove).
// It returns (removed true, nil) when the remote was present and pointed at the
// locker URL, (removed false, nil) when the remote was absent or pointed
// elsewhere (left untouched), and an error only on a config read/write failure.
// Unwatch only removes a remote it can attribute to us — never a user's own
// remote that happens to share the name.
func RemoveArchiveRemote(repoPath, locker string) (bool, error) {
	want := GitRemoteURL(locker)
	repo, err := git.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return false, fmt.Errorf("gitarchive: open repo for unwatch: %w", err)
	}
	cfg, err := repo.Config()
	if err != nil {
		return false, fmt.Errorf("gitarchive: read config: %w", err)
	}
	remote, ok := cfg.Remotes[RemoteName]
	if !ok {
		return false, nil
	}
	// Only remove when the remote is unambiguously ours (single URL equal to
	// the canonical pinner URL for this locker).
	if len(remote.URLs) == 1 && remote.URLs[0] == want {
		delete(cfg.Remotes, RemoteName)
		if err := repo.Storer.SetConfig(cfg); err != nil {
			return false, fmt.Errorf("gitarchive: write config during unwatch: %w", err)
		}
		return true, nil
	}
	return false, nil
}

// RepairArchiveRemote ensures the work repo at repoPath carries the canonical
// "archive" remote pointing at locker's URL, fixing a missing or mispointed one.
// It returns (repaired bool, nil) where repaired reports whether a change was
// actually written. This backs `pinner git doctor`'s URL-repair pass.
func RepairArchiveRemote(repoPath, locker string) (bool, error) {
	want := GitRemoteURL(locker)
	repo, err := git.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return false, fmt.Errorf("gitarchive: open repo for repair: %w", err)
	}
	cfg, err := repo.Config()
	if err != nil {
		return false, fmt.Errorf("gitarchive: read config: %w", err)
	}
	if existing, ok := cfg.Remotes[RemoteName]; ok && len(existing.URLs) == 1 && existing.URLs[0] == want {
		return false, nil
	}
	cfg.Remotes[RemoteName] = &config.RemoteConfig{
		Name: RemoteName,
		URLs: []string{want},
	}
	if err := repo.Storer.SetConfig(cfg); err != nil {
		return false, fmt.Errorf("gitarchive: write config during repair: %w", err)
	}
	return true, nil
}
