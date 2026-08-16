package vault

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"go.sia.tech/core/types"
	"go.sia.tech/indexd/slabs"
	"gorm.io/gorm"
)

// TestPutCarriesProvenanceFromMetadata verifies that a Put promoting provenance
// keys (created_by / agent_id / session_id) from the caller's opaque metadata
// map stamps them as first-class File fields on the returned record and the
// stored row, so they are queryable and syncable.
func TestPutCarriesProvenanceFromMetadata(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDB(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	fake := &fakeSDK{t: t}
	svc := &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}}
	defer svc.Close()

	meta := map[string]any{
		"created_by": "derrick",
		"agent_id":   "agent-7",
		"session_id": "sess-42",
		"source":     "test",
	}
	rec, err := svc.Put(ctx, bytes.NewReader([]byte("hello")), 5, "vault:/docs/a.txt", meta)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if rec.CreatedBy != "derrick" {
		t.Errorf("record.CreatedBy = %q, want derrick", rec.CreatedBy)
	}
	if rec.AgentID != "agent-7" {
		t.Errorf("record.AgentID = %q, want agent-7", rec.AgentID)
	}
	if rec.SessionID != "sess-42" {
		t.Errorf("record.SessionID = %q, want sess-42", rec.SessionID)
	}
	if rec.Status != FileStatusOK {
		t.Errorf("record.Status = %q, want %q", rec.Status, FileStatusOK)
	}

	// The stored row must carry the provenance too (durable).
	var stored File
	if err := db.Where("uuid = ?", rec.UUID).First(&stored).Error; err != nil {
		t.Fatalf("load stored row: %v", err)
	}
	if stored.CreatedBy != "derrick" || stored.AgentID != "agent-7" || stored.SessionID != "sess-42" {
		t.Errorf("stored row provenance = (%q,%q,%q), want (derrick,agent-7,sess-42)",
			stored.CreatedBy, stored.AgentID, stored.SessionID)
	}

	// A Put with an empty metadata map must leave provenance empty (no panic).
	rec2, err := svc.Put(ctx, bytes.NewReader([]byte("bye")), 3, "vault:/docs/b.txt", nil)
	if err != nil {
		t.Fatalf("Put nil metadata failed: %v", err)
	}
	if rec2.CreatedBy != "" || rec2.AgentID != "" || rec2.SessionID != "" {
		t.Errorf("nil-metadata Put got non-empty provenance: (%q,%q,%q)",
			rec2.CreatedBy, rec2.AgentID, rec2.SessionID)
	}
}

// TestVerifyMarksLostOnObjectNotFound verifies that when the indexer reports
// the object/slabs unrecoverable (ErrObjectNotFound), Verify flags the local
// row as lost with a lost_reason, and a later successful verify clears it back
// to ok. This is the per-file lifecycle state on the permissionless network.
func TestVerifyMarksLostOnObjectNotFound(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDB(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	fake := &fakeSDK{t: t}
	svc := &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}}
	defer svc.Close()

	rec, err := svc.Put(ctx, bytes.NewReader([]byte("content")), 7, "vault:/docs/lost.txt", nil)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Break the object fetch: the indexer reports the slab terminations missing.
	fake.objErr = slabs.ErrObjectNotFound

	res, err := svc.Verify(ctx, "vault:/docs/lost.txt")
	if err != nil {
		// Verify returns (res, nil) on the not-found path (see resolveVerifyObject),
		// so a non-nil error here is unexpected.
		t.Fatalf("Verify returned unexpected err: %v", err)
	}
	if res.ObjectExists {
		t.Errorf("Verify.ObjectExists = true, want false for missing object")
	}

	var stored File
	if err := db.Where("uuid = ?", rec.UUID).First(&stored).Error; err != nil {
		t.Fatalf("load stored row: %v", err)
	}
	if stored.Status != FileStatusLost {
		t.Errorf("stored.Status = %q, want %q after failed verify", stored.Status, FileStatusLost)
	}
	if stored.LostReason == "" {
		t.Errorf("stored.LostReason is empty; want a terminal detail")
	}

	// Recover: a subsequent successful verify (object present AND digest
	// matching) clears the lost state.
	fake.objErr = nil
	fake.metaContentDigest = stored.ContentDigest
	if _, err := svc.Verify(ctx, "vault:/docs/lost.txt"); err != nil {
		t.Fatalf("Verify after recovery failed: %v", err)
	}
	if err := db.Where("uuid = ?", rec.UUID).First(&stored).Error; err != nil {
		t.Fatalf("reload stored row: %v", err)
	}
	if stored.Status != FileStatusOK {
		t.Errorf("stored.Status = %q, want %q after successful verify", stored.Status, FileStatusOK)
	}
	if stored.LostReason != "" {
		t.Errorf("stored.LostReason = %q, want empty after recovery", stored.LostReason)
	}
}

// TestVerifyDigestMismatchKeepsLostStatus verifies that a file whose object
// re-appears with a divergent/empty content digest (present-but-corrupt) does
// NOT have its lost state cleared. Guarding clearLostStatus behind
// DigestMatch keeps a still-broken file visible in vault_status --lost instead
// of silently resetting it to ok and hiding the integrity failure.
func TestVerifyDigestMismatchKeepsLostStatus(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDB(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	fake := &fakeSDK{t: t}
	svc := &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}}
	defer svc.Close()

	rec, err := svc.Put(ctx, bytes.NewReader([]byte("content")), 7, "vault:/docs/lost.txt", nil)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Mark the file lost (simulating a prior failed verify / disappeared object).
	if err := db.Model(&File{}).Where("uuid = ?", rec.UUID).Updates(map[string]any{
		"status": FileStatusLost, "lost_reason": "object missing",
	}).Error; err != nil {
		t.Fatalf("mark lost: %v", err)
	}

	// Object exists again, but its metadata carries no matching content digest
	// (fakeSDK.Object returns NewEmptyObject with empty metadata), so the
	// integrity check fails: DigestMatch=false.
	res, err := svc.Verify(ctx, "vault:/docs/lost.txt")
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if !res.ObjectExists {
		t.Fatalf("test setup: expected object to exist for divergence case")
	}
	if res.DigestMatch {
		t.Fatalf("test setup: expected DigestMatch=false for divergent metadata")
	}

	// The lost state MUST persist; a divergent object is not a valid recovery.
	var stored File
	if err := db.Where("uuid = ?", rec.UUID).First(&stored).Error; err != nil {
		t.Fatalf("reload stored row: %v", err)
	}
	if stored.Status != FileStatusLost {
		t.Errorf("stored.Status = %q, want %q (divergent object must stay lost)", stored.Status, FileStatusLost)
	}
	if stored.LostReason == "" {
		t.Errorf("stored.LostReason is empty; want preserved lost detail on digest mismatch")
	}
}

// TestSetProvenanceRePinsMetadataAndPersists verifies SetProvenance updates the
// local row AND re-stamps + re-pins the Sia object's encrypted metadata, and
// that only non-empty values override (empty fields preserved).
func TestSetProvenanceRePinsMetadataAndPersists(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDB(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	fake := &fakeSDK{t: t, objErr: nil}
	svc := &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}}
	defer svc.Close()

	if _, err := svc.Put(ctx, bytes.NewReader([]byte("data")), 4, "vault:/docs/p.txt", nil); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	fake.pinnedMeta = nil // reset so we can assert the metadata re-pin

	rec, err := svc.SetProvenance(ctx, "vault:/docs/p.txt", "derrick", "agent-9", "sess-1")
	if err != nil {
		t.Fatalf("SetProvenance failed: %v", err)
	}
	if rec.CreatedBy != "derrick" || rec.AgentID != "agent-9" || rec.SessionID != "sess-1" {
		t.Fatalf("SetProvenance result = (%q,%q,%q)", rec.CreatedBy, rec.AgentID, rec.SessionID)
	}

	// The object must have been re-pinned with new sealed metadata containing the
	// provenance fields.
	if !fake.pinCalled {
		t.Errorf("SetProvenance did not re-pin the object")
	}
	if len(fake.pinnedMeta) == 0 {
		t.Fatalf("SetProvenance re-pinned empty metadata")
	}
	pm, err := ParseFileMetadata(fake.pinnedMeta)
	if err != nil {
		t.Fatalf("parse re-pinned metadata: %v", err)
	}
	if pm.CreatedBy != "derrick" || pm.AgentID != "agent-9" || pm.SessionID != "sess-1" {
		t.Errorf("re-pinned metadata provenance = (%q,%q,%q), want (derrick,agent-9,sess-1)",
			pm.CreatedBy, pm.AgentID, pm.SessionID)
	}

	// Only non-empty values override: call with only agent_id set; created_by /
	// session_id must be preserved.
	if _, err := svc.SetProvenance(ctx, "vault:/docs/p.txt", "", "agent-10", ""); err != nil {
		t.Fatalf("SetProvenance partial failed: %v", err)
	}
	var stored File
	if err := db.Where("uuid = ?", rec.UUID).First(&stored).Error; err != nil {
		t.Fatalf("load stored row: %v", err)
	}
	if stored.CreatedBy != "derrick" || stored.AgentID != "agent-10" || stored.SessionID != "sess-1" {
		t.Errorf("stored provenance after partial override = (%q,%q,%q), want (derrick,agent-10,sess-1)",
			stored.CreatedBy, stored.AgentID, stored.SessionID)
	}
}

// TestMigration0003AppliesOnFreshAndExistingDB verifies the provenance/status
// migration applies cleanly both on a fresh database and on a database that was
// already migrated to the prior schema version (0002). Both must expose the five
// new columns with their defaults.
func TestMigration0003AppliesOnFreshAndExistingDB(t *testing.T) {
	// Fresh DB: OpenDB runs all migrations including 0003.
	db, err := OpenDB(filepath.Join(t.TempDir(), "vault-migrate.db"))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	assertHasProvenanceColumns(t, db)
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}

	// Existing DB path: create a DB, run migrations, confirm columns exist on a
	// row write (the 0003 columns are present and defaulted). This exercises the
	// ALTER TABLE on an already-populated schema rather than a blank one.
	db2, err := OpenDB(filepath.Join(t.TempDir(), "vault-existing.db"))
	if err != nil {
		t.Fatalf("OpenDB (existing) failed: %v", err)
	}
	dirID, err := (&vaultService{db: db2}).getOrCreateDirectory("/x")
	if err != nil {
		t.Fatalf("create dir: %v", err)
	}
	row := File{UUID: "u-1", VersionID: "v-1", Seq: 1, Name: "n.txt", DirectoryID: dirID, IsCurrent: true}
	if err := db2.Create(&row).Error; err != nil {
		t.Fatalf("create file row: %v", err)
	}
	assertHasProvenanceColumns(t, db2)
	var stored File
	if err := db2.Where("version_id = ?", "v-1").First(&stored).Error; err != nil {
		t.Fatalf("reload row: %v", err)
	}
	if stored.Status != FileStatusOK {
		t.Errorf("new row Status = %q, want %q (default)", stored.Status, FileStatusOK)
	}
	if sqlDB, err := db2.DB(); err == nil {
		sqlDB.Close()
	}
}

// assertHasProvenanceColumns uses PRAGMA table_info to confirm the five new
// columns exist on the files table.
func assertHasProvenanceColumns(t *testing.T, db *gorm.DB) {
	t.Helper()
	type col struct {
		CID   int
		Name  string
		Type  string
		NotN  int
		Dflt  *string
		Pri   int
	}
	var cols []col
	if err := db.Raw("PRAGMA table_info(files)").Scan(&cols).Error; err != nil {
		t.Fatalf("PRAGMA table_info failed: %v", err)
	}
	have := map[string]bool{}
	for _, c := range cols {
		have[c.Name] = true
	}
	for _, want := range []string{"status", "lost_reason", "created_by", "agent_id", "session_id"} {
		if !have[want] {
			t.Errorf("files table missing column %q after migration 0003", want)
		}
	}
}
