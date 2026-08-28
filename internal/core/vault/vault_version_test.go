package vault

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"go.sia.tech/core/types"
	"go.sia.tech/indexd/api/app"
	"go.sia.tech/indexd/slabs"
	"go.sia.tech/siastorage"
)

// versionSDK is a content-serving fake sdkClient. It does two jobs:
//
//  1. It records every byte stream passed to Upload (in order) so a test can
//     assert exactly what content was (re-)uploaded.
//  2. It SERVES content back from Download, keyed by the object hash that the
//     service passes to Object(). This lets VersionDownload/VersionRestore
//     actually retrieve a specific version's bytes, proving restore re-uploads
//     the OLD version's content rather than inventing fresh bytes.
//
// The service derives a File row's ObjectKey from siastorage.Object.ID(), which
// is the hash of the (never-populated) slab list, so real Put calls all stamp
// rows with the same empty-slab key. To give distinct version rows distinct,
// content-serving object keys, a test registers explicit content under an
// explicit object hash (via registerContent) and seeds File rows whose
// ObjectKey is that hash's hex string — the same "seed a known row" pattern the
// other *_test.go files already use. Object() hands the requested hash to
// Download() so the right content is served.
type versionSDK struct {
	t   *testing.T
	mu  sync.Mutex
	objs map[types.Hash256][]byte // object hash -> its uploaded content

	lastObjectHash types.Hash256 // hash passed to the most recent Object() call
	uploads        [][]byte      // every byte stream Upload received, in order
	downloads      int           // number of Download calls served
	downloadErr    error         // injected error to fail Download mid-read (nil = serve normally)
}

func newVersionSDK(t *testing.T) *versionSDK {
	return &versionSDK{t: t, objs: make(map[types.Hash256][]byte)}
}

// registerContent makes Download serve `content` for the object whose hash is
// `h`. Use the returned hex string as a seeded File row's ObjectKey.
func (f *versionSDK) registerContent(h types.Hash256, content []byte) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objs[h] = content
	return h.String()
}

func (f *versionSDK) Account(_ context.Context) (app.AccountResponse, error) {
	return app.AccountResponse{}, nil
}
func (f *versionSDK) AppKey() types.PrivateKey {
	return types.PrivateKey{}
}
func (f *versionSDK) Upload(_ context.Context, _ *siastorage.Object, r io.Reader, _ ...siastorage.UploadOption) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads = append(f.uploads, b)
	return nil
}
func (f *versionSDK) PinObject(_ context.Context, _ siastorage.Object) error { return nil }
func (f *versionSDK) Object(_ context.Context, hash types.Hash256) (siastorage.Object, error) {
	f.mu.Lock()
	f.lastObjectHash = hash
	f.mu.Unlock()
	return siastorage.NewEmptyObject(), nil
}
func (f *versionSDK) ObjectEvents(_ context.Context, _ slabs.Cursor, _ int) ([]siastorage.ObjectEvent, error) {
	return nil, nil
}
func (f *versionSDK) Download(_ siastorage.Object, _ ...siastorage.DownloadOption) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloads++
	content := f.objs[f.lastObjectHash]
	if f.downloadErr != nil {
		// Serve a reader that fails mid-read, simulating a truncated/failed
		// historical-object download (some bytes then an error).
		return io.NopCloser(&partialErrReader{Reader: bytes.NewReader(content), Err: f.downloadErr}), nil
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}
func (f *versionSDK) DeleteObject(_ context.Context, _ types.Hash256) error { return nil }
func (f *versionSDK) CreateSharedObjectURL(_ context.Context, _ types.Hash256, _ time.Time) (string, error) {
	return "", nil
}
func (f *versionSDK) DownloadSharedObject(_ context.Context, _ string, _ ...siastorage.DownloadOption) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (f *versionSDK) Close() error { return nil }

// lastUpload returns the bytes of the most recent Upload call.
func (f *versionSDK) lastUpload() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.uploads) == 0 {
		return nil
	}
	return f.uploads[len(f.uploads)-1]
}

// openVaultTestService opens an isolated SQLite DB and builds a real vaultService
// wired to the given SDK, mirroring the construction pattern used by the other
// internal/core/vault tests (OpenDB + &vaultService{db, sdk, appKey}).
func openVaultTestService(t *testing.T, sdk sdkClient) (*vaultService, *gorm.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return &vaultService{
		db:     db,
		sdk:    sdk,
		appKey: types.PrivateKey{},
	}, db
}

// TestVersion_OverwriteCreatesMultipleVersions proves the P0 versioning core
// end to end against a real service + fake SDK:
//   - N overwrites of the same path create exactly N live version rows;
//   - VersionList returns them newest-first (descending seq);
//   - exactly one row has IsCurrent=true, and it is the max-seq row;
//   - VersionGet(versionID) returns that exact record.
func TestVersion_OverwriteCreatesMultipleVersions(t *testing.T) {
	ctx := context.Background()
	const path = "vault:/docs/report.txt"
	const n = 4

	fake := newVersionSDK(t)
	svc, _ := openVaultTestService(t, fake)

	// (1) Multiple overwrites create multiple version rows. Every Put to the
	// same path mints a NEW version row (new version_id, higher seq) and
	// promotes it to current, preserving prior rows.
	versionIDs := make([]string, n)
	contents := make([][]byte, n)
	for i := 0; i < n; i++ {
		contents[i] = []byte("content-v" + string(rune('0'+i)))
		rec, err := svc.Put(ctx, bytes.NewReader(contents[i]), int64(len(contents[i])), path, nil)
		if err != nil {
			t.Fatalf("Put #%d failed: %v", i, err)
		}
		versionIDs[i] = rec.VersionID
		if rec.VersionID == "" {
			t.Fatalf("Put #%d produced empty version_id", i)
		}
		for j := 0; j < i; j++ {
			if versionIDs[j] == versionIDs[i] {
				t.Fatalf("version %d and %d share version_id %q", j, i, versionIDs[i])
			}
		}
	}

	// (2) VersionList returns exactly N live version rows, newest-first.
	list, err := svc.VersionList(ctx, path)
	if err != nil {
		t.Fatalf("VersionList failed: %v", err)
	}
	if len(list) != n {
		t.Fatalf("VersionList returned %d rows, want %d (one per overwrite)", len(list), n)
	}

	// (3) VersionGet on each specific version_id returns that exact record.
	for i := 0; i < n; i++ {
		got, err := svc.VersionGet(ctx, path, versionIDs[i])
		if err != nil {
			t.Fatalf("VersionGet(%q) failed: %v", versionIDs[i], err)
		}
		if got.VersionID != versionIDs[i] {
			t.Errorf("VersionGet returned version_id %q, want %q", got.VersionID, versionIDs[i])
		}
		if got.Name != "report.txt" {
			t.Errorf("VersionGet(%d) Name = %q, want report.txt", i, got.Name)
		}
	}

	// Mutation-test the core property: exactly one current, and it is the
	// max-seq (newest) row.
	var currents int
	maxSeq := uint(0)
	for _, f := range list {
		if f.IsCurrent {
			currents++
		}
		if f.Seq > maxSeq {
			maxSeq = f.Seq
		}
	}
	if currents != 1 {
		t.Fatalf("expected exactly 1 IsCurrent row after %d writes, got %d", n, currents)
	}
	if list[0].IsCurrent != true {
		t.Errorf("newest-first list[0] should be current, got IsCurrent=%v", list[0].IsCurrent)
	}
	if list[0].Seq != maxSeq {
		t.Errorf("newest-first list[0].Seq = %d, want max-seq %d", list[0].Seq, maxSeq)
	}

	// Rows are strictly newest-first by seq.
	for i := 1; i < len(list); i++ {
		if list[i].Seq >= list[i-1].Seq {
			t.Errorf("list not newest-first at %d: seq %d should be < %d", i, list[i].Seq, list[i-1].Seq)
		}
	}

	// The uploader recorded exactly N content uploads, one per Put.
	if len(fake.uploads) != n {
		t.Fatalf("fake SDK recorded %d uploads, want %d", len(fake.uploads), n)
	}
}

// TestVersion_RestoreReuploadsOldContentAsNewVersion proves VersionRestore
// works end to end: it RETRIEVES an old version's content through the real
// VersionDownload -> SDK.Download path, re-uploads it via Put as a NEW current
// version (a brand-new version_id), and preserves every prior version row.
func TestVersion_RestoreReuploadsOldContentAsNewVersion(t *testing.T) {
	ctx := context.Background()
	const path = "vault:/docs/doc.txt"

	fake := newVersionSDK(t)
	svc, db := openVaultTestService(t, fake)

	// Seed a versioned history directly (the same "known row" pattern the other
	// *_test.go files use): two prior versions, each with a DISTINCT object key
	// whose content the fake serves back. The oldest ("v1") is what we restore.
	dirID, err := svc.getOrCreateDirectory("/docs")
	if err != nil {
		t.Fatalf("getOrCreateDirectory: %v", err)
	}

	keyV1 := fake.registerContent(mustHash(t, "00000000000000000000000000000000000000000000000000000000000000aa"), []byte("v1 content"))
	keyV2 := fake.registerContent(mustHash(t, "00000000000000000000000000000000000000000000000000000000000000bb"), []byte("v2 content"))

	c1 := File{UUID: "uuid-doc", Name: "doc.txt", DirectoryID: dirID, IsCurrent: false,
		VersionID: "version-v1", Seq: 1, ObjectKey: keyV1, Size: int64(len("v1 content"))}
	c2 := File{UUID: "uuid-doc", Name: "doc.txt", DirectoryID: dirID, IsCurrent: true,
		VersionID: "version-v2", Seq: 2, ObjectKey: keyV2, Size: int64(len("v2 content"))}
	for _, f := range []File{c1, c2} {
		if err := db.Create(&f).Error; err != nil {
			t.Fatalf("seed version row: %v", err)
		}
	}

	// (4) Restore the OLD version's content (v1) as a NEW current version.
	restored, err := svc.VersionRestore(ctx, path, "version-v1")
	if err != nil {
		t.Fatalf("VersionRestore failed: %v", err)
	}

	// Restore minted a brand-new current version (new version_id, higher seq).
	if restored.VersionID == "" {
		t.Fatal("restored version has empty version_id")
	}
	if restored.VersionID == "version-v1" || restored.VersionID == "version-v2" {
		t.Fatalf("restored version reused an existing version_id %q, must be new", restored.VersionID)
	}
	if restored.Seq <= c2.Seq {
		t.Errorf("restored seq %d should be > prior newest seq %d", restored.Seq, c2.Seq)
	}

	// NOTE: Put/VersionRestore return the in-memory record with IsCurrent=false
	// (promoteCurrent flips is_current in the DB, not on the returned struct).
	// Currentness is therefore asserted from VersionList, which re-reads the DB:
	// exactly one row is current and it is the freshly-restored one.
	listBeforeCurrentCheck, err := svc.VersionList(ctx, path)
	if err != nil {
		t.Fatalf("VersionList (current check) failed: %v", err)
	}
	{
		var currents int
		for _, f := range listBeforeCurrentCheck {
			if f.IsCurrent {
				currents++
				if f.VersionID != restored.VersionID {
					t.Errorf("current row version_id %q, want restored %q", f.VersionID, restored.VersionID)
				}
			}
		}
		if currents != 1 {
			t.Fatalf("expected exactly 1 current row after restore, got %d", currents)
		}
	}

	// Restore actually RETRIEVED the old content and re-uploaded it: the last
	// Put the fake SDK saw must be exactly the v1 bytes.
	if got := fake.lastUpload(); !bytes.Equal(got, []byte("v1 content")) {
		t.Errorf("restore re-uploaded %q, want the old version content %q", got, "v1 content")
	}

	// All prior version rows are preserved (v1 and v2 still listed).
	list, err := svc.VersionList(ctx, path)
	if err != nil {
		t.Fatalf("VersionList after restore failed: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("after restore expected 3 version rows (v1, v2, restored), got %d", len(list))
	}

	// Exactly one current, and it is the freshly-restored (max-seq) row.
	var currents int
	for _, f := range list {
		if f.IsCurrent {
			currents++
			if f.VersionID != restored.VersionID {
				t.Errorf("current row version_id %q, want restored %q", f.VersionID, restored.VersionID)
			}
		}
	}
	if currents != 1 {
		t.Fatalf("expected exactly 1 current row after restore, got %d", currents)
	}

	// The old version is still retrievable by its original version_id.
	old, err := svc.VersionGet(ctx, path, "version-v1")
	if err != nil {
		t.Fatalf("VersionGet(old version) after restore failed: %v", err)
	}
	if old.VersionID != "version-v1" || old.IsCurrent {
		t.Errorf("old version should remain non-current and fetchable, got id=%q is_current=%v", old.VersionID, old.IsCurrent)
	}

	if fake.downloads == 0 {
		t.Error("VersionRestore never invoked SDK.Download — content was not retrieved")
	}
}

// TestVersion_RestorePropagatesDownloadError verifies that a failed/truncated
// historical download surfaces as a VersionRestore error and does NOT mint a
// partial/empty version as the new current winner. Regression for the
// CloseWithError propagation.
func TestVersion_RestorePropagatesDownloadError(t *testing.T) {
	ctx := context.Background()
	const path = "vault:/docs/doc.txt"

	fake := newVersionSDK(t)
	svc, db := openVaultTestService(t, fake)

	dirID, err := svc.getOrCreateDirectory("/docs")
	if err != nil {
		t.Fatalf("getOrCreateDirectory: %v", err)
	}
	keyV1 := fake.registerContent(mustHash(t, "00000000000000000000000000000000000000000000000000000000000000aa"), []byte("v1 content"))
	if err := db.Create(&File{UUID: "uuid-doc", Name: "doc.txt", DirectoryID: dirID, IsCurrent: true,
		VersionID: "version-v1", Seq: 1, ObjectKey: keyV1, Size: int64(len("v1 content"))}).Error; err != nil {
		t.Fatalf("seed version row: %v", err)
	}

	// Inject a mid-read failure into the historical download.
	fake.downloadErr = errors.New("simulated download failure after partial bytes")

	if _, err := svc.VersionRestore(ctx, path, "version-v1"); err == nil {
		t.Fatal("VersionRestore succeeded on a failed download; want an error")
	}

	// No new (current) version row may have been minted from the partial stream.
	rows, err := svc.VersionList(ctx, path)
	if err != nil {
		t.Fatalf("VersionList: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("failed restore minted %d version rows, want 1 (original only)", len(rows))
	}
	if !rows[0].IsCurrent || rows[0].VersionID != "version-v1" {
		t.Fatalf("original row altered by failed restore: current=%v version=%q", rows[0].IsCurrent, rows[0].VersionID)
	}
}

// TestVersion_RestorePreservesTags asserts that restoring an old version mints
// a new current winner that KEEPS the live file's tags, rather than coming up
// empty (which would silently drop the label set from the restored row).
func TestVersion_RestorePreservesTags(t *testing.T) {
	ctx := context.Background()
	const path = "vault:/docs/doc.txt"

	fake := newVersionSDK(t)
	svc, db := openVaultTestService(t, fake)

	dirID, err := svc.getOrCreateDirectory("/docs")
	if err != nil {
		t.Fatalf("getOrCreateDirectory: %v", err)
	}

	keyV1 := fake.registerContent(mustHash(t, "00000000000000000000000000000000000000000000000000000000000000aa"), []byte("v1 content"))
	keyV2 := fake.registerContent(mustHash(t, "00000000000000000000000000000000000000000000000000000000000000bb"), []byte("v2 content"))

	c1 := File{UUID: "uuid-doc", Name: "doc.txt", DirectoryID: dirID, IsCurrent: false,
		VersionID: "version-v1", Seq: 1, ObjectKey: keyV1, Size: int64(len("v1 content"))}
	c2 := File{UUID: "uuid-doc", Name: "doc.txt", DirectoryID: dirID, IsCurrent: true,
		VersionID: "version-v2", Seq: 2, ObjectKey: keyV2, Size: int64(len("v2 content"))}
	for _, f := range []File{c1, c2} {
		if err := db.Create(&f).Error; err != nil {
			t.Fatalf("seed version row: %v", err)
		}
	}
	// Attach a tag to the current winner's row. Re-read it by (name,dir) to get
	// the real auto-increment ID (the loop var `f` above is a copy).
	var c2row File
	if err := db.Where("name = ? AND is_current = 1", "doc.txt").First(&c2row).Error; err != nil {
		t.Fatalf("re-load current winner: %v", err)
	}
	if err := reconcileTagsTx(db, c2row.ID, []string{"finance", "draft"}); err != nil {
		t.Fatalf("reconcileTagsTx: %v", err)
	}

	restored, err := svc.VersionRestore(ctx, path, "version-v1")
	if err != nil {
		t.Fatalf("VersionRestore: %v", err)
	}

	// And the restored winner's tags must be persisted to the local join.
	gotTags, err := svc.currentTags(restored.ID)
	if err != nil {
		t.Fatalf("currentTags(restored): %v", err)
	}
	want := []string{"draft", "finance"} // normalizeTags + reconcileTagsTx order-sort
	if len(gotTags) != len(want) {
		t.Fatalf("restored tags = %v, want %v", gotTags, want)
	}
	for i := range want {
		if gotTags[i] != want[i] {
			t.Fatalf("restored tags = %v, want %v", gotTags, want)
		}
	}
}

// partialErrReader wraps an inner reader and injects err once the underlying
// content has been fully consumed, simulating a download that fails after
// emitting some bytes (a truncated/failed historical-object read).
type partialErrReader struct {
	io.Reader
	Err error
}

func (r *partialErrReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF && r.Err != nil {
		return n, r.Err
	}
	return n, err
}

// mustHash parses a 64-hex-char string into a types.Hash256, failing the test on
// error.
func mustHash(t *testing.T, hexStr string) types.Hash256 {
	t.Helper()
	b, err := hex.DecodeString(hexStr)
	if err != nil {
		t.Fatalf("bad test hash %q: %v", hexStr, err)
	}
	var h types.Hash256
	copy(h[:], b)
	return h
}
