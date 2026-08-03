package vault

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"go.sia.tech/core/types"
	"go.sia.tech/indexd/api/app"
	"go.sia.tech/indexd/slabs"
	"go.sia.tech/siastorage"
	"gorm.io/gorm"
)

// sdkClient is the subset of siastorage.SDK methods used by vaultService.
// Defined as an interface so tests can use a fake.
type sdkClient interface {
	Account(ctx context.Context) (app.AccountResponse, error)
	Upload(ctx context.Context, obj *siastorage.Object, r io.Reader, opts ...siastorage.UploadOption) error
	PinObject(ctx context.Context, obj siastorage.Object) error
	Object(ctx context.Context, objectKey types.Hash256) (siastorage.Object, error)
	ObjectEvents(ctx context.Context, cursor slabs.Cursor, limit int) ([]siastorage.ObjectEvent, error)
	Download(obj siastorage.Object, opts ...siastorage.DownloadOption) (io.ReadCloser, error)
	DeleteObject(ctx context.Context, key types.Hash256) error
	CreateSharedObjectURL(ctx context.Context, objectKey types.Hash256, validUntil time.Time) (string, error)
	Close() error
	AppKey() types.PrivateKey
}

// vaultService implements VaultService.
type vaultService struct {
	sdk    sdkClient
	db     *gorm.DB
	appKey types.PrivateKey
}

// NewVaultService creates a vault service from an SDK and an open database.
func NewVaultService(sdk *siastorage.SDK, db *gorm.DB) VaultService {
	return &vaultService{
		sdk:    sdk,
		db:     db,
		appKey: sdk.AppKey(),
	}
}

// NewVaultServiceForProfile creates a vault service for a specific profile.
// It loads the app key from the profile's state.json and opens the profile's SQLite cache.
func NewVaultServiceForProfile(profileName string, indexerURL string) (VaultService, error) {
	state, err := LoadProfileState(profileName)
	if err != nil {
		return nil, fmt.Errorf("failed to load profile state: %w", err)
	}
	if state.AppKey == "" {
		return nil, fmt.Errorf("profile %q has no app key. Run 'pinner vault login --profile %s' first", profileName, profileName)
	}

	appKey, err := DecodeAppKey(state.AppKey)
	if err != nil {
		return nil, err
	}

	appID := AppID()
	metadata := siastorage.AppMetadata{
		ID:          appID,
		Name:        "Pinner CLI Vault",
		Description: "Private encrypted file storage via Sia",
		ServiceURL:  indexerURL,
	}

	builder := siastorage.NewBuilder(indexerURL, metadata)
	sdk, err := builder.SDK(appKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create Sia SDK: %w", err)
	}

	db, err := OpenDB(ProfileDBPath(profileName))
	if err != nil {
		sdk.Close()
		return nil, err
	}

	return &vaultService{
		sdk:    sdk,
		db:     db,
		appKey: appKey,
	}, nil
}

// Init initializes the local vault database.
func (s *vaultService) Init(ctx context.Context) error {
	var count int64
	if err := s.db.Model(&File{}).Count(&count).Error; err != nil {
		return fmt.Errorf("vault db check failed: %w", err)
	}
	return nil
}

// CheckReady verifies the indexer has propagated the account registration.
// Right after login, the indexer needs a moment to propagate on the network.
// This returns a clear error so the user knows to wait and retry.
func (s *vaultService) CheckReady(ctx context.Context) error {
	account, err := s.sdk.Account(ctx)
	if err != nil {
		return fmt.Errorf("failed to check account status: %w", err)
	}
	if !account.Ready {
		return fmt.Errorf("account is not ready yet — the indexer is still propagating registration on the network; try again in a few seconds")
	}
	return nil
}

// Put uploads a file to the vault at the given vault path.
func (s *vaultService) Put(ctx context.Context, r io.Reader, size int64, vaultPath string, metadata map[string]any) (*File, error) {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return nil, err
	}
	if vp.IsDir {
		return nil, fmt.Errorf("destination must be a file path, not a directory")
	}

	// Get or create directory
	dirID, err := s.getOrCreateDirectory(vp.Directory)
	if err != nil {
		return nil, err
	}

	// Detect media type
	mediaType := mime.TypeByExtension(filepath.Ext(vp.Name))

	// Build file metadata
	now := time.Now().UTC().Format(time.RFC3339)
	fileMeta := FileMetadata{
		Name:      vp.Name,
		MediaType: mediaType,
		Size:      size,
		CreatedAt: now,
		Metadata:  metadata,
	}

	// Create Sia object and attach metadata
	obj := siastorage.NewEmptyObject()

	// Tee reader through SHA-256 hasher while uploading
	hasher := sha256.New()
	teeReader := io.TeeReader(r, hasher)

	if err := s.sdk.Upload(ctx, &obj, teeReader); err != nil {
		return nil, fmt.Errorf("upload failed: %w", err)
	}

	// Digest is known only after upload completes. Attach it to the object
	// metadata so remote sync reconstructs ContentDigest; otherwise Verify
	// and Stat report an empty digest on secondary devices.
	contentDigest := hex.EncodeToString(hasher.Sum(nil))
	fileMeta.ContentDigest = contentDigest
	metaJSON, err := fileMeta.JSON()
	if err != nil {
		return nil, err
	}
	obj.UpdateMetadata(metaJSON)

	if err := s.sdk.PinObject(ctx, obj); err != nil {
		return nil, fmt.Errorf("pin failed: %w", err)
	}

	objectKey := obj.ID()

	// Store in local DB
	// Load the prior record (same name+dirID) before we overwrite
	var prior File
	q := s.db.Where("name = ?", vp.Name)
	if dirID == nil {
		q = q.Where("directory_id IS NULL")
	} else {
		q = q.Where("directory_id = ?", dirID)
	}
	hasPrior := q.First(&prior).Error == nil

	// Within a transaction, delete the prior DB row (to free the unique
	// constraint) and insert the new record. If the insert fails, the
	// transaction rolls back, preserving the prior row so the path stays
	// tracked locally instead of vanishing (a vanished record would only be
	// recoverable via sync, which loses the directory placement).
	record := &File{
		Name:          vp.Name,
		DirectoryID:   dirID,
		ObjectKey:     objectKey.String(),
		Size:          size,
		MediaType:     mediaType,
		ContentDigest: contentDigest,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}

	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		if dirID == nil {
			if err := tx.Where("name = ? AND directory_id IS NULL", vp.Name).Delete(&File{}).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Where("name = ? AND directory_id = ?", vp.Name, dirID).Delete(&File{}).Error; err != nil {
				return err
			}
		}
		return tx.Create(record).Error
	})
	if txErr != nil {
		// Upload succeeded but DB write failed — object is on indexer but not
		// tracked locally. The transaction rolled back, so any prior local
		// record is preserved.
		return nil, fmt.Errorf("failed to store file record: %w (run 'pinner vault sync' to recover)", txErr)
	}

	// New record committed — now safe to delete the prior object from the indexer.
	// Guard against deleting the just-pinned object when identical content yields the same ID.
	if hasPrior && prior.ObjectKey != objectKey.String() {
		priorHash, err := parseHash256(prior.ObjectKey)
		if err != nil {
			// Record already committed; the orphaned prior object is harmless
			// and can be reclaimed later. Don't fail the Put — the new file
			// is already saved and pinned.
			_ = err
		} else {
			// Skip the indexer delete when another path still references this
			// object. Sia object IDs are content-addressed, so identical
			// content at different paths shares one object; deleting it would
			// orphan every other local record pointing at it.
			var refs int64
			s.db.Model(&File{}).Where("object_key = ?", prior.ObjectKey).Count(&refs)
			if refs == 0 {
				if delErr := s.sdk.DeleteObject(ctx, priorHash); delErr != nil {
					// Record already committed; the orphaned prior object is harmless
					// and can be reclaimed later. Non-fatal — the put succeeded.
					_ = delErr
				}
			}
		}
	}

	return record, nil
}

// Get downloads a file from the vault to the given writer.
func (s *vaultService) Get(ctx context.Context, vaultPath string, w io.Writer) error {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return err
	}

	dirID, err := s.getDirectoryID(vp.Directory)
	if err != nil {
		return fmt.Errorf("directory not found: %w", err)
	}

	var record File
	q := s.db.Where("name = ?", vp.Name)
	if dirID == nil {
		q = q.Where("directory_id IS NULL")
	} else {
		q = q.Where("directory_id = ?", dirID)
	}
	if err := q.First(&record).Error; err != nil {
		return fmt.Errorf("file not found: %s", vaultPath)
	}

	objHash, err := parseHash256(record.ObjectKey)
	if err != nil {
		return fmt.Errorf("failed to parse object key: %w", err)
	}
	obj, err := s.sdk.Object(ctx, objHash)
	if err != nil {
		return fmt.Errorf("failed to get object from indexer: %w", err)
	}

	reader, err := s.sdk.Download(obj)
	if err != nil {
		return fmt.Errorf("failed to start download: %w", err)
	}
	defer reader.Close()

	_, err = io.Copy(w, reader)
	return err
}

// List lists files and directories at the given vault path.
func (s *vaultService) List(ctx context.Context, vaultPath string) ([]ListItem, error) {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return nil, err
	}

	dirPath := vp.Directory
	// If listing a file path (no trailing /), list its parent. A bare dir
	// like "vault:/docs" parses to Directory="/", Name="docs"; resolve that
	// to /docs so we list the directory, not the root.
	if !vp.IsDir && vp.Name != "" {
		if vp.Directory == "/" {
			dirPath = "/" + vp.Name
		} else {
			dirPath = vp.Directory + "/" + vp.Name
		}
	}

	dirID, err := s.getDirectoryID(dirPath)
	if err != nil {
		// Directory doesn't exist yet — return empty list
		return []ListItem{}, nil
	}

	var items []ListItem

	// List subdirectories (direct children only)
	prefix := dirPath
	if prefix != "/" {
		prefix = prefix + "/"
	}
	var dirs []Directory
	likePattern := escapeLike(prefix) + "%"
	s.db.Where("path LIKE ? ESCAPE '\\' AND path != ?", likePattern, dirPath).Find(&dirs)
	for _, d := range dirs {
		rest := strings.TrimPrefix(d.Path, prefix)
		if !strings.Contains(rest, "/") {
			items = append(items, ListItem{
				Name:      rest,
				Type:      "dir",
				CreatedAt: d.CreatedAt.Format(time.RFC3339),
			})
		}
	}

	// List files in this directory
	var files []File
	if dirID == nil {
		s.db.Where("directory_id IS NULL").Find(&files)
	} else {
		s.db.Where("directory_id = ?", dirID).Find(&files)
	}
	for _, f := range files {
		items = append(items, ListItem{
			Name:      f.Name,
			Type:      "file",
			Size:      f.Size,
			MediaType: f.MediaType,
			CreatedAt: f.CreatedAt.Format(time.RFC3339),
			UpdatedAt: f.UpdatedAt.Format(time.RFC3339),
		})
	}

	return items, nil
}

// Stat returns metadata about a file or directory.
func (s *vaultService) Stat(ctx context.Context, vaultPath string) (*StatResult, error) {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return nil, err
	}

	// If it's a directory or root
	if vp.IsDir || vp.Name == "" {
		// Verify the directory exists (getDirectoryID errors if missing); the
		// resulting dirID only exists to confirm that, so discard it.
		if _, err := s.getDirectoryID(vp.Directory); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, vaultPath)
		}
		return &StatResult{
			Type:  "dir",
			Name:  filepath.Base(vp.Directory),
			Path:  vaultPath,
			Size:  0,
		}, nil
	}

	// It's a file
	dirID, err := s.getDirectoryID(vp.Directory)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, vaultPath)
	}

	var record File
	q := s.db.Where("name = ?", vp.Name)
	if dirID == nil {
		q = q.Where("directory_id IS NULL")
	} else {
		q = q.Where("directory_id = ?", dirID)
	}
	if err := q.First(&record).Error; err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, vaultPath)
	}

	return &StatResult{
		Type:          "file",
		Name:          record.Name,
		Path:          vaultPath,
		Size:          record.Size,
		MediaType:     record.MediaType,
		ContentDigest: record.ContentDigest,
		ObjectID:      record.ObjectKey,
		CreatedAt:     record.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     record.UpdatedAt.Format(time.RFC3339),
	}, nil
}

// Cat streams file content to the writer.
func (s *vaultService) Cat(ctx context.Context, vaultPath string, w io.Writer) error {
	return s.Get(ctx, vaultPath, w)
}

// Verify checks content integrity: SHA-256 digest + object existence on indexer.
func (s *vaultService) Verify(ctx context.Context, vaultPath string) (*VerifyResult, error) {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return nil, err
	}

	dirID, err := s.getDirectoryID(vp.Directory)
	if err != nil {
		return nil, fmt.Errorf("file not found: %s", vaultPath)
	}

	var record File
	q := s.db.Where("name = ?", vp.Name)
	if dirID == nil {
		q = q.Where("directory_id IS NULL")
	} else {
		q = q.Where("directory_id = ?", dirID)
	}
	if err := q.First(&record).Error; err != nil {
		return nil, fmt.Errorf("file not found: %s", vaultPath)
	}

	result := &VerifyResult{
		Path:          vaultPath,
		ContentDigest: record.ContentDigest,
		ObjectID:      record.ObjectKey,
	}

	// Check object exists on indexer
	objHash, err := parseHash256(record.ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse object key: %w", err)
	}
	// Check object exists on indexer. Only a genuine NotFound should report
	// ObjectExists=false; any other (transient indexer/network) error must
	// surface as an error rather than misleadingly reporting the object as
	// missing/corrupted.
	obj, err := s.sdk.Object(ctx, objHash)
	if err != nil {
		if errors.Is(err, slabs.ErrObjectNotFound) {
			result.ObjectExists = false
			result.DigestMatch = false
			return result, nil
		}
		return nil, fmt.Errorf("failed to fetch object from indexer: %w", err)
	}
	result.ObjectExists = true

	// Download object and recompute SHA-256 to verify content integrity
	if result.ObjectExists {
		reader, err := s.sdk.Download(obj)
		if err != nil {
			return nil, fmt.Errorf("failed to download for verification: %w", err)
		}
		defer reader.Close()

		hasher := sha256.New()
		if _, err := io.Copy(hasher, reader); err != nil {
			return nil, fmt.Errorf("failed to read content for verification: %w", err)
		}
		computedDigest := hex.EncodeToString(hasher.Sum(nil))
		result.DigestMatch = computedDigest == record.ContentDigest
	} else {
		result.DigestMatch = false
	}

	return result, nil
}

// Remove deletes a file from the vault (local DB + indexer).
func (s *vaultService) Remove(ctx context.Context, vaultPath string) error {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return err
	}

	dirID, err := s.getDirectoryID(vp.Directory)
	if err != nil {
		return fmt.Errorf("file not found: %s", vaultPath)
	}

	var record File
	q := s.db.Where("name = ?", vp.Name)
	if dirID == nil {
		q = q.Where("directory_id IS NULL")
	} else {
		q = q.Where("directory_id = ?", dirID)
	}
	if err := q.First(&record).Error; err != nil {
		return fmt.Errorf("file not found: %s", vaultPath)
	}

	// Determine whether another path shares this content-addressed object.
	// Sia object IDs are content-addressed, so identical content at different
	// paths shares one object; if another local record still references this
	// object key, deleting it from the indexer would orphan that other path.
	// Only delete the indexer object when this is the last local reference.
	var shared int64
	s.db.Model(&File{}).
		Where("object_key = ? AND id <> ?", record.ObjectKey, record.ID).
		Count(&shared)

	objHash, err := parseHash256(record.ObjectKey)
	if err != nil {
		return fmt.Errorf("failed to parse object key: %w", err)
	}

	// Delete the local DB row FIRST so the path disappears atomically and the
	// create-before-destroy invariant (mirroring Put) holds: if this fails, we
	// return before touching the indexer, so the local record never points at a
	// deleted remote object.
	if err := s.db.Delete(&record).Error; err != nil {
		return fmt.Errorf("failed to delete file record: %w", err)
	}

	// Best-effort indexer cleanup of the now-orphaned content-addressed object
	// (only when no other path still references it).
	//
	// This is deliberately NON-FATAL: the local path is already removed and the
	// create-before-destroy invariant is preserved, so a cleanup failure only
	// leaks an orphaned content-addressed object (reclaimable later). Returning
	// an error here would make `vault rm` report total failure after partial
	// success, and a retry would then hit "file not found" because the record
	// is already gone — misleading the caller. Treat it as best-effort.
	if shared == 0 {
		// Cleanup failure is intentionally ignored: the object is orphaned on
		// the indexer but the remove itself already succeeded.
		if err := s.sdk.DeleteObject(ctx, objHash); err != nil {
			_ = err // best-effort; see comment above
		}
	}

	return nil
}

// Share generates a time-limited sia:// share URL for a file.
func (s *vaultService) Share(ctx context.Context, vaultPath string, validUntil time.Time) (string, error) {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return "", err
	}

	dirID, err := s.getDirectoryID(vp.Directory)
	if err != nil {
		return "", fmt.Errorf("file not found: %s", vaultPath)
	}

	var record File
	q := s.db.Where("name = ?", vp.Name)
	if dirID == nil {
		q = q.Where("directory_id IS NULL")
	} else {
		q = q.Where("directory_id = ?", dirID)
	}
	if err := q.First(&record).Error; err != nil {
		return "", fmt.Errorf("file not found: %s", vaultPath)
	}

	objHash, err := parseHash256(record.ObjectKey)
	if err != nil {
		return "", fmt.Errorf("failed to parse object key: %w", err)
	}
	shareURL, err := s.sdk.CreateSharedObjectURL(ctx, objHash, validUntil)
	if err != nil {
		return "", fmt.Errorf("failed to create share URL: %w", err)
	}

	return NormalizeShareURL(shareURL), nil
}

// Close releases resources.
func (s *vaultService) Close() error {
	var dbErr error
	if s.db != nil {
		if sqlDB, err := s.db.DB(); err == nil {
			dbErr = sqlDB.Close()
		}
	}
	if s.sdk != nil {
		if err := s.sdk.Close(); err != nil {
			return errors.Join(err, dbErr)
		}
	}
	return dbErr
}

// getOrCreateDirectory creates all intermediate directories for a path.
func (s *vaultService) getOrCreateDirectory(path string) (*uint, error) {
	if path == "/" || path == "" {
		return nil, nil // root directory, NULL FK
	}

	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	currentPath := ""
	var parentID *uint

	for _, part := range parts {
		if part == "" {
			continue
		}
		currentPath = currentPath + "/" + part

		var dir Directory
		result := s.db.Where("path = ?", currentPath).First(&dir)
		if result.Error == gorm.ErrRecordNotFound {
			dir = Directory{
				Path:    currentPath,
				SortKey: part,
			}
			if err := s.db.Create(&dir).Error; err != nil {
				return nil, fmt.Errorf("failed to create directory %s: %w", currentPath, err)
			}
		} else if result.Error != nil {
			return nil, result.Error
		}
		id := dir.ID
		parentID = &id
	}

	return parentID, nil
}

// getDirectoryID returns the directory ID for a path, or error if not found.
func (s *vaultService) getDirectoryID(path string) (*uint, error) {
	if path == "/" || path == "" {
		return nil, nil
	}
	var dir Directory
	if err := s.db.Where("path = ?", path).First(&dir).Error; err != nil {
		return nil, err
	}
	id := dir.ID
	return &id, nil
}

// parseHash256 parses a hex-encoded Hash256 string.
func parseHash256(hexStr string) (types.Hash256, error) {
	var h types.Hash256
	if err := h.UnmarshalText([]byte(hexStr)); err != nil {
		return types.Hash256{}, fmt.Errorf("invalid object key %q: %w", hexStr, err)
	}
	return h, nil
}

// marshalCursor serializes a slabs.Cursor to JSON string.
func marshalCursor(c slabs.Cursor) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// unmarshalCursor deserializes a slabs.Cursor from JSON string.
func unmarshalCursor(s string) (slabs.Cursor, error) {
	var c slabs.Cursor
	err := json.Unmarshal([]byte(s), &c)
	return c, err
}

// escapeLike escapes SQL LIKE metacharacters in s using backslash as the escape character.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}
