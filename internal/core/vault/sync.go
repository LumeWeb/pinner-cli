package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.sia.tech/indexd/slabs"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// isLiveNameConflict reports whether err is the SQLite unique-constraint
// violation fired by idx_files_live_name_dir: the partial unique index that
// atomically allows at most one LIVE file per (name, directory). Put uses this
// to detect that a concurrent writer won a path and to re-resolve rather than
// insert a duplicate live row.
func isLiveNameConflict(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "idx_files_live_name_dir") &&
		strings.Contains(err.Error(), "UNIQUE")
}

// isDirNameConflict reports whether err is the SQLite unique-constraint
// violation fired on directories.path (the unique INDEX idx_directories_path).
// go-sqlite3 reports column-level UNIQUE constraints as "UNIQUE constraint
// failed: directories.path" (columns, not the index name), so we match on that.
// getOrCreateDirectory uses it to detect that a concurrent writer created the
// same directory between its check and insert, and re-resolves instead of
// failing the whole op (the same conflict semantics Put uses for files).
func isDirNameConflict(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed: directories.path")
}

// Sync pulls changes from the indexer into the local cache.
//
// It consumes ObjectEvents, applies each to the local store idempotently, and
// ALWAYS advances the cursor to the last event in the batch. Events whose
// metadata is missing/malformed are skipped (the cursor still advances past
// them); they are re-processed on a later sync pass once the uploading device
// finishes stamping metadata, because the store is an idempotent upsert keyed
// by the per-file UUID. Skips are deliberately not stalled or retried; the
// engine relies on periodic re-runs.
func (s *vaultService) Sync(ctx context.Context) (applied int, full bool, err error) {
	// Load cursor
	var cursorRecord SyncDownCursor
	result := s.db.First(&cursorRecord)
	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		return 0, false, fmt.Errorf("failed to load sync cursor: %w", result.Error)
	}

	var cursor slabs.Cursor
	if cursorRecord.Cursor != "" {
		var err error
		cursor, err = unmarshalCursor(cursorRecord.Cursor)
		if err != nil {
			return 0, false, fmt.Errorf("failed to parse sync cursor: %w", err)
		}
	}

	// Sync talks to the indexer, so it needs the (lazily-built) SDK.
	sdk, err := s.ensureSDK()
	if err != nil {
		return 0, false, fmt.Errorf("failed to create Sia SDK: %w", err)
	}

	// Fetch events
	events, err := sdk.ObjectEvents(ctx, cursor, 100)
	if err != nil {
		return 0, false, fmt.Errorf("failed to fetch object events: %w", err)
	}

	// A full batch means there may be more events behind the cursor; callers
	// use this to decide whether to loop. The applied count alone is
	// insufficient: a batch that is entirely skips returns 0 applied but still
	// advances the cursor past real events.
	full = len(events) == 100

	// Apply each event idempotently. applied counts the events actually
	// written to the local store (deleted/tombstoned or recorded/updated).
	applied = 0
	for _, ev := range events {
		if ev.Deleted {
			// Soft-delete the local row(s) this delete event refers to. Setting
			// deleted_at (rather than hard-deleting) keeps the record
			// recoverable if the object is re-uploaded out of order, and lets a
			// later sync resurrect it without a full rebuild.
			//
			// An object-key delete means the content-addressed object no longer
			// exists remotely, so EVERY live current alias referencing it is
			// gone; tombstone them all. Historical versions (is_current=0) and
			// already-tombstoned rows are preserved. When the delete event
			// carries file metadata with a per-file UUID, disambiguate to that
			// exact row first so a shared (deduplicated) key clears only the
			// deleted file, not unrelated live aliases.
			now := time.Now().UTC()
			if ev.Object != nil {
				if rawMeta := ev.Object.Metadata(); len(rawMeta) > 0 {
					if m, merr := ParseFileMetadata(rawMeta); merr == nil && m.ID != "" {
						// The metadata names a specific file: tombstone exactly that
						// row. If no live current row matches that UUID (e.g. a
						// legacy object, or a bare content-address carryover), fall
						// through to the object_key-based tombstone below.
						res := s.db.Model(&File{}).
							Where("uuid = ? AND is_current = 1", m.ID).
							Update("deleted_at", now)
						if res.Error != nil {
							return applied, full, fmt.Errorf("failed to tombstone file record: %w", res.Error)
						}
						if res.RowsAffected > 0 {
							applied++
							continue
						}
					}
				}
			}
			if err := s.db.Model(&File{}).
				Where("object_key = ? AND is_current = 1 AND deleted_at IS NULL", ev.Key.String()).
				Update("deleted_at", now).Error; err != nil {
				return applied, full, fmt.Errorf("failed to tombstone file record: %w", err)
			}
			applied++
			continue
		}
		if ev.Object == nil {
			// No object and not a deletion; nothing to record. The cursor
			// advances past it.
			continue
		}
		// Parse metadata from unsealed object. Missing/malformed metadata is a
		// transient state (the uploading device may not have finished stamping
		// it); skip it and rely on the next sync re-tick to re-process.
		rawMeta := ev.Object.Metadata()
		if len(rawMeta) == 0 {
			continue
		}
		fileMeta, err := ParseFileMetadata(rawMeta)
		if err != nil {
			continue
		}

		// Resolve the file's stable identity. New clients stamp a UUID into the
		// object metadata; for legacy objects that lack one we derive a stable
		// ID from the content key so the same object maps to the same row across
		// syncs (never a random UUID that would duplicate rows on each run).
		fileID := fileMeta.ID
		if fileID == "" {
			fileID = "obj-" + ev.Key.String()
		}
		// Version identity (decision #2 — key sync on (UUID, version_id) so
		// multi-version history is consistent across devices). New clients stamp
		// version_id/seq into object metadata; legacy objects have neither and
		// resolve to the null version (version_id="", seq=0).
		versionID := fileMeta.VersionID
		versionSeq := fileMeta.Seq
		if versionID == "" {
			versionID = ""
		}

		// Resolve the object's target directory from its metadata so files
		// uploaded to a nested path on one device sync into the same directory
		// hierarchy on another (root = nil). resolveVaultDirectory uses the
		// service DB (not the tx), so it must run before the transaction opens.
		dirID, err := resolveVaultDirectory(s.db, fileMeta.Directory)
		if err != nil {
			return applied, full, fmt.Errorf("failed to resolve directory for %s: %w", ev.Key.String(), err)
		}

		// Find existing by (stable identity, version). Identity is NOT the name:
		// two distinct content-addressed objects may share a name, and each
		// version of a logical file is its own row keyed by (UUID, version_id).
		// A rename is a metadata update on the same (UUID, version_id) row; a
		// new content version syncs in as a NEW row under the same UUID.
		//
		// The write and the is_current promotion happen atomically so the
		// partial unique index idx_files_live_name_dir keeps exactly one current
		// live winner per path even as sync applies out-of-order events.
		txErr := s.db.Transaction(func(tx *gorm.DB) error {
			var existing File
			result := tx.Where("uuid = ? AND version_id = ?", fileID, versionID).First(&existing)
			if result.Error == gorm.ErrRecordNotFound {
				// New version not yet tracked; create its own row keyed by
				// (UUID, version_id). It starts non-current so a second
				// distinct row (different version/object) with the same name
				// coexists without violating idx_files_live_name_dir;
				// promoteCurrent below then makes THIS object the new winner and
				// demotes the prior one. No distinct version is ever dropped.
				now := time.Now().UTC()
				var metaJSON datatypes.JSON
				if fileMeta.Metadata != nil {
					// Persist the user metadata carried in the object's
					// FileMetadata on the newly-created row (same behavior as
					// upsertFromMeta and Put) so Stat returns it immediately on a
					// fresh-cache sync rather than only after an overwrite.
					if b, jerr := json.Marshal(fileMeta.Metadata); jerr == nil {
						metaJSON = datatypes.JSON(b)
					}
				}
				existing = File{
					UUID:          fileID,
					VersionID:     versionID,
					Seq:           versionSeq,
					Name:          fileMeta.Name,
					DirectoryID:   dirID, // resolved from object metadata (root = nil)
					IsCurrent:     false,
					ObjectKey:     ev.Key.String(),
					Size:          fileMeta.Size,
					MediaType:     fileMeta.MediaType,
					ContentDigest: fileMeta.ContentDigest,
					Metadata:      metaJSON,
					CreatedAt:     now,
					UpdatedAt:     now,
				}
				if err := tx.Create(&existing).Error; err != nil {
					// A unique-index collision (two distinct objects claiming the
					// same live name) is an app-layer namespace condition, not a
					// network error: keep the first-seen binding and drop the new
					// claim, then continue syncing (first-seen policy).
					if isLiveNameConflict(err) {
						return nil
					}
					return err
				}
			} else if result.Error != nil {
				return result.Error
			} else {
				if err := upsertFromMeta(tx, &existing, fileMeta, ev.Key.String(), ev.UpdatedAt, dirID); err != nil {
					return err
				}
			}
			return promoteCurrent(tx, existing.Name, existing.DirectoryID, existing.ID)
		})
		if txErr != nil {
			return applied, full, fmt.Errorf("failed to record sync event: %w", txErr)
		}
		applied++
	}

	// Always advance the cursor to the last event in the batch. Skips don't
	// hold it; a later pass re-processes them and the idempotent upsert makes
	// that safe.
	if len(events) > 0 {
		last := events[len(events)-1]
		newCursor := cursor
		newCursor.After = last.UpdatedAt
		newCursor.Key = last.Key

		cursorJSON, err := marshalCursor(newCursor)
		if err != nil {
			return applied, full, fmt.Errorf("failed to serialize cursor: %w", err)
		}

		if cursorRecord.ID == 0 {
			cursorRecord = SyncDownCursor{
				Cursor:    cursorJSON,
				UpdatedAt: time.Now(),
			}
			if err := s.db.Create(&cursorRecord).Error; err != nil {
				return applied, full, fmt.Errorf("failed to persist sync cursor: %w", err)
			}
		} else {
			cursorRecord.Cursor = cursorJSON
			cursorRecord.UpdatedAt = time.Now()
			if err := s.db.Save(&cursorRecord).Error; err != nil {
				return applied, full, fmt.Errorf("failed to persist sync cursor: %w", err)
			}
		}
	}

	return applied, full, nil
}

// Sync's internal cursor persistence. The persisted SyncDownCursor token is
// only consumed internally by Sync; callers that need to confirm forward
// progress (e.g. cache rebuild) compare the cursor across iterations via the
// SyncDownCursor row.
