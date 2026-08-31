//go:build sqlite_fts5

package vault

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"go.sia.tech/core/types"
)

// helper: build a disposable vaultService + fake SDK on a temp DB.
func newTagTestService(t *testing.T) (*vaultService, *fakeSDK) {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	fake := &fakeSDK{t: t}
	svc := &vaultService{db: db, sdk: fake, appKey: types.PrivateKey{}}
	t.Cleanup(func() { _ = svc.Close() })
	return svc, fake
}

// TestPutPromotesTagsFromMetadata verifies a metadata map carrying a 'tags' key
// seeds the file's durable tags at upload (via the reconcile path), readable via
// Stat.
func TestPutPromotesTagsFromMetadata(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTagTestService(t)

	tagsMeta := map[string]any{"tags": []any{"SessionX", "SessionX", "report"}}
	if _, err := svc.Put(ctx, bytes.NewReader([]byte("a")), 1, "vault:/docs/t.txt", tagsMeta); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	st, err := svc.Stat(ctx, "vault:/docs/t.txt")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	// normalizeTags lowercases + dedupes + sorts.
	want := []string{"report", "sessionx"}
	if !equalStrings(st.Tags, want) {
		t.Fatalf("Stat.Tags = %v, want %v", st.Tags, want)
	}

	// The tags must be persisted to the join (durable), not just the stat view.
	names, err := svc.TagList(ctx)
	if err != nil {
		t.Fatalf("TagList failed: %v", err)
	}
	if !equalStrings(names, want) {
		t.Fatalf("TagList = %v, want %v", names, want)
	}
}

// TestAddRemoveSetTagsRePinAndReconcile verifies the durable re-pin-and-write
// path: AddTags/RemoveTags/SetTags each re-pin the object and reconcile the
// local join so Stat reflects the result.
func TestAddRemoveSetTagsRePinAndReconcile(t *testing.T) {
	ctx := context.Background()
	svc, fake := newTagTestService(t)

	if _, err := svc.Put(ctx, bytes.NewReader([]byte("b")), 1, "vault:/docs/x.txt", nil); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// AddTags
	if _, err := svc.AddTags(ctx, "vault:/docs/x.txt", []string{"alpha", "Beta"}); err != nil {
		t.Fatalf("AddTags failed: %v", err)
	}
	if !fake.pinCalled {
		t.Fatalf("AddTags did not re-pin the object")
	}
	st, err := svc.Stat(ctx, "vault:/docs/x.txt")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !equalStrings(st.Tags, []string{"alpha", "beta"}) {
		t.Fatalf("after AddTags Stat.Tags = %v, want [alpha beta]", st.Tags)
	}

	// AddTags idempotent + additive
	if _, err := svc.AddTags(ctx, "vault:/docs/x.txt", []string{"beta", "gamma"}); err != nil {
		t.Fatalf("AddTags 2 failed: %v", err)
	}
	st, _ = svc.Stat(ctx, "vault:/docs/x.txt")
	if !equalStrings(st.Tags, []string{"alpha", "beta", "gamma"}) {
		t.Fatalf("after AddTags 2 Stat.Tags = %v, want [alpha beta gamma]", st.Tags)
	}

	// RemoveTags
	if _, err := svc.RemoveTags(ctx, "vault:/docs/x.txt", []string{"alpha"}); err != nil {
		t.Fatalf("RemoveTags failed: %v", err)
	}
	st, _ = svc.Stat(ctx, "vault:/docs/x.txt")
	if !equalStrings(st.Tags, []string{"beta", "gamma"}) {
		t.Fatalf("after RemoveTags Stat.Tags = %v, want [beta gamma]", st.Tags)
	}

	// SetTags (replace-all)
	if _, err := svc.SetTags(ctx, "vault:/docs/x.txt", []string{"zeta"}); err != nil {
		t.Fatalf("SetTags failed: %v", err)
	}
	st, _ = svc.Stat(ctx, "vault:/docs/x.txt")
	if !equalStrings(st.Tags, []string{"zeta"}) {
		t.Fatalf("after SetTags Stat.Tags = %v, want [zeta]", st.Tags)
	}

	// The removed tags (beta/gamma/alpha) must not linger in the global tag list.
	names, err := svc.TagList(ctx)
	if err != nil {
		t.Fatalf("TagList failed: %v", err)
	}
	if !equalStrings(names, []string{"zeta"}) {
		t.Fatalf("TagList = %v, want [zeta] (dead tags pruned)", names)
	}
}

// TestTagsSyncFromPutMetadataToObject verifies tags set at Put-time are written
// into the sealed object metadata (durable cross-device), i.e. the sidecar
// carries Metadata['tags'].
func TestTagsWrittenToObjectMetadata(t *testing.T) {
	ctx := context.Background()
	svc, fake := newTagTestService(t)

	tagsMeta := map[string]any{"tags": []any{"SessionX"}}
	if _, err := svc.Put(ctx, bytes.NewReader([]byte("c")), 1, "vault:/docs/m.txt", tagsMeta); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// The object that was pinned must carry the tags in its sealed metadata.
	pm, err := ParseFileMetadata(fake.pinnedMeta)
	if err != nil {
		t.Fatalf("parse pinned metadata: %v", err)
	}
	tags, ok := pm.Metadata["tags"]
	if !ok {
		t.Fatalf("pinned metadata has no 'tags' key: %v", pm.Metadata)
	}
	asSlice := toStringSlice(tags)
	if !equalStrings(asSlice, []string{"sessionx"}) {
		t.Fatalf("pinned Metadata['tags'] = %v, want [sessionx]", asSlice)
	}
}

func toStringSlice(v any) []string {
	if s, ok := v.([]string); ok {
		return s
	}
	if arr, ok := v.([]any); ok {
		out := make([]string, 0, len(arr))
		for _, e := range arr {
			out = append(out, toString(e))
		}
		return out
	}
	return nil
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
