package objmeta_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner/core/vault"

	"go.lumeweb.com/pinner-cli/internal/core/objmeta"
)

func TestCardRoundtrip(t *testing.T) {
	c := objmeta.New(objmeta.KindGitPack, 12345, "abc123", 1700000000, json.RawMessage(`{"locker":"widgets","pack":"k","full":true}`))
	raw, err := objmeta.Encode(c)
	require.NoError(t, err)

	got, err := objmeta.Decode(raw)
	require.NoError(t, err)
	require.Equal(t, objmeta.Schema, got.Schema)
	require.Equal(t, objmeta.KindGitPack, got.Kind)
	require.Equal(t, int64(12345), got.Size)
	require.Equal(t, "abc123", got.Digest)
	require.Equal(t, int64(1700000000), got.Created)
	require.JSONEq(t, `{"locker":"widgets","pack":"k","full":true}`, string(got.Body))
}

func TestCardCreatedIsIntegerUnixTimestamp(t *testing.T) {
	c := objmeta.New(objmeta.KindGitTip, 10, "d", 1700000000, nil)
	raw, err := objmeta.Encode(c)
	require.NoError(t, err)

	// The wire format must carry the creation timestamp as an INTEGER under
	// `created_at` (the vault compatibility guard). Assert the JSON shape so a
	// future refactor cannot silently flip it back to a string.
	s := string(raw)
	require.Contains(t, s, `"created_at":1700000000`)
	require.False(t, strings.Contains(s, `"created_at":"`), "created_at must be an integer, not a string")
}

func TestCardCap(t *testing.T) {
	// A card within budget is accepted.
	small := objmeta.New(objmeta.KindGitShare, 5, "d", 1, json.RawMessage(`{"locker":"x","gen":0,"until":9}`))
	raw, err := objmeta.Encode(small)
	require.NoError(t, err)
	require.LessOrEqual(t, len(raw), objmeta.MaxCardSize)

	// Oversize body pushes the serialized card past the 1024-byte cap.
	big := objmeta.New(objmeta.KindGitPack, 5, "d", 1, json.RawMessage(fmt.Sprintf(`{"locker":"%s"}`, strings.Repeat("x", objmeta.MaxCardSize))))
	_, err = objmeta.Encode(big)
	require.ErrorIs(t, err, objmeta.ErrOversize)

	// Decode also rejects oversize payloads.
	_, err = objmeta.Decode([]byte(strings.Repeat(" ", objmeta.MaxCardSize+1)))
	require.ErrorIs(t, err, objmeta.ErrOversize)
}

func TestRouteLegacyVaultFile(t *testing.T) {
	// A legacy vault FileMetadata (it has id + name and no schema/kind) routes
	// to KindVaultFile — the vault.file schema, not a git card.
	legacy := `{"id":"11111111-2222-3333-4444-555555555555","version_id":"v1","name":"report.pdf","directory":"/reports","media_type":"application/pdf","size":2048,"created_at":"2024-01-01T00:00:00Z","content_digest":"dd","status":"ok"}`
	k, err := objmeta.Route([]byte(legacy))
	require.NoError(t, err)
	require.Equal(t, objmeta.KindVaultFile, k)
	require.True(t, objmeta.IsVaultFile([]byte(legacy)))
	require.False(t, objmeta.IsCard([]byte(legacy)))

	// Sanity: the same legacy metadata must parse fine as a vault FileMetadata.
	_, err = vault.ParseFileMetadata(json.RawMessage(legacy))
	require.NoError(t, err)
}

func TestRouteCard(t *testing.T) {
	for _, tc := range []struct {
		kind objmeta.Kind
		want objmeta.Kind
	}{
		{objmeta.KindGitPack, objmeta.KindGitPack},
		{objmeta.KindGitTip, objmeta.KindGitTip},
		{objmeta.KindGitShare, objmeta.KindGitShare},
	} {
		raw, err := objmeta.Encode(objmeta.New(tc.kind, 1, "d", 1, nil))
		require.NoError(t, err)
		k, err := objmeta.Route(raw)
		require.NoError(t, err)
		require.Equal(t, tc.want, k)
		require.True(t, objmeta.IsCard(raw))
	}
}

func TestRouteUnknown(t *testing.T) {
	_, err := objmeta.Route(nil)
	require.ErrorIs(t, err, objmeta.ErrNotCard)

	// JSON but not a pinner card and not a legacy vault file.
	_, err = objmeta.Route([]byte(`{"foo":1}`))
	require.ErrorIs(t, err, objmeta.ErrNotCard)

	// Wrong schema.
	_, err = objmeta.Route([]byte(`{"schema":"pinner.other/v1","kind":"git.pack"}`))
	require.ErrorIs(t, err, objmeta.ErrNotCard)

	// Unknown kind on a valid schema.
	_, err = objmeta.Route([]byte(`{"schema":"pinner.obj/v1","kind":"git.bogus"}`))
	require.ErrorIs(t, err, objmeta.ErrUnknownKind)
}

// TestGitCardDoesNotParseAsVaultFileMetadata is the compatibility guard that
// makes kind routing work: a git card, because its created_at is an INTEGER
// (unix seconds), must FAIL vault.ParseFileMetadata, so vault.Sync skips it and
// never creates an empty-named vault File row.
func TestGitCardDoesNotParseAsVaultFileMetadata(t *testing.T) {
	raw, err := objmeta.Encode(objmeta.New(objmeta.KindGitPack, 1024, "sha256abcd", 1700000000,
		json.RawMessage(`{"locker":"widgets","pack":"k","full":true}`)))
	require.NoError(t, err)

	// Route identifies it as a git card...
	k, err := objmeta.Route(raw)
	require.NoError(t, err)
	require.Equal(t, objmeta.KindGitPack, k)

	// ...but the vault parser must REJECT it (created_at is an int, not the
	// RFC3339 string FileMetadata expects). Without this guard, vault.Sync
	// would upsert an empty-named File row for every git object.
	_, err = vault.ParseFileMetadata(raw)
	require.Error(t, err, "git card must not parse as vault FileMetadata")
	require.Contains(t, err.Error(), "created_at")
}
