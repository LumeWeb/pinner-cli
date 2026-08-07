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
	"sync"
	"time"

	"github.com/google/uuid"
	"go.sia.tech/core/types"
	"go.sia.tech/indexd/api/app"
	"go.sia.tech/indexd/slabs"
	"go.sia.tech/siastorage"
	"gorm.io/datatypes"
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
	db     *gorm.DB
	appKey types.PrivateKey

	// indexerURL and metadata are retained so the Sia SDK can be built lazily
	// on first use. Constructing the SDK hits the network (CheckAppAuth +
	// refreshHosts against the indexer), so building it eagerly for every
	// command makes even a local-cache-only `ls` take seconds. Read-only
	// commands (List/Stat/Cat) never touch the SDK.
	indexerURL string
	metadata   siastorage.AppMetadata

	sdkMu  sync.Mutex // guards sdk, sdkErr, closed (fast-path read, lazy build, Close)
	sdk    sdkClient
	sdkErr error
	closed bool
}

// ensureSDK returns the Sia SDK, building it (and hitting the network) only on
// first use. Tests that inject a fake SDK directly (s.sdk != nil) get it back
// unchanged; production services build the real SDK lazily. All access to
// sdk/sdkErr/closed is serialized through sdkMu so a concurrent Close() can
// never race an in-flight lazy build (or leak the SDK built after it). Once the
// service is Closed, ensureSDK returns ErrVaultClosed rather than lazily
// rebuilding a fresh SDK on a disposed service.
func (s *vaultService) ensureSDK() (sdkClient, error) {
	s.sdkMu.Lock()
	defer s.sdkMu.Unlock()
	if s.closed {
		return nil, ErrVaultClosed
	}
	if s.sdk != nil {
		return s.sdk, nil
	}
	builder := siastorage.NewBuilder(s.indexerURL, s.metadata)
	s.sdk, s.sdkErr = builder.SDK(s.appKey)
	return s.sdk, s.sdkErr
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
	// Reject path-traversal profile names before they are used to construct
	// filesystem paths (state.json, SQLite cache). ResolveProfile already
	// guards user input at the CLI boundary, but this constructor is reached
	// directly by callers and must not embed ../ or absolute paths.
	if err := ValidateProfileName(profileName); err != nil {
		return nil, err
	}
	state, err := LoadProfileState(profileName)
	if err != nil {
		return nil, fmt.Errorf("failed to load profile state: %w", err)
	}
	if state.AppKey == "" {
		return nil, fmt.Errorf("profile %q has no app key. Provision it with 'pinner vault create --profile %s' or 'pinner vault restore --profile %s'", profileName, profileName, profileName)
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

	// Open the cache WITHOUT running goose migrations or constructing the Sia
	// SDK. Migrations are applied at schema-maintenance boundaries
	// (create/restore/cache rebuild). The SDK is built lazily on first use
	// because building it hits the network (CheckAppAuth + refreshHosts) — a
	// local-cache-only `ls`/`stat`/`cat` should not pay a multi-second network
	// round-trip.
	db, err := OpenDBNoMigrate(ProfileDBPath(profileName))
	if err != nil {
		return nil, err
	}

	return &vaultService{
		db:         db,
		appKey:     appKey,
		indexerURL: indexerURL,
		metadata:   metadata,
	}, nil
}

// CheckReady verifies the indexer has propagated the account registration.
// Right after login, the indexer needs a moment to propagate on the network.
// This returns a clear error so the user knows to wait and retry.
func (s *vaultService) CheckReady(ctx context.Context) error {
	sdk, err := s.ensureSDK()
	if err != nil {
		return fmt.Errorf("failed to create Sia SDK: %w", err)
	}
	account, err := sdk.Account(ctx)
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
	vp, err = RequireActiveProfile(vp)
	if err != nil {
		return nil, err
	}
	if vp.IsDir {
		return nil, fmt.Errorf("destination must be a file path, not a directory")
	}

	// Upload requires the (lazily-built, network-connected) SDK.
	sdk, err := s.ensureSDK()
	if err != nil {
		return nil, fmt.Errorf("failed to create Sia SDK: %w", err)
	}

	// Get or create directory
	dirID, err := s.getOrCreateDirectory(vp.Directory)
	if err != nil {
		return nil, err
	}

	// Detect media type
	mediaType := mime.TypeByExtension(filepath.Ext(vp.Name))

	// Resolve the file's stable identity. Overwriting an existing path keeps
	// the current file's UUID (it is the same logical file, new content); a
	// brand-new path gets a fresh UUID. Identity is never the name, so two
	// distinct objects sharing a name are still tracked separately.
	fileID := ""
	if current, err := s.findCurrentFile(vp.Name, dirID); err == nil {
		fileID = current.UUID
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		// A real failure (not a missing row) must not be masked as a brand-new
		// file — doing so would mint a fresh UUID and break the stable-identity
		// overwrite contract (and could duplicate the object on the next sync).
		return nil, fmt.Errorf("failed to resolve current file for %s: %w", vp.Name, err)
	}
	// mintedFresh records whether we created the UUID ourselves because no
	// current row existed yet. Only a freshly-minted identity is at risk of a
	// concurrent-create conflict (an existing path is an overwrite and can
	// never collide on insert), so the pre-transaction adopt pre-flight below
	// runs only in that case.
	mintedFresh := false
	if fileID == "" {
		fileID = uuid.NewString()
		mintedFresh = true
	}

	// Build file metadata
	now := time.Now().UTC().Format(time.RFC3339)
	fileMeta := FileMetadata{
		ID:        fileID,
		Name:      vp.Name,
		Directory: vp.Directory,
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

	if err := sdk.Upload(ctx, &obj, teeReader); err != nil {
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

	if err := sdk.PinObject(ctx, obj); err != nil {
		return nil, fmt.Errorf("pin failed: %w", err)
	}

	objectKey := obj.ID()

	// Store in local DB, keyed by UUID, and promote this file as the single
	// winner for its (name, dir) path — atomically. Overwriting an existing
	// path keeps its UUID (same logical file, new content); a brand-new path
	// gets a fresh UUID. The DB write and the is_current promotion happen in
	// one transaction so the partial unique index idx_files_live_name_dir
	// enforces at most one current live row per path.

	// Capture the prior object key (if this is an overwrite) BEFORE we write,
	// so we can best-effort clean up the orphaned prior content after.
	priorObjectKey := ""
	var record *File
	nowTs := time.Now().UTC()
	// Persist the user-supplied metadata map on the local File row so it
	// survives cache rebuilds and is returned by Stat. The Sia object already
	// carries it in its encrypted metadata; this is the local DB mirror.
	userMetaJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}
	rec := File{
		UUID:          fileID,
		Name:          vp.Name,
		DirectoryID:   dirID,
		IsCurrent:     true,
		ObjectKey:     objectKey.String(),
		Size:          size,
		MediaType:     mediaType,
		ContentDigest: contentDigest,
		Metadata:      datatypes.JSON(userMetaJSON),
		CreatedAt:     nowTs,
		UpdatedAt:     nowTs,
	}

	// The store below is wrapped in a bounded retry to resolve the concurrent-
	// create race WITHOUT ever running network I/O inside the single-connection
	// SQLite write transaction. OpenDB caps the pool at SetMaxOpenConns(1), so
	// holding the open transaction across an indexer round-trip would block every
	// other vault DB operation (Get/List/Stat/Sync) for the full network latency
	// and risk the 5000ms busy_timeout. Every network call in this block runs in
	// the adoptPreflight helper OUTSIDE the transaction.
	//
	// adoptPreflight re-resolves (name, dir) for a freshly-minted path. If a
	// concurrent writer claimed the path after we minted our UUID, we are the
	// loser: adopt the winner's UUID and re-stamp + re-pin the object with it
	// here, before any write transaction opens. Re-pinning first guarantees the
	// remote object and the committed row share the adopted identity — a re-pin
	// failure returns before anything is committed, so Sync can never mint a
	// duplicate from stale-metadata (this is the invariant the original
	// in-transaction re-pin enforced, now without holding the connection).
	//
	// If the transaction still hits a create-conflict (a writer committed
	// between adoptPreflight and tx.Create), we retry: the next preflight finds
	// that winner, adopts + re-pins it outside the transaction, and the retry's
	// transaction takes the overwrite path (no conflict). The object's content
	// ID is unaffected by metadata, so re-pinning only rewrites the UUID stamp.
	const maxAdoptRetries = 4
	for attempt := 0; attempt < maxAdoptRetries; attempt++ {
		if mintedFresh {
			if adopted, aerr := s.adoptPreflight(ctx, &obj, &fileMeta, vp.Name, dirID, &rec); aerr != nil {
				return nil, fmt.Errorf("failed to adopt concurrent winner: %w", aerr)
			} else if adopted {
				// The object now carries the adopted UUID; the transaction below
				// must target that identity (overwrite path), not our old UUID.
				fileID = rec.UUID
			}
		}

		err = s.db.Transaction(func(tx *gorm.DB) error {
			var prior File
			switch tx.Where("uuid = ?", rec.UUID).First(&prior).Error {
			case nil:
				priorObjectKey = prior.ObjectKey
				// Update the existing UUID row (overwrite). Mark it current; the
				// promotion below demotes any other live current row in the group.
				prior.ObjectKey = rec.ObjectKey
				prior.Size = rec.Size
				prior.MediaType = rec.MediaType
				prior.ContentDigest = rec.ContentDigest
				prior.Metadata = datatypes.JSON(userMetaJSON)
				prior.IsCurrent = true
				prior.UpdatedAt = rec.UpdatedAt
				prior.DeletedAt = nil // resurrect if it was tombstoned
				if err := tx.Save(&prior).Error; err != nil {
					return err
				}
				rec = prior
				return promoteCurrent(tx, vp.Name, dirID, rec.ID)
			case gorm.ErrRecordNotFound:
				// A concurrent writer can win the (name, directory) path between
				// adoptPreflight and this insert, tripping the partial unique
				// index. This is not fatal: return the conflict so the retry
				// loop above re-runs adoptPreflight, which now resolves the
				// winner and re-pins the object OUTSIDE the transaction, and
				// then takes the overwrite path here. No network I/O happens
				// under the single-connection write lock.
				if err := tx.Create(&rec).Error; err != nil {
					return err
				}
				return promoteCurrent(tx, vp.Name, dirID, rec.ID)
			default:
				return fmt.Errorf("failed to query existing file by uuid %q", rec.UUID)
			}
		})
		if err == nil {
			record = &rec
			break
		}
		if !isLiveNameConflict(err) {
			return nil, fmt.Errorf("failed to store file record: %w", err)
		}
		// A create-conflict: go around again. On retry adoptPreflight re-pins
		// the object with the winner's UUID before the write transaction, so a
		// re-pin failure still surfaces (a) after any commit and (b) before any
		// adoption is persisted.
	}
	if err != nil {
		return nil, fmt.Errorf("failed to store file record after %d attempts: %w", maxAdoptRetries, err)
	}

	// Best-effort cleanup of a prior object that is no longer referenced by any
	// LIVE local row (only when the overwritten UUID row had different content).
	if priorObjectKey != "" && priorObjectKey != objectKey.String() {
		priorHash, perr := parseHash256(priorObjectKey)
		if perr == nil {
			var refs int64
			s.db.Model(&File{}).Where("object_key = ? AND deleted_at IS NULL", priorObjectKey).Count(&refs)
			if refs == 0 {
				if delErr := sdk.DeleteObject(ctx, priorHash); delErr != nil {
					_ = delErr // non-fatal; orphaned content can be reclaimed later
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
	vp, err = RequireActiveProfile(vp)
	if err != nil {
		return err
	}

	record, err := s.resolveFile(vp)
	if err != nil {
		return err
	}

	objHash, err := parseHash256(record.ObjectKey)
	if err != nil {
		return fmt.Errorf("failed to parse object key: %w", err)
	}
	sdk, err := s.ensureSDK()
	if err != nil {
		return fmt.Errorf("failed to create Sia SDK: %w", err)
	}
	obj, err := sdk.Object(ctx, objHash)
	if err != nil {
		return fmt.Errorf("failed to get object from indexer: %w", err)
	}

	reader, err := sdk.Download(obj)
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
	vp, err = RequireActiveProfile(vp)
	if err != nil {
		return nil, err
	}

	dirPath := vp.Directory
	// A non-trailing-slash path with a leaf name is ambiguous: it could be a
	// bare directory (`vault:/docs` -> list /docs) or a file (`vault:/docs/report.pdf`
	// -> list its parent /docs). When the leaf lives under a NON-root parent,
	// it is a file path, so resolve the parent (vp.Directory already holds it)
	// rather than treating the whole path as a directory to look up. A leaf at
	// ROOT (`vault:/docs`, Directory == "/") is treated as a directory to list,
	// matching `vault:/docs` listing the /docs dir.
	if !vp.IsDir && vp.Name != "" {
		if vp.Directory == "/" {
			dirPath = JoinDirPath("/", vp.Name)
		} else {
			dirPath = vp.Directory // parent directory of the target file
		}
	}

	dirID, err := s.getDirectoryID(dirPath)
	if err != nil {
		// A genuinely missing directory is an empty listing, not an error; a
		// real DB failure (lock timeout, corruption) must NOT silently present
		// a populated vault as empty, so propagate it.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return []ListItem{}, nil
		}
		return nil, fmt.Errorf("failed to resolve directory %s: %w", dirPath, err)
	}

	var items []ListItem

	// List subdirectories (direct children only)
	prefix := dirPath
	if prefix != "/" {
		prefix = prefix + "/"
	}
	var dirs []Directory
	likePattern := escapeLike(prefix) + "%"
	// Direct children only, filtered in SQL: the path must start with the
	// prefix (LIKE) and, after the prefix, contain no further '/' — i.e. it is
	// a single-level child, not a deeper descendant. This keeps memory bounded
	// to the immediate children instead of loading the entire subtree on every
	// listing and pruning it in Go.
	if err := s.db.Where("path LIKE ? ESCAPE '\\' AND path != ? AND instr(substr(path, length(?) + 1), '/') = 0",
		likePattern, dirPath, prefix).Find(&dirs).Error; err != nil {
		return nil, fmt.Errorf("failed to list subdirectories of %s: %w", dirPath, err)
	}
	for _, d := range dirs {
		items = append(items, ListItem{
			Name:      strings.TrimPrefix(d.Path, prefix),
			Type:      "dir",
			CreatedAt: d.CreatedAt.Format(time.RFC3339),
		})
	}

	// List files in this directory. Names are non-unique, but exactly one row
	// per (name, dir) is the current winner (is_current=1, enforced by the
	// partial unique index), so listing current+live rows yields one entry per
	// file path. Historical versions (is_current=0) and tombstoned rows are not
	// surfaced as path entries.
	var files []File
	var fileErr error
	if dirID == nil {
		fileErr = s.db.Where("directory_id IS NULL AND is_current = 1 AND deleted_at IS NULL").
			Order("updated_at DESC, id DESC").Find(&files).Error
	} else {
		fileErr = s.db.Where("directory_id = ? AND is_current = 1 AND deleted_at IS NULL", dirID).
			Order("updated_at DESC, id DESC").Find(&files).Error
	}
	if fileErr != nil {
		return nil, fmt.Errorf("failed to list files of %s: %w", dirPath, fileErr)
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
	vp, err = RequireActiveProfile(vp)
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
			Type: "dir",
			Name: filepath.Base(vp.Directory),
			Path: vaultPath,
			Size: 0,
		}, nil
	}

	// It's a file
	dirID, err := s.getDirectoryID(vp.Directory)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, vaultPath)
	}

	var record File
	if f, err := s.findCurrentFile(vp.Name, dirID); err != nil {
		// Not a file. A bare root leaf (vault:/docs -> Directory "/", Name
		// "docs") is ambiguous: it can be a directory that `vault ls /docs`
		// lists. Mirror List's root-leaf handling: if the leaf names an
		// existing directory at root, report it as a directory instead of a
		// misleading not-found.
		if vp.Directory == "/" {
			if _, derr := s.getDirectoryID(JoinDirPath("/", vp.Name)); derr == nil {
				return &StatResult{Type: "dir", Name: vp.Name, Path: vaultPath}, nil
			}
		}
		return nil, fmt.Errorf("%w: %s", ErrNotFound, vaultPath)
	} else {
		record = f
	}

	var metaOut map[string]any
	if len(record.Metadata) > 0 {
		_ = json.Unmarshal(record.Metadata, &metaOut) // best-effort: malformed local metadata is surfaced empty, not an error
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
		Metadata:      metaOut,
	}, nil
}

// Cat streams file content to the writer.
func (s *vaultService) Cat(ctx context.Context, vaultPath string, w io.Writer) error {
	return s.Get(ctx, vaultPath, w)
}

// Verify checks content integrity: object existence on the indexer and a
// digest match. It is deliberately SHALLOW — it compares the stored digest in
// the object's metadata against the local row's ContentDigest WITHOUT
// downloading the full file content, so it is cheap even for large encrypted
// files. Use VerifyDeep for a true full-content re-hash.
func (s *vaultService) Verify(ctx context.Context, vaultPath string) (*VerifyResult, error) {
	res, obj, exists, err := s.resolveVerifyObject(ctx, vaultPath)
	if err != nil || !exists {
		return res, err
	}
	// Shallow integrity: trust the digest the object's metadata declares, and
	// compare it to the local row's ContentDigest. No content download.
	objDigest := ""
	if rawMeta := obj.Metadata(); len(rawMeta) > 0 {
		if m, merr := ParseFileMetadata(rawMeta); merr == nil {
			objDigest = m.ContentDigest
		}
	}
	res.DigestMatch = objDigest != "" && objDigest == res.ContentDigest
	return res, nil
}

// VerifyDeep is like Verify, but additionally downloads the full object content
// and recomputes SHA-256 so DigestMatch reflects actual bytes on the indexer
// rather than the metadata-declared digest. This transfers the entire file over
// the network; use it only when a true integrity check is required.
func (s *vaultService) VerifyDeep(ctx context.Context, vaultPath string) (*VerifyResult, error) {
	res, obj, exists, err := s.resolveVerifyObject(ctx, vaultPath)
	if err != nil || !exists {
		return res, err
	}

	sdk, err := s.ensureSDK()
	if err != nil {
		return nil, fmt.Errorf("failed to create Sia SDK: %w", err)
	}
	reader, err := sdk.Download(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to download for verification: %w", err)
	}
	defer reader.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, reader); err != nil {
		return nil, fmt.Errorf("failed to read content for verification: %w", err)
	}
	computedDigest := hex.EncodeToString(hasher.Sum(nil))
	res.DigestMatch = computedDigest == res.ContentDigest
	return res, nil
}

// resolveVerifyObject resolves the file record and its indexer object. It
// returns a populated *VerifyResult (with ObjectExists set), the raw
// siastorage.Object for callers that need to download content (VerifyDeep), and
// an exists flag. On a genuine NotFound it returns (result with
// ObjectExists=false, zero object, false, nil); any other error is returned as
// (nil, zero object, false, err).
func (s *vaultService) resolveVerifyObject(ctx context.Context, vaultPath string) (*VerifyResult, siastorage.Object, bool, error) {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return nil, siastorage.Object{}, false, err
	}
	vp, err = RequireActiveProfile(vp)
	if err != nil {
		return nil, siastorage.Object{}, false, err
	}

	record, err := s.resolveFile(vp)
	if err != nil {
		return nil, siastorage.Object{}, false, err
	}

	result := &VerifyResult{
		Path:          vaultPath,
		ContentDigest: record.ContentDigest,
		ObjectID:      record.ObjectKey,
	}

	objHash, err := parseHash256(record.ObjectKey)
	if err != nil {
		return nil, siastorage.Object{}, false, fmt.Errorf("failed to parse object key: %w", err)
	}
	// Check object exists on indexer. Only a genuine NotFound should report
	// ObjectExists=false; any other (transient indexer/network) error must
	// surface as an error rather than misleadingly reporting the object as
	// missing/corrupted.
	sdk, err := s.ensureSDK()
	if err != nil {
		return nil, siastorage.Object{}, false, fmt.Errorf("failed to create Sia SDK: %w", err)
	}
	obj, err := sdk.Object(ctx, objHash)
	if err != nil {
		if errors.Is(err, slabs.ErrObjectNotFound) {
			result.ObjectExists = false
			result.DigestMatch = false
			return result, siastorage.Object{}, false, nil
		}
		return nil, siastorage.Object{}, false, fmt.Errorf("failed to fetch object from indexer: %w", err)
	}
	result.ObjectExists = true
	return result, obj, true, nil
}

// Remove deletes a file from the vault (local DB + indexer).
func (s *vaultService) Remove(ctx context.Context, vaultPath string) error {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return err
	}
	vp, err = RequireActiveProfile(vp)
	if err != nil {
		return err
	}

	record, err := s.resolveFile(vp)
	if err != nil {
		return err
	}

	objHash, err := parseHash256(record.ObjectKey)
	if err != nil {
		return fmt.Errorf("failed to parse object key: %w", err)
	}

	// Tombstone, re-count live references, and decide the indexer delete all
	// inside ONE transaction. Sia object IDs are content-addressed, so identical
	// content at different paths shares a single object; when tombstoning the
	// last live reference the object must be deleted from the indexer exactly
	// once. Doing the COUNT after the tombstone within the same transaction
	// closes the check-then-act race: if two concurrent Removes of sibling paths
	// share one object, the transaction serializes them so the last remover sees
	// zero remaining live references and deletes — the object cannot be orphaned
	// by both remover reading shared>0 and skipping the delete.
	now := time.Now().UTC()
	deleteObject := false
	err = s.db.Transaction(func(tx *gorm.DB) error {
		// Soft-delete (deleted_at, not hard-delete) the row FIRST so the path
		// disappears atomically and the create-before-destroy invariant holds:
		// if this fails, return before touching the indexer. Soft-delete keeps
		// the record recoverable if it is re-uploaded.
		if err := tx.Model(&File{}).Where("id = ?", record.ID).
			Update("deleted_at", now).Error; err != nil {
			return fmt.Errorf("failed to delete file record: %w", err)
		}
		// Re-count LIVE references (tombstoned rows no longer reference the
		// object). Our own row is now tombstoned, so this reflects the true
		// post-remove state and includes any row another concurrent Remove is
		// about to tombstone — only the final remover sees zero.
		var shared int64
		if err := tx.Model(&File{}).
			Where("object_key = ? AND deleted_at IS NULL", record.ObjectKey).
			Count(&shared).Error; err != nil {
			return fmt.Errorf("failed to count object references: %w", err)
		}
		deleteObject = shared == 0
		return nil
	})
	if err != nil {
		return err
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
	if deleteObject {
		// Reclaiming the orphaned object requires the indexer, so build the SDK
		// only on this path (lazily). A remove that leaves other references
		// (deleteObject==false) never touches the network.
		sdk, err := s.ensureSDK()
		if err == nil {
			if err := sdk.DeleteObject(ctx, objHash); err != nil {
				_ = err // best-effort; see comment above
			}
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
	vp, err = RequireActiveProfile(vp)
	if err != nil {
		return "", err
	}

	record, err := s.resolveFile(vp)
	if err != nil {
		return "", err
	}

	objHash, err := parseHash256(record.ObjectKey)
	if err != nil {
		return "", fmt.Errorf("failed to parse object key: %w", err)
	}
	sdk, err := s.ensureSDK()
	if err != nil {
		return "", fmt.Errorf("failed to create Sia SDK: %w", err)
	}
	shareURL, err := sdk.CreateSharedObjectURL(ctx, objHash, validUntil)
	if err != nil {
		return "", fmt.Errorf("failed to create share URL: %w", err)
	}

	return NormalizeShareURL(shareURL), nil
}

// Status reports live vault health and usage. Remote fields come from a real
// probe of the indexer account endpoint (never inferred from local state);
// local fields come from the local index cache.
func (s *vaultService) Status(ctx context.Context) (*StatusResult, error) {
	res := &StatusResult{
		// A service instance is only constructed with a decryption key loaded,
		// so an unlocked local session is implied by reaching this code.
		Unlocked:   true,
		CacheState: "missing",
	}

	// Remote: probe the indexer account endpoint. Success proves reachability;
	// any error means the remote is unreachable and the reason is captured.
	sdk, sdkErr := s.ensureSDK()
	if sdkErr != nil {
		res.RemoteError = sdkErr.Error()
	} else if account, err := sdk.Account(ctx); err != nil {
		res.RemoteError = err.Error()
	} else {
		res.RemoteReachable = true
		res.RemoteReady = account.Ready
		res.StorageUsed = account.PinnedSize
		res.StorageLimit = account.MaxPinnedData
		if account.RemainingStorage <= account.MaxPinnedData {
			res.RemainingStorage = account.RemainingStorage
		}
	}

	// Local: read the index cache. A DB opened by the constructor exists, so a
	// profile that has synced reports counts; a fresh profile reports 0.
	var objects int64
	if err := s.db.Model(&File{}).
		Where("is_current = 1 AND deleted_at IS NULL").
		Count(&objects).Error; err == nil {
		res.ObjectsIndexed = objects
		res.CacheState = "healthy"
	}
	var totalBytes int64
	if err := s.db.Model(&File{}).
		Where("is_current = 1 AND deleted_at IS NULL").
		Select("COALESCE(SUM(size), 0)").
		Scan(&totalBytes).Error; err != nil {
		totalBytes = 0
	}
	res.TotalBytes = totalBytes

	// Last sync time from the most recent cursor row.
	var cursor SyncDownCursor
	if err := s.db.Order("updated_at DESC").First(&cursor).Error; err == nil && !cursor.UpdatedAt.IsZero() {
		res.LastSyncTime = cursor.UpdatedAt.UTC().Format(time.RFC3339)
	}

	return res, nil
}

// Close releases resources. It is idempotent: fields are nil'ed after being
// closed, so a second call (e.g. an explicit close followed by a deferred
// close) is a no-op rather than re-invoking sdk.Close() on an already-closed
// SDK (which may error or panic).
func (s *vaultService) Close() error {
	var dbErr error
	if s.db != nil {
		if sqlDB, err := s.db.DB(); err == nil {
			dbErr = sqlDB.Close()
		}
		s.db = nil
	}
	// Release the SDK under the same lock that guards ensureSDK, so a Close()
	// that races an in-flight lazy build waits for it and then closes the SDK
	// it produced (previously it could observe sdk==nil and leak the SDK built
	// right after). Mark the service closed so a subsequent ensureSDK returns
	// ErrVaultClosed instead of lazily rebuilding a fresh SDK on a disposed
	// service.
	s.sdkMu.Lock()
	sdk := s.sdk
	s.sdk = nil
	s.closed = true
	s.sdkMu.Unlock()

	if sdk != nil {
		if err := sdk.Close(); err != nil {
			return errors.Join(err, dbErr)
		}
	}
	return dbErr
}

// getOrCreateDirectory creates all intermediate directories for a path.
func (s *vaultService) getOrCreateDirectory(path string) (*uint, error) {
	return resolveVaultDirectory(s.db, path)
}

// resolveVaultDirectory resolves a vault directory path to its DirectoryID,
// creating any missing intermediate directories along the way (root/empty => nil).
// It mirrors getOrCreateDirectory but takes a *gorm.DB so Sync can resolve the
// directory from an object's FileMetadata.Directory using the shared service DB
// BEFORE opening its write transaction (avoiding a single-connection deadlock).
func resolveVaultDirectory(db *gorm.DB, path string) (*uint, error) {
	if path == "" || path == "/" {
		return nil, nil // root directory, NULL FK
	}

	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	currentPath := ""
	var parentID *uint

	for _, part := range parts {
		if part == "" {
			continue
		}
		currentPath = JoinDirPath(currentPath, part)

		var dir Directory
		result := db.Where("path = ?", currentPath).First(&dir)
		if result.Error == gorm.ErrRecordNotFound {
			dir = Directory{
				Path:    currentPath,
				SortKey: part,
			}
			if err := db.Create(&dir).Error; err != nil {
				// A concurrent writer can create the same directory path between
				// our lookup and this insert (unique idx_directories_path).
				// Re-resolve the now-existing row instead of failing.
				if isDirNameConflict(err) {
					var existing Directory
					if qerr := db.Where("path = ?", currentPath).First(&existing).Error; qerr == nil {
						parentID = &existing.ID
						continue
					}
				}
				return nil, fmt.Errorf("failed to create directory %s: %w", currentPath, err)
			}
		} else if result.Error != nil {
			return nil, result.Error
		}
		parentID = &dir.ID
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

// upsertFromMeta applies a synced object's metadata to an existing row (update
// in place, including renames — same UUID row, new name — and resurrection of a
// previously-tombstoned object that re-appears). Marked current; the caller
// promotes it as the (name, dir) winner.
func upsertFromMeta(tx *gorm.DB, existing *File, meta FileMetadata, objectKey string, updatedAt time.Time, dirID *uint) error {
	existing.Name = meta.Name
	existing.DirectoryID = dirID // reflect a move/rename from the object metadata
	existing.ObjectKey = objectKey
	existing.Size = meta.Size
	existing.MediaType = meta.MediaType
	existing.ContentDigest = meta.ContentDigest
	existing.UpdatedAt = updatedAt
	if meta.Metadata != nil {
		// Persist the user metadata carried in the object's FileMetadata so the
		// local row mirrors what the remote object carries after a cache rebuild.
		metaJSON, err := json.Marshal(meta.Metadata)
		if err == nil {
			existing.Metadata = datatypes.JSON(metaJSON)
		}
	}
	// Drop is_current before the save: the row may be switching (name, dir) groups
	// via a rename, and saving it as current in a new group it doesn't own yet
	// would transiently violate idx_files_live_name_dir. promoteCurrent (the
	// caller) demotes the group's existing winner and re-promotes this row
	// atomically afterward.
	existing.IsCurrent = false
	if existing.DeletedAt != nil {
		existing.DeletedAt = nil // resurrect: object re-appeared after a tombstone
	}
	return tx.Save(existing).Error
}

// findCurrentFile resolves the current (winner) live file for a (name, dir).
// Exactly one row per (name, dir) has is_current=1 (enforced by the partial
// unique index idx_files_live_name_dir). Tombstoned (soft-deleted) rows are
// never current, so a removed file is no longer resolvable by path. Returns
// gorm.ErrRecordNotFound if none exists.
func (s *vaultService) findCurrentFile(name string, dirID *uint) (File, error) {
	var f File
	q := s.db.Where("name = ? AND is_current = 1 AND deleted_at IS NULL", name)
	if dirID == nil {
		q = q.Where("directory_id IS NULL")
	} else {
		q = q.Where("directory_id = ?", dirID)
	}
	err := q.First(&f).Error
	return f, err
}

// resolveFile resolves a vault path to its current live File record. It parses
// the path, resolves (or confirms) the parent directory, and loads the current
// non-tombstoned file row for that name. A genuinely-missing directory or
// missing file yields an ErrNotFound-wrapped error (distinguishable via
// errors.Is); any genuine DB/transient failure is returned as a distinct,
// non-ErrNotFound error so callers never mistake a DB outage for a free path.
// Used by Get, Verify, Remove, and Share, which all previously duplicated this
// resolve sequence inline.
func (s *vaultService) resolveFile(vp *VaultPath) (File, error) {
	dirID, err := s.getDirectoryID(vp.Directory)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return File{}, fmt.Errorf("%w: %s", ErrNotFound, vp.Raw)
		}
		return File{}, fmt.Errorf("failed to resolve directory %q: %w", vp.Directory, err)
	}
	f, err := s.findCurrentFile(vp.Name, dirID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return File{}, fmt.Errorf("%w: %s", ErrNotFound, vp.Raw)
		}
		return File{}, fmt.Errorf("failed to resolve file %q: %w", vp.Raw, err)
	}
	return f, nil
}

// adoptPreflight resolves a concurrent-Put conflict BEFORE any write
// transaction is opened. When a freshly-minted path turns out to already have a
// live current winner (another writer committed between our initial
// findCurrentFile read and this preflight), we are the loser: adopt the
// winner's UUID and re-stamp + re-pin the just-uploaded object with it here.
//
// Doing this OUTSIDE the transaction is what keeps the indexer network
// round-trip (PinObject) from running while the single SQLite connection
// (SetMaxOpenConns(1)) holds an open write transaction. It also preserves the
// no-divergence invariant the original in-transaction re-pin enforced: the
// re-pin happens BEFORE any row is committed, so if it fails we return and
// nothing is persisted — Sync can never mint a duplicate from stale-metadata.
//
// rec is updated in place to the adopted identity so the caller's subsequent
// transaction targets the winner's row (overwrite path) instead of re-creating
// a conflicting row. Returns (false, nil) when no adoption is needed.
func (s *vaultService) adoptPreflight(ctx context.Context, obj *siastorage.Object, fileMeta *FileMetadata, name string, dirID *uint, rec *File) (bool, error) {
	sdk, err := s.ensureSDK()
	if err != nil {
		return false, fmt.Errorf("failed to create Sia SDK: %w", err)
	}
	current, err := s.findCurrentFile(name, dirID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// No concurrent winner yet — nothing to adopt.
			return false, nil
		}
		return false, fmt.Errorf("resolve current file for adoption: %w", err)
	}
	if current.UUID == rec.UUID {
		// The path's current winner is already our identity; no adoption needed.
		return false, nil
	}

	// Adopt the winner's UUID. Scope to the current live winner (findCurrentFile
	// already does), so we never force-promote a historical row and violate
	// idx_files_live_name_dir.
	*rec = File{
		UUID:          current.UUID,
		Name:          name,
		DirectoryID:   dirID,
		ObjectKey:     obj.ID().String(),
		Size:          fileMeta.Size,
		MediaType:     fileMeta.MediaType,
		ContentDigest: fileMeta.ContentDigest,
		IsCurrent:     true,
		UpdatedAt:     time.Now().UTC(),
	}
	// Carry forward the winner's prior object key (if any) for post-commit
	// orphan cleanup, and the prior row's created-at.
	rec.CreatedAt = current.CreatedAt

	// Re-stamp the remote object metadata with the adopted identity and re-pin
	// it. This is a network round-trip but runs here, BEFORE the write
	// transaction, so it never blocks the single-connection write lock; and a
	// failure returns before anything is committed (no divergence).
	fileMeta.ID = current.UUID
	rmeta, rerr := fileMeta.JSON()
	if rerr != nil {
		return false, fmt.Errorf("re-stamp object metadata after adopting UUID: %w", rerr)
	}
	obj.UpdateMetadata(rmeta)
	if perr := sdk.PinObject(ctx, *obj); perr != nil {
		return false, fmt.Errorf("re-pin object after adopting UUID: %w", perr)
	}
	return true, nil
}

// promoteCurrent makes targetID the single winner for its (name, dir) group
// within a transaction: it demotes any other live current row in that group and
// promotes targetID. This is the write-side counterpart to findCurrentFile and
// mirrors the reference apps' recalculateCurrentForGroup. It must be called on
// the same *gorm.DB (transaction) that wrote targetID so the promotion and the
// write are atomic.
func promoteCurrent(tx *gorm.DB, name string, dirID *uint, targetID uint) error {
	q := tx.Model(&File{}).Where("name = ? AND is_current = 1 AND deleted_at IS NULL", name)
	if dirID == nil {
		q = q.Where("directory_id IS NULL")
	} else {
		q = q.Where("directory_id = ?", dirID)
	}
	// Demote every other live current row in the group first (this produces the
	// unique-index vacancy that promoting targetID fills).
	if err := q.Update("is_current", false).Error; err != nil {
		return err
	}
	return tx.Model(&File{}).Where("id = ? AND deleted_at IS NULL", targetID).
		Update("is_current", true).Error
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
