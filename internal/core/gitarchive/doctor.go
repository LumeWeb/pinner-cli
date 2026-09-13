package gitarchive

import (
	"fmt"

	"gorm.io/gorm"
)

// This file holds the local, DB-only health checks powering `pinner git doctor`.
// Sia-dependent passes (account scan, pack download, republish) live behind the
// Store/session seam and are wired at the CLI layer; here we verify the local
// bookkeeping invariants that doctor can always check without a network round
// trip: the bind, the locker registration, the archived tip generation, and
// whether the git_objects table is cold for the locker.

// DoctorReport is the result of a local git archive health check.
type DoctorReport struct {
	// Bound reports whether the repo is bound to a locker.
	Bound bool
	// Locker is the bound locker name (empty when not bound).
	Locker string
	// RepoRegistered reports whether a git_repos row exists for the locker.
	RepoRegistered bool
	// TipGen is the registered tip generation for the locker.
	TipGen int
	// ObjectCount is the number of git_objects rows for the locker.
	ObjectCount int64
	// Cold reports whether no git_objects rows exist for the locker (a fresh,
	// never-scanned or never-published locker).
	Cold bool
	// HasTipObject reports whether at least one git.tip object row exists.
	HasTipObject bool
	// HasPackObject reports whether at least one git.pack object row exists.
	HasPackObject bool
}

// LocalDoctor runs the local (DB-only) portion of `pinner git doctor` for the
// repo at repoPath. It reports whether the repo is bound and the health of the
// locker registration and git_objects rows, without touching the network. The
// Sia-dependent passes are the CLI layer's responsibility (via the Store).
func LocalDoctor(db *gorm.DB, repoPath string) (*DoctorReport, error) {
	report := &DoctorReport{}

	bind, err := FindBindByRepo(db, repoPath)
	if err != nil {
		return nil, err
	}
	if bind == nil {
		return report, nil // not bound: nothing else to check locally
	}
	report.Bound = true
	report.Locker = bind.Locker

	var repo GitRepo
	err = db.Where("locker = ?", bind.Locker).First(&repo).Error
	switch {
	case err == gorm.ErrRecordNotFound:
		report.RepoRegistered = false
	case err != nil:
		return nil, fmt.Errorf("gitarchive: find repo %q: %w", bind.Locker, err)
	default:
		report.RepoRegistered = true
		report.TipGen = repo.TipGen
	}

	if err := db.Model(&GitObject{}).
		Where("locker = ?", bind.Locker).
		Count(&report.ObjectCount).Error; err != nil {
		return nil, fmt.Errorf("gitarchive: count objects %q: %w", bind.Locker, err)
	}
	report.Cold = report.ObjectCount == 0

	var tipN, packN int64
	if err := db.Model(&GitObject{}).
		Where("locker = ? AND kind = ?", bind.Locker, "git.tip").
		Count(&tipN).Error; err != nil {
		return nil, fmt.Errorf("gitarchive: count tip objects %q: %w", bind.Locker, err)
	}
	if err := db.Model(&GitObject{}).
		Where("locker = ? AND kind = ?", bind.Locker, "git.pack").
		Count(&packN).Error; err != nil {
		return nil, fmt.Errorf("gitarchive: count pack objects %q: %w", bind.Locker, err)
	}
	report.HasTipObject = tipN > 0
	report.HasPackObject = packN > 0

	return report, nil
}
