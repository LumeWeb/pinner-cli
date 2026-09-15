package gitarchive

import (
	"fmt"

	"gorm.io/gorm"
)

// RemoteName is the fixed remote name `pinner git watch` installs pointing at
// the archive locker.
const RemoteName = "archive"

// GitRemoteURL renders the pinner remote URL for a locker (the value installed
// as Git's "archive" remote and used by the git-remote-pinner helper).
func GitRemoteURL(locker string) string {
	return "pinner::" + locker
}

// EnsureRepo upserts the git_repos row for locker, recording its display name
// and lineage fingerprint. When a row already exists it is updated in place and
// returned.
func EnsureRepo(db *gorm.DB, locker, name, lineage string) (GitRepo, error) {
	var repo GitRepo
	err := db.Where("locker = ?", locker).First(&repo).Error
	if err == nil {
		repo.Name = name
		repo.Lineage = lineage
		if err := db.Save(&repo).Error; err != nil {
			return GitRepo{}, fmt.Errorf("gitarchive: update repo %q: %w", locker, err)
		}
		return repo, nil
	}
	if err != gorm.ErrRecordNotFound {
		return GitRepo{}, fmt.Errorf("gitarchive: find repo %q: %w", locker, err)
	}
	repo = GitRepo{Locker: locker, Name: name, Lineage: lineage}
	if err := db.Create(&repo).Error; err != nil {
		return GitRepo{}, fmt.Errorf("gitarchive: create repo %q: %w", locker, err)
	}
	return repo, nil
}

// FindReposByLineage returns every locker whose recorded lineage matches key,
// ordered by locker name. Returning more than one entry signals an ambiguous
// lineage match — the caller should ask the user to disambiguate with --locker
// or force a new locker with --new.
func FindReposByLineage(db *gorm.DB, key string) ([]GitRepo, error) {
	var repos []GitRepo
	if err := db.Where("lineage = ?", key).Order("locker").Find(&repos).Error; err != nil {
		return nil, fmt.Errorf("gitarchive: match lineage: %w", err)
	}
	return repos, nil
}

// AddBind records that the repo at repoPath is bound to locker under
// RemoteName, upserting on repeated binds of the same locker.
func AddBind(db *gorm.DB, locker, repoPath string) (GitBind, error) {
	bind := GitBind{Locker: locker, Remote: RemoteName, RepoPath: repoPath}
	var existing GitBind
	err := db.Where("locker = ?", locker).First(&existing).Error
	if err == nil {
		existing.RepoPath = repoPath
		existing.Remote = RemoteName
		if err := db.Save(&existing).Error; err != nil {
			return GitBind{}, fmt.Errorf("gitarchive: update bind %q: %w", locker, err)
		}
		return existing, nil
	}
	if err != gorm.ErrRecordNotFound {
		return GitBind{}, fmt.Errorf("gitarchive: find bind %q: %w", locker, err)
	}
	if err := db.Create(&bind).Error; err != nil {
		return GitBind{}, fmt.Errorf("gitarchive: create bind %q: %w", locker, err)
	}
	return bind, nil
}

// FindBindByRepo returns the bind (and thus the locker + remote) for a local
// repo path, or nil when the path is not bound to any archive.
func FindBindByRepo(db *gorm.DB, repoPath string) (*GitBind, error) {
	var bind GitBind
	err := db.Where("repo_path = ?", repoPath).First(&bind).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("gitarchive: find bind %q: %w", repoPath, err)
	}
	return &bind, nil
}

// ListRepos returns every tracked locker (git_repos rows) ordered by locker
// name. This backs `pinner git ls`.
func ListRepos(db *gorm.DB) ([]GitRepo, error) {
	var repos []GitRepo
	if err := db.Order("locker").Find(&repos).Error; err != nil {
		return nil, fmt.Errorf("gitarchive: list repos: %w", err)
	}
	return repos, nil
}

// DeleteBind removes the git_binds row for repoPath, returning (true, nil) when
// a bind existed and was removed, and (false, nil) when repoPath was not bound.
// It backs `pinner git unwatch` (the local bookkeeping half).
func DeleteBind(db *gorm.DB, repoPath string) (bool, error) {
	var bind GitBind
	err := db.Where("repo_path = ?", repoPath).First(&bind).Error
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("gitarchive: find bind %q: %w", repoPath, err)
	}
	if err := db.Delete(&bind).Error; err != nil {
		return false, fmt.Errorf("gitarchive: delete bind %q: %w", repoPath, err)
	}
	return true, nil
}

// DeleteRepoByLocker removes the git_repos row for locker, returning
// (true, nil) when a row existed and was removed. It backs `pinner git unwatch`
// cleanup so an unwatched locker no longer appears in `pinner git ls`.
func DeleteRepoByLocker(db *gorm.DB, locker string) (bool, error) {
	var repo GitRepo
	err := db.Where("locker = ?", locker).First(&repo).Error
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("gitarchive: find repo %q: %w", locker, err)
	}
	if err := db.Delete(&repo).Error; err != nil {
		return false, fmt.Errorf("gitarchive: delete repo %q: %w", locker, err)
	}
	return true, nil
}
