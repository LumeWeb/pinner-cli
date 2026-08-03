package vault

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.sia.tech/indexd/slabs"
	"gorm.io/gorm"
)

// isUniqueConflict reports whether err is a SQLite unique-constraint
// violation. Used by Sync so a duplicate root-name insert skips the event
// instead of aborting the whole loop.
func isUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	return strings.HasPrefix(err.Error(), "UNIQUE constraint")
}

// Sync pulls changes from the indexer into the local cache.
func (s *vaultService) Sync(ctx context.Context) (int, error) {
	// Load cursor
	var cursorRecord SyncDownCursor
	result := s.db.First(&cursorRecord)
	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		return 0, fmt.Errorf("failed to load sync cursor: %w", result.Error)
	}

	var cursor slabs.Cursor
	if cursorRecord.Cursor != "" {
		var err error
		cursor, err = unmarshalCursor(cursorRecord.Cursor)
		if err != nil {
			return 0, fmt.Errorf("failed to parse sync cursor: %w", err)
		}
	}

	// Fetch events
	events, err := s.sdk.ObjectEvents(ctx, cursor, 100)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch object events: %w", err)
	}

	count := len(events)
	// Index of the last event that was passed (deleted, recorded/updated, or
	// a passable no-op), the index of the FIRST transient skip that is
	// INTERLEAVED (a real object with empty/unparsable metadata following a
	// processed event), and whether any event was actually processed (deleted
	// or recorded/updated).
	//
	// Interleaved transient skips must NOT be passed over: if the cursor
	// advances past them they would be permanently invisible in the local
	// cache because the next fetch starts after the cursor. So the cursor
	// stops at (firstSkipped-1) so interleaved skips are retried next sync.
	//
	// A LEADING transient skip (empty/unparsable metadata before any processed
	// event) is unresolvable for the moment and there is nothing before it to
	// stop the cursor at; retrying it would livelock the whole batch. It is
	// therefore treated as a passable no-op the cursor advances past (like a
	// nil-Object event), so later events — including any interleaved skip —
	// are still handled correctly.
	//
	// A nil-Object, non-deletion event is always a passable no-op: it has no
	// object and is not a deletion, so there is never any metadata to resolve.
	lastProcessed := -1
	firstSkipped := -1
	// Seed seenProcessed from a pending interleaved skip carried over from the
	// previous batch: if the cursor was stopped before such a skip, the same
	// skip reappearing at the head of THIS batch must stay classified as an
	// interleaved retry, not be reclassified as a fresh leading skip (which
	// would drop it permanently before its metadata resolves).
	seenProcessed := cursorRecord.PendingSkip
	for i, ev := range events {
		if ev.Deleted {
			// Remove file by object key. A failed delete must not advance the
			// cursor: otherwise the stale local record would be permanently
			// orphaned (still visible in ls/stat although removed remotely)
			// and never re-processed. Return the error so the batch is aborted
			// and retried from before this event.
			if err := s.db.Where("object_key = ?", ev.Key.String()).Delete(&File{}).Error; err != nil {
				return count, fmt.Errorf("failed to delete file record: %w", err)
			}
			lastProcessed = i
			seenProcessed = true
			continue
		}
		if ev.Object == nil {
			// No object and not a deletion — a passable no-op that can never
			// yield file content. Advance the cursor past it instead of
			// stalling the batch on an unresolvable leading skip.
			lastProcessed = i
			continue
		}
		// Parse metadata from unsealed object
		rawMeta := ev.Object.Metadata()
		if len(rawMeta) == 0 {
			// Real object but no metadata yet — transient.
			if seenProcessed {
				// Interleaved after a processed event: stop the cursor before
				// it so it is retried once metadata appears.
				if firstSkipped < 0 {
					firstSkipped = i
				}
			} else {
				// Leading before any processed event: unresolvable for now,
				// pass over it so the batch makes forward progress.
				lastProcessed = i
			}
			continue
		}
		fileMeta, err := ParseFileMetadata(rawMeta)
		if err != nil {
			// Real object with unparsable metadata — transient.
			if seenProcessed {
				if firstSkipped < 0 {
					firstSkipped = i
				}
			} else {
				lastProcessed = i
			}
			continue
		}
		// Find existing by object key
		var existing File
		result := s.db.Where("object_key = ?", ev.Key.String()).First(&existing)
		if result.Error == gorm.ErrRecordNotFound {
			// New file from another client — place in root for MVP
			// (we can't determine its directory without path metadata)
			now := time.Now().UTC()
			existing = File{
				Name:          fileMeta.Name,
				DirectoryID:  nil,
				ObjectKey:     ev.Key.String(),
				Size:          fileMeta.Size,
				MediaType:     fileMeta.MediaType,
				ContentDigest: fileMeta.ContentDigest,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			if err := s.db.Create(&existing).Error; err != nil {
				// A duplicate name at root (from a different source
				// object) hits the unique index. Skip it rather than
				// aborting the remaining events — the local record
				// already reflects the first-seen file. A re-try would
				// only hit the same conflict, so this counts as
				// processed (the file is already tracked under another
				// object key).
				if isUniqueConflict(err) {
					lastProcessed = i
					seenProcessed = true
					continue
				}
				return count, fmt.Errorf("failed to create file record: %w", err)
			}
			lastProcessed = i
			seenProcessed = true
		} else if result.Error != nil {
			return count, fmt.Errorf("failed to query existing file: %w", result.Error)
		} else {
			// Update existing record with fresh metadata
			existing.Name = fileMeta.Name
			existing.Size = fileMeta.Size
			existing.MediaType = fileMeta.MediaType
			existing.ContentDigest = fileMeta.ContentDigest
			existing.UpdatedAt = ev.UpdatedAt
			if err := s.db.Save(&existing).Error; err != nil {
				return count, fmt.Errorf("failed to update file record: %w", err)
			}
			lastProcessed = i
			seenProcessed = true
		}
	}

	// If a skip is interleaved before the last processed event, stop at the
	// skip so it is retried, rather than the cursor jumping past it to a
	// later processed event. Leading skips need no special handling here:
	// they were advanced past as passable during the loop, so the cursor
	// stops at the last processed event before the first INTERLEAVED skip
	// without ever jumping to the end of the batch.
	if firstSkipped >= 0 && firstSkipped <= lastProcessed {
		lastProcessed = firstSkipped - 1
	}

	// An interleaved transient skip that is still present in this batch (and
	// unresolved) must be carried as pending so the next batch keeps treating
	// it as an interleaved retry rather than a droppable leading skip. If no
	// interleaved skip remains, clear the pending flag.
	pendingSkip := firstSkipped >= 0

	// Update cursor to the last successfully-processed event (capped before
	// any interleaved skipped event, or advanced past a leading skip), not
	// the last event in the batch, so skipped events are revisited on the
	// next sync.
	if lastProcessed >= 0 {
		last := events[lastProcessed]
		newCursor := cursor
		newCursor.After = last.UpdatedAt
		newCursor.Key = last.Key

		cursorJSON, err := marshalCursor(newCursor)
		if err != nil {
			return count, fmt.Errorf("failed to serialize cursor: %w", err)
		}

		if cursorRecord.ID == 0 {
			cursorRecord = SyncDownCursor{
				Cursor:      cursorJSON,
				PendingSkip: pendingSkip,
				UpdatedAt:   time.Now(),
			}
			s.db.Create(&cursorRecord)
		} else {
			cursorRecord.Cursor = cursorJSON
			cursorRecord.PendingSkip = pendingSkip
			cursorRecord.UpdatedAt = time.Now()
			s.db.Save(&cursorRecord)
		}
	} else if firstSkipped >= 0 {
		// lastProcessed < 0 while an interleaved skip is present means the
		// retried skip is at the head of the batch with nothing before it to
		// rewind to (seeded from a carried-over pending skip). Do not advance
		// the cursor — wait for the skip to resolve — but persist the pending
		// flag so the retry intent survives across this batch.
		if cursorRecord.ID == 0 {
			cursorRecord = SyncDownCursor{
				Cursor:      cursorRecord.Cursor,
				PendingSkip: true,
				UpdatedAt:   time.Now(),
			}
			s.db.Create(&cursorRecord)
		} else {
			cursorRecord.PendingSkip = true
			cursorRecord.UpdatedAt = time.Now()
			s.db.Save(&cursorRecord)
		}
	} else if cursorRecord.PendingSkip {
		// Nothing was processed and no interleaved skip is present this batch
		// (e.g. an empty batch, or the carried-over pending object was removed
		// without a delete event). Clear the stale flag so it does not keep
		// reclassifying a genuinely-leading transient skip as an interleaved
		// retry, which would stall sync indefinitely.
		cursorRecord.PendingSkip = false
		cursorRecord.UpdatedAt = time.Now()
		s.db.Save(&cursorRecord)
	}

	return count, nil
}

// SyncCursor returns the persisted sync cursor token (the raw JSON string), or
// "" if none has been saved yet. Callers that loop over Sync (e.g. `vault
// cache rebuild`) can compare it across iterations to detect when the cursor
// stops advancing — Sync returns the full batch size even when it holds the
// cursor before an unresolved transient-metadata skip, so count alone cannot
// distinguish forward progress from a stall.
func (s *vaultService) SyncCursor() string {
	var cursorRecord SyncDownCursor
	if err := s.db.First(&cursorRecord).Error; err != nil {
		return ""
	}
	return cursorRecord.Cursor
}
