package gitarchive_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"go.sia.tech/core/types"
	"go.sia.tech/siastorage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"go.lumeweb.com/pinner/core/vault"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
	"go.lumeweb.com/pinner-cli/internal/core/objmeta"
)

// openDB opens an in-memory-ish SQLite test DB in a temp file so AutoMigrate
// (which writes DDL) behaves exactly as on the real cache.db.
func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	return db
}

func card(t *testing.T, kind objmeta.Kind, body string) []byte {
	t.Helper()
	raw, err := objmeta.Encode(objmeta.New(kind, 2048, "sha256digest", 1700000000, json.RawMessage(body)))
	require.NoError(t, err)
	return raw
}

func objectKey(t *testing.T) types.Hash256 {
	t.Helper()
	sum := sha256.Sum256([]byte("git-object-key"))
	return types.Hash256(sum)
}

func TestAutoMigrateCreatesTables(t *testing.T) {
	db := openDB(t)
	require.NoError(t, gitarchive.AutoMigrate(db))

	require.True(t, db.Migrator().HasTable("git_repos"))
	require.True(t, db.Migrator().HasTable("git_objects"))
	require.True(t, db.Migrator().HasTable("git_binds"))

	// AutoMigrate must run on the same DB as the vault schema without
	// clobbering it. Creating the vault File table then re-migrating git tables
	// keeps both intact.
	require.NoError(t, db.AutoMigrate(&vault.File{}))
	require.True(t, db.Migrator().HasTable("files"))
	require.NoError(t, gitarchive.AutoMigrate(db))
	require.True(t, db.Migrator().HasTable("git_objects"))
	require.True(t, db.Migrator().HasTable("files"))
}

func TestIngestCardWritesGitObjectNoFileRows(t *testing.T) {
	db := openDB(t)
	// Migrate BOTH the vault schema (so a File table exists) and gitarchive.
	require.NoError(t, db.AutoMigrate(&vault.File{}))
	require.NoError(t, gitarchive.AutoMigrate(db))

	raw := card(t, objmeta.KindGitPack, `{"locker":"widgets","pack":"k","full":true}`)
	key := objectKey(t)

	handled, err := gitarchive.IngestCard(db, key.String(), raw)
	require.NoError(t, err)
	require.True(t, handled)

	// Exactly one git_objects row.
	var gobjs []gitarchive.GitObject
	require.NoError(t, db.Find(&gobjs).Error)
	require.Len(t, gobjs, 1)
	require.Equal(t, key.String(), gobjs[0].ObjectKey)
	require.Equal(t, "git.pack", gobjs[0].Kind)
	require.Equal(t, "widgets", gobjs[0].Locker)
	require.Equal(t, int64(2048), gobjs[0].Size)
	require.Equal(t, "sha256digest", gobjs[0].Digest)
	require.Equal(t, int64(1700000000), gobjs[0].Created)

	// And ZERO vault File rows: ingesting a git card must never create one.
	var files int64
	require.NoError(t, db.Model(&vault.File{}).Count(&files).Error)
	require.Zero(t, files)

	// Ingesting the same object key again is idempotent.
	handled, err = gitarchive.IngestCard(db, key.String(), raw)
	require.NoError(t, err)
	require.True(t, handled)
	require.NoError(t, db.Find(&gobjs).Error)
	require.Len(t, gobjs, 1)
}

func TestIngestCardIgnoresLegacyVaultFile(t *testing.T) {
	db := openDB(t)
	require.NoError(t, db.AutoMigrate(&vault.File{}))
	require.NoError(t, gitarchive.AutoMigrate(db))

	// A legacy vault FileMetadata must NOT be ingested as a git object.
	legacy := `{"id":"11111111-2222-3333-4444-555555555555","name":"report.pdf","media_type":"application/pdf","size":2048,"created_at":"2024-01-01T00:00:00Z"}`
	handled, err := gitarchive.IngestCard(db, "legacykey", []byte(legacy))
	require.NoError(t, err)
	require.False(t, handled)

	var gobjs []gitarchive.GitObject
	require.NoError(t, db.Find(&gobjs).Error)
	require.Len(t, gobjs, 0)
}

func TestScanIngestsGitCardsAndSkipsVaultFiles(t *testing.T) {
	db := openDB(t)
	require.NoError(t, db.AutoMigrate(&vault.File{}))
	require.NoError(t, gitarchive.AutoMigrate(db))

	// Fixture events: one git card, one vault file, one deleted event.
	gitObj := siastorage.NewEmptyObject()
	gitObj.UpdateMetadata(card(t, objmeta.KindGitTip, `{"locker":"widgets","gen":2,"prev":"p"}`))

	vaultObj := siastorage.NewEmptyObject()
	vaultObj.UpdateMetadata([]byte(`{"id":"22222222-2222-3333-4444-555555555555","name":"notes.txt","created_at":"2024-01-01T00:00:00Z"}`))

	events := []siastorage.ObjectEvent{
		{Key: objectKey(t), Object: &gitObj},
		{Key: types.Hash256(sha256.Sum256([]byte("vault"))), Object: &vaultObj},
		{Key: types.Hash256(sha256.Sum256([]byte("del"))), Deleted: true},
	}

	ingested, err := gitarchive.Scan(context.Background(), db, events)
	require.NoError(t, err)
	require.Equal(t, 1, ingested)

	var gitObjs []gitarchive.GitObject
	require.NoError(t, db.Find(&gitObjs).Error)
	require.Len(t, gitObjs, 1)
	require.Equal(t, "git.tip", gitObjs[0].Kind)

	var files int64
	require.NoError(t, db.Model(&vault.File{}).Count(&files).Error)
	require.Zero(t, files)
}
