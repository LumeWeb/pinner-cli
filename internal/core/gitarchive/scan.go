package gitarchive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.sia.tech/siastorage"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"go.lumeweb.com/pinner-cli/internal/core/objmeta"
)

// IngestCard records one git-archive card (already recognized at the objmeta
// layer) into the local git_objects table. It returns (true, nil) when the
// metadata was a git card that was ingested, (false, nil) when it was NOT a
// git card (a vault file or anything else) and so was ignored, and a non-nil
// error only on a local DB failure.
//
// IngestCard intentionally writes ONLY to git_objects/git_repos; it never
// touches the vault File table, so scanning git cards can never produce vault
// File rows.
func IngestCard(db *gorm.DB, objectKey string, raw []byte) (bool, error) {
	card, err := objmeta.Decode(raw)
	if err != nil {
		// Not a pinner card at all (including legacy vault FileMetadata, or an
		// unparseable/oversize payload): not ours to ingest.
		if errors.Is(err, objmeta.ErrNotCard) || errors.Is(err, objmeta.ErrUnknownKind) {
			return false, nil
		}
		return false, nil
	}

	var body datatypes.JSON
	if len(card.Body) > 0 {
		b, jerr := json.Marshal(card.Body)
		if jerr == nil {
			body = datatypes.JSON(b)
		}
	}

	now := time.Now().UTC()
	obj := GitObject{
		ObjectKey: objectKey,
		Locker:    lockerFromCard(card),
		Kind:      string(card.Kind),
		Size:      card.Size,
		Digest:    card.Digest,
		Created:   card.Created,
		Body:      body,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Idempotent upsert keyed by object key.
	if err := db.Where("object_key = ?", objectKey).Assign(map[string]any{
		"locker":     obj.Locker,
		"kind":       obj.Kind,
		"size":       obj.Size,
		"digest":     obj.Digest,
		"created":    obj.Created,
		"body":       obj.Body,
		"updated_at": now,
	}).FirstOrCreate(&obj).Error; err != nil {
		return true, fmt.Errorf("gitarchive: ingest card %s: %w", objectKey, err)
	}
	return true, nil
}

// lockerFromCard extracts the locker/name from a card body for indexing. It is
// best-effort: bodies vary by kind (pack/tip/share all carry a locker).
func lockerFromCard(c *objmeta.Card) string {
	var probe struct {
		Locker string `json:"locker"`
	}
	if len(c.Body) > 0 {
		_ = json.Unmarshal(c.Body, &probe)
	}
	return probe.Locker
}

// Scan walks object events and ingests any pinner git cards found into
// git_objects. Non-card events (including vault files) are skipped. It returns
// the number of git cards ingested. This is the gitarchive parallel to
// vault.Sync: it only owns git-kind objects and never writes File rows.
func Scan(ctx context.Context, db *gorm.DB, events []siastorage.ObjectEvent) (int, error) {
	ingested := 0
	for _, ev := range events {
		if err := ctx.Err(); err != nil {
			return ingested, err
		}
		if ev.Deleted || ev.Object == nil {
			continue
		}
		meta := ev.Object.Metadata()
		if len(meta) == 0 {
			continue
		}
		if !objmeta.IsCard(meta) {
			continue // vault files and unknown metadata are not git cards
		}
		ok, err := IngestCard(db, ev.Key.String(), meta)
		if err != nil {
			return ingested, err
		}
		if ok {
			ingested++
		}
	}
	return ingested, nil
}
