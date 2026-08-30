package vault

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"go.sia.tech/core/types"
	"go.sia.tech/indexd/api/app"
	"go.sia.tech/indexd/slabs"
	"go.sia.tech/siastorage"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// packedUpload is the subset of siastorage.PackedUpload used by the vault flush
// engine. Abstracted as an interface (rather than the concrete
// *siastorage.PackedUpload) so tests can inject a fake owning the add/finalize
// behavior. The real *siastorage.PackedUpload satisfies it.
type packedUpload interface {
	Add(ctx context.Context, r io.Reader) (int64, error)
	Finalize(ctx context.Context) ([]siastorage.Object, error)
	Close() error
}

// sdkClient is the subset of siastorage.SDK methods used by vaultService.
// Defined as an interface so tests can use a fake.
type sdkClient interface {
	Account(ctx context.Context) (app.AccountResponse, error)
	Upload(ctx context.Context, obj *siastorage.Object, r io.Reader, opts ...siastorage.UploadOption) error
	// UploadPacked creates a packed upload that batches multiple objects into
	// shared slabs (erasure-coded + encrypted by the SDK). Returns a
	// PackedUpload to Add readers to and Finalize. Used by the background flush
	// so several small vault files share a slab set instead of each paying for a
	// full host-set write.
	UploadPacked(opts ...siastorage.UploadOption) (packedUpload, error)
	PinObject(ctx context.Context, obj siastorage.Object) error
	Object(ctx context.Context, objectKey types.Hash256) (siastorage.Object, error)
	ObjectEvents(ctx context.Context, cursor slabs.Cursor, limit int) ([]siastorage.ObjectEvent, error)
	Download(obj siastorage.Object, opts ...siastorage.DownloadOption) (io.ReadCloser, error)
	DeleteObject(ctx context.Context, key types.Hash256) error
	CreateSharedObjectURL(ctx context.Context, objectKey types.Hash256, validUntil time.Time) (string, error)
	DownloadSharedObject(ctx context.Context, sharedURL string, opts ...siastorage.DownloadOption) (io.ReadCloser, error)
	Close() error
	AppKey() types.PrivateKey
}

// realSDK adapts the concrete *siastorage.SDK to the sdkClient interface. It is
// only needed because Go interface satisfaction does not allow a covariant
// return type: the SDK's UploadPacked returns the concrete
// *siastorage.PackedUpload, while sdkClient declares the packedUpload interface
// so tests can fake it. The adapter narrows the return type.
type realSDK struct {
	*siastorage.SDK
}

func (r *realSDK) UploadPacked(opts ...siastorage.UploadOption) (packedUpload, error) {
	return r.SDK.UploadPacked(opts...)
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

	// FTS5 name-search availability, cached for the service lifetime.
	// Compile-time FTS5 support and the files_fts virtual table presence never
	// change while a service is open, so it is probed once.
	ftsMu      sync.Mutex
	ftsChecked bool
	ftsOK      bool

	// sharedObjectFn overrides the default app.Client.SharedObject call used
	// in ShareAccept. When nil, an app.Client is built from indexerURL. Tests
	// inject it to serve slab metadata without an HTTP server.
	sharedObjectFn func(ctx context.Context, shareURL string) (slabs.SharedObject, []byte, error)

	// uploadsDir is the per-profile directory where Put buffers plaintext bytes
	// before they are uploaded+pinned to Sia. Set at construction; "" disables
	// staging (Put then performs a synchronous durable upload instead).
	uploadsDir string

	// diskUsageLimit caps the total bytes buffered in uploadsDir awaiting
	// upload. 0 = unlimited. When exceeded, Put blocks (up to diskUsageTimeout)
	// waiting for the flush loop to drain+delete staged files, then returns
	// ErrSlowDown. Mirrors s3d's addDiskUsage backpressure.
	diskUsageLimit   int64
	diskUsageTimeout time.Duration

	diskUsageMu sync.Mutex
	diskUsage   int64
	diskWake    chan struct{}

	// flushMu serializes all flush work on this service so a Flush and a
	// FlushPath (or two Flushes) can never race on the same pending row's
	// staged buffer — both might otherwise snapshot the same StagedPath,
	// UploadPacked it twice, and each finalizeDurable deletes it. Mirrors
	// s3d's uploadMu around uploadObjects.
	flushMu sync.Mutex
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
	real, err := builder.SDK(s.appKey)
	if err != nil {
		s.sdkErr = err
		return nil, err
	}
	s.sdk = &realSDK{SDK: real}
	return s.sdk, nil
}

// NewVaultService creates a vault service from an SDK and an open database.
func NewVaultService(sdk *siastorage.SDK, db *gorm.DB) VaultService {
	return &vaultService{
		sdk:    &realSDK{SDK: sdk},
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

	// Open the cache WITHOUT running full goose migrations on every command or
	// constructing the Sia SDK. A profile cache that predates a later schema
	// migration (e.g. a pre-versioning vault lacking files.seq) is a real-world
	// permanent-upgrade gap: OpenDBNoMigrate would open it stale and every write
	// would fail with "no such column: seq". OpenDBUpgradeIfStale runs the
	// pending migrations only when the on-disk schema is actually behind, so an
	// up-to-date cache stays untouched (no migration output, no extra work) and
	// a stale one is upgraded in place. The SDK is built lazily on first use
	// because building it hits the network (CheckAppAuth + refreshHosts); a
	// local-cache-only `ls`/`stat`/`cat` should not pay a multi-second network
	// round-trip.
	db, err := OpenDBUpgradeIfStale(ProfileDBPath(profileName))
	if err != nil {
		return nil, err
	}

	return &vaultService{
		db:               db,
		appKey:           appKey,
		indexerURL:       indexerURL,
		metadata:         metadata,
		uploadsDir:       ProfileUploadsDir(profileName),
		diskUsageTimeout: DefaultDiskUsageTimeout,
	}, nil
}

// VaultServiceOption configures a vaultService at construction.
type VaultServiceOption func(*vaultService)

// WithDiskUsageLimit sets the maximum bytes buffered in the staging directory
// before Put blocks (see addDiskUsage). 0 leaves it unlimited.
func WithDiskUsageLimit(limit int64) VaultServiceOption {
	return func(s *vaultService) { s.diskUsageLimit = limit }
}

// WithUploadsDir overrides the staging directory (tests and embedding use).
func WithUploadsDir(dir string) VaultServiceOption {
	return func(s *vaultService) { s.uploadsDir = dir }
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
		return fmt.Errorf("account is not ready yet: the indexer is still propagating registration on the network; try again in a few seconds")
	}
	return nil
}

// putImmediate performs a synchronous, durable vault write (upload + pin now)
// with no local staging buffer. It is the legacy path used when a service has
// no staging directory configured (e.g. unit tests that inject a fake SDK
// directly and expect a completed object) — production uses the staging Put in
// upload_staging.go.
func (s *vaultService) putImmediate(ctx context.Context, r io.Reader, size int64, vaultPath string, metadata map[string]any) (*File, error) {
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

	if rerr := s.CheckReady(ctx); rerr != nil {
		return nil, rerr
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
		// file; doing so would mint a fresh UUID and break the stable-identity
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

	// Build file metadata. Version identity is computed UP FRONT (before the
	// object is pinned) so the encrypted object metadata — the cross-device sync
	// vehicle — carries the same version_id/seq as the local row. version_id is
	// random/opaque (public handle); seq is the monotonic per-UUID ordering,
	// derived from the local cache so it survives cache rebuilds on the writing
	// device. (A cross-process write racing between this read and the commit is
	// reconciled inside the transaction below; version_id, not seq, is the
	// disambiguator.)
	now := time.Now().UTC().Format(time.RFC3339)
	versionID := newVersionID()
	var curSeq uint
	s.db.Model(&File{}).Where("uuid = ?", fileID).Select("COALESCE(MAX(seq),0)").Scan(&curSeq)
	versionSeq := curSeq + 1
	// Best-effort tag promotion: if the caller's opaque metadata map carries a
	// 'tags' key ([]string or []any of strings), normalize it and seed the
	// object's Metadata['tags'] array so the tags are durable (re-pinned with
	// the object) and searchable. Enables `--tags` at upload.
	putTags := tagsFromMetadata(metadata)
	fileMeta := FileMetadata{
		ID:        fileID,
		VersionID: versionID,
		Seq:       versionSeq,
		Name:      vp.Name,
		Directory: vp.Directory,
		MediaType: mediaType,
		Size:      size,
		CreatedAt: now,
		Metadata:  metadata,
		Status:    FileStatusOK,
	}
	// Seed the planar tags array in the sealed object metadata so sync-down
	// reconstructs file_tags on every device without an extra call.
	if len(putTags) > 0 {
		if fileMeta.Metadata == nil {
			fileMeta.Metadata = map[string]any{}
		}
		fileMeta.Metadata["tags"] = putTags
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
	// winner for its (name, dir) path, atomically. Overwriting an existing
	// path keeps its UUID (same logical file, new content); a brand-new path
	// gets a fresh UUID. The DB write and the is_current promotion happen in
	// one transaction so the partial unique index idx_files_live_name_dir
	// enforces at most one current live row per path.

	// Capture the prior object key is NOT needed: with versioning we preserve
	// every version's content (each prior current row retains its ObjectKey),
	// so no post-write orphan deletion occurs on overwrite.
	nowTs := time.Now().UTC()
	// Persist the user-supplied metadata map on the local File row so it
	// survives cache rebuilds and is returned by Stat. The Sia object already
	// carries it in its encrypted metadata; this is the local copy.
	// Marshal the normalized metadata (with putTags merged) into the row so it
	// matches the sealed object exactly, preventing divergence on sync-down.
	userMetaJSON, err := json.Marshal(fileMeta.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}
	// Project the well-known write-context keys onto the normalized search
	// columns (source/host/agent) from the metadata map being stamped. The
	// object's sealed metadata remains the durable source; these columns are
	// the local searchable cache, reconciled on sync-down like tags.
	recSource, recHost, recAgent := WriteContextColumns(metadata)
	rec := File{
		UUID:        fileID,
		VersionID:   versionID,
		Seq:         versionSeq,
		Name:        vp.Name,
		DirectoryID: dirID,
		Source:      recSource,
		Host:        recHost,
		Agent:       recAgent,
		// A freshly-minted path inserts as is_current=true so the partial
		// unique index idx_files_live_name_dir (which only constrains
		// is_current=1) still fires when a concurrent writer claims the same
		// brand-new path, letting the retry/adopt loop converge instead of
		// leaving an orphaned invisible version. An overwrite path is NOT
		// current at insert: setting is_current would collide with the
		// existing winner's own live row, so promoteCurrent below does that
		// final demote+promote. (adoptPreflight resets this to false on the
		// adopt path so the adopted row never races the winner's live row.)
		IsCurrent:     mintedFresh, // promoteCurrent demotes the prior current + promotes this row atomically
		ObjectKey:     objectKey.String(),
		Size:          size,
		MediaType:     mediaType,
		ContentDigest: contentDigest,
		Metadata:      datatypes.JSON(userMetaJSON),
		Status:        FileStatusOK,
		CreatedAt:     nowTs,
		UpdatedAt:     nowTs,
	}
	// Persist + promote the row. See commitFileRecord for the concurrency
	// notes; adoptPreflight (network) runs outside the write transaction.
	return s.commitFileRecord(ctx, vp, dirID, &rec, putTags, mintedFresh, func() (bool, error) {
		adopted, aerr := s.adoptPreflight(ctx, &obj, &fileMeta, vp.Name, dirID, &rec)
		if aerr != nil {
			return false, fmt.Errorf("failed to adopt concurrent winner: %w", aerr)
		}
		return adopted, nil
	})
}

// Get downloads a file from the vault to the given writer. If the local row
// has no recorded content digest (e.g. an accepted share that nobody has
// decrypted yet), Get computes the SHA-256 from the download stream and
// backfills it onto the local row and sealed object metadata. This backfill is
// best-effort: a failure to persist the digest does not fail the download.
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

	// A not-yet-durable object (still buffered locally awaiting background
	// upload+pin) is served straight from its staged plaintext buffer — no Sia
	// interaction needed, and no ObjectKey to fetch yet. This is what makes a
	// locally-staged write immediately readable within the same MCP instance
	// without forcing a flush.
	if record.StagedPath != "" {
		return copyStaged(w, record.StagedPath)
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

	if record.ContentDigest == "" {
		// Tee through a hasher so the download backfills the digest in a
		// single pass — no second download needed.
		hasher := sha256.New()
		if _, err := io.Copy(io.MultiWriter(w, hasher), reader); err != nil {
			return fmt.Errorf("failed to read content: %w", err)
		}
		computedDigest := hex.EncodeToString(hasher.Sum(nil))
		// Best-effort backfill: the download already succeeded, so a failure
		// to persist the digest must not turn it into a failed Get.
		_ = s.backfillDigest(ctx, vaultPath, computedDigest, obj)
		return nil
	}

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

	// Initialize a non-nil empty slice so an empty listing serializes as [] in
	// the tool envelope ({"status":"ok","value":[]}) rather than null, matching
	// every other list tool (pins_list, websites_list, ...). A nil slice would
	// marshal to null and the MCP result converter drops the value key.
	items := make([]ListItem, 0)

	// List subdirectories (direct children only)
	prefix := dirPath
	if prefix != "/" {
		prefix = prefix + "/"
	}
	var dirs []Directory
	likePattern := escapeLike(prefix) + "%"
	// Direct children only, filtered in SQL: the path must start with the
	// prefix (LIKE) and, after the prefix, contain no further '/'; i.e. it is
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
			Status:    f.Status,
			CreatedAt: f.CreatedAt.Format(time.RFC3339),
			UpdatedAt: f.UpdatedAt.Format(time.RFC3339),
		})
	}

	// When a non-trailing-slash path under a non-root parent returns zero
	// results, the path was treated as a file path listing its parent. If
	// the parent also had no files, the caller likely intended the path as a
	// directory. Retry treating the leaf as a directory (mirrors how a root
	// leaf works), so a missing trailing slash does not silently yield an
	// empty listing for a real directory. The retry calls List with a path
	// whose trailing slash forces IsDir=true, preventing re-entry.
	if len(items) == 0 && !vp.IsDir && vp.Name != "" && vp.Directory != "/" {
		retryPath := VaultScheme + JoinDirPath(vp.Directory, vp.Name) + "/"
		return s.List(ctx, retryPath)
	}

	return items, nil
}

// Search returns live vault FILES matching the request. Results are ordered by
// creation time (newest-first), except when an FTS5 name match is used, in
// which case BM25 relevance is preferred then recency. The Request.Query is an
// optional opaque name substring (case-insensitive, backed by FTS5 trigram
// when available and LIKE otherwise). The Request.Where list is ANDed: tag AND
// membership / OR-within-field, directory prefixes, status / write-context
// columns, creation-time bounds, and negation — compiled by applyWhere (tags
// and dirs via join/prefix SQL, the rest through the queryutil GORM builder).
// Each result carries a full vault path plus the same metadata Stat surfaces,
// so results are directly actionable.
func (s *vaultService) Search(_ context.Context, req SearchRequest) ([]SearchItem, error) {
	q := s.db.Table("files").
		Select("files.*").
		Joins("LEFT JOIN directories ON directories.id = files.directory_id").
		Where("files.is_current = 1 AND files.deleted_at IS NULL")

	q, err := s.applyWhere(q, req.Where)
	if err != nil {
		return nil, err
	}

	// Note: the name predicate is intentionally NOT applied here. It is added
	// at query-execution time so the FTS5-vs-LIKE decision (and its distinct
	// ordering) happens after all structured filters are assembled.

	// Bound results so a metadata search over a large vault never loads every
	// match into memory at once; the tag/path assembly below is batched so a
	// result set of N rows needs a constant number of queries (not 2N+1).
	//
	// Name search execution: try FTS5 (trigram) when it is available and the
	// query is long enough (>=3 chars); otherwise use the original LIKE
	// substring match. A MATCH error also falls back to LIKE, so behavior never
	// regresses. FTS orders by BM25 relevance then recency; the LIKE and
	// no-name paths order by recency alone.
	searchLimit := req.Limit
	if searchLimit <= 0 {
		searchLimit = 500
	}
	var records []File
	searched := false
	if name := strings.TrimSpace(req.Query); name != "" {
		if param, ok := ftsMatchParam(name); ok && s.ftsAvailable() {
			// ftsSearch runs on a clone of q so a failed MATCH never corrupts
			// the base predicates used by the LIKE fallback below.
			searched = s.ftsSearch(q, param, searchLimit, &records)
		}
		if !searched {
			q = q.Where("files.name LIKE ? ESCAPE '\\'", "%"+escapeLike(req.Query)+"%")
		}
	}
	if !searched {
		if err := q.Order("files.created_at DESC").Limit(searchLimit).Find(&records).Error; err != nil {
			return nil, fmt.Errorf("search failed: %w", err)
		}
	}
	if len(records) == 0 {
		return []SearchItem{}, nil
	}

	// Batch-load directory paths in one query so building the full vault path
	// does not issue a per-result row lookup.
	dirByID := map[uint]string{}
	if ids := lo.FilterMap(records, func(rec File, _ int) (uint, bool) {
		if rec.DirectoryID == nil {
			return 0, false
		}
		return *rec.DirectoryID, true
	}); len(ids) > 0 {
		var dirs []Directory
		if err := s.db.Where("id IN ?", ids).Find(&dirs).Error; err == nil {
			dirByID = lo.SliceToMap(dirs, func(d Directory) (uint, string) { return d.ID, d.Path })
		}
	}

	// Batch-load each result row's tags in one query joining file_tags + tags.
	tagsByID := map[uint][]string{}
	{
		ids := make([]uint, 0, len(records))
		for _, rec := range records {
			ids = append(ids, rec.ID)
		}
		type tagRow struct {
			FileID uint
			Name   string
		}
		var rows []tagRow
		if err := s.db.Table("file_tags").
			Select("file_tags.file_id, tags.name").
			Joins("JOIN tags ON tags.id = file_tags.tag_id").
			Where("file_tags.file_id IN ?", ids).
			Scan(&rows).Error; err == nil {
			for _, r := range rows {
				tagsByID[r.FileID] = append(tagsByID[r.FileID], r.Name)
			}
		}
	}

	items := make([]SearchItem, 0, len(records))
	for _, rec := range records {
		path := rec.Name
		if rec.DirectoryID != nil {
			if p, ok := dirByID[*rec.DirectoryID]; ok {
				path = JoinDirPath(p, rec.Name)
			}
		}
		var metaOut map[string]any
		if len(rec.Metadata) > 0 {
			_ = json.Unmarshal(rec.Metadata, &metaOut) // best-effort
		}
		items = append(items, SearchItem{
			Path:          VaultScheme + path,
			Name:          rec.Name,
			Size:          rec.Size,
			MediaType:     rec.MediaType,
			ContentDigest: rec.ContentDigest,
			ObjectID:      rec.ObjectKey,
			Status:        rec.Status,
			CreatedAt:     rec.CreatedAt.Format(time.RFC3339),
			UpdatedAt:     rec.UpdatedAt.Format(time.RFC3339),
			Tags:          tagsByID[rec.ID],
			Metadata:      metaOut,
			Source:        rec.Source,
			Host:          rec.Host,
			Agent:         rec.Agent,
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
		// lists. Match List's root-leaf handling: if the leaf names an
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
	// Load the file's first-class tags from the local join (a cache of the
	// authoritative Metadata['tags'] array in the object). Best-effort: a
	// read failure yields an empty tag list rather than failing Stat.
	tags, _ := s.currentTags(record.ID)
	return &StatResult{
		Type:          "file",
		Name:          record.Name,
		Path:          vaultPath,
		Size:          record.Size,
		MediaType:     record.MediaType,
		ContentDigest: record.ContentDigest,
		ObjectID:      record.ObjectKey,
		Status:        record.Status,
		LostReason:    record.LostReason,
		CreatedAt:     record.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     record.UpdatedAt.Format(time.RFC3339),
		Metadata:      metaOut,
		Tags:          tags,
		Source:        record.Source,
		Host:          record.Host,
		Agent:         record.Agent,
	}, nil
}

// Cat streams file content to the writer.
func (s *vaultService) Cat(ctx context.Context, vaultPath string, w io.Writer) error {
	return s.Get(ctx, vaultPath, w)
}

// Verify checks content integrity: object existence on the indexer and a
// digest match. It is deliberately SHALLOW: it compares the stored digest in
// the object's metadata against the local row's ContentDigest WITHOUT
// downloading the full file content, so it is cheap even for large encrypted
// files. Use VerifyDeep for a true full-content re-hash.
//
// An accepted share has no recorded digest until someone decrypts the content
// (via Get or VerifyDeep). Until then, DigestVerified is "unverified" — the
// file may be perfectly fine; the tool simply has no digest to compare against.
func (s *vaultService) Verify(ctx context.Context, vaultPath string) (*VerifyResult, error) {
	res, obj, exists, err := s.resolveVerifyObject(ctx, vaultPath)
	if err != nil || !exists {
		if res != nil {
			if res.ObjectExists {
				res.DigestVerified = DigestVerifiedUnverified
			} else {
				res.DigestVerified = DigestVerifiedMismatch
			}
		}
		return res, err
	}

	if res.ContentDigest == "" {
		// No digest has ever been recorded. This is expected for accepted
		// shares before first decrypt. It is NOT a corruption signal.
		res.DigestVerified = DigestVerifiedUnverified
		return res, nil
	}

	// Shallow integrity: trust the digest the object's metadata declares, and
	// compare it to the local row's ContentDigest. No content download.
	objDigest := ""
	if rawMeta := obj.Metadata(); len(rawMeta) > 0 {
		if m, merr := ParseFileMetadata(rawMeta); merr == nil {
			objDigest = m.ContentDigest
		}
	}
	if objDigest != "" && objDigest == res.ContentDigest {
		res.DigestMatch = boolPtr(true)
		res.DigestVerified = DigestVerifiedVerified
		// Only a matching digest proves the object is present and correct; a
		// divergence (present-but-corrupt object) must NOT clear lost state.
		s.clearLostStatus(ctx, vaultPath)
	} else if objDigest != "" {
		// Both hashes exist and differ: a genuine mismatch.
		res.DigestMatch = boolPtr(false)
		res.DigestVerified = DigestVerifiedMismatch
	} else {
		// Cache miss: the object's sealed metadata carries no digest to compare
		// against the local row. There is only one hash, so this is NOT a
		// mismatch — we simply cannot verify without re-hashing. Leave
		// DigestMatch nil ("no verdict").
		res.DigestVerified = DigestVerifiedUnverified
	}
	return res, nil
}

// VerifyDeep downloads the full object content and recomputes SHA-256 so
// DigestMatch reflects actual bytes on the indexer rather than the
// metadata-declared digest. This transfers the entire file over the network;
// use it only when a true integrity check is required.
//
// If no digest has been recorded (e.g. an accepted share that nobody has
// decrypted yet), VerifyDeep populates it: it downloads, hashes, and writes
// the computed digest back to the local row and the sealed object metadata.
// Subsequent shallow verifies then match without a download.
func (s *vaultService) VerifyDeep(ctx context.Context, vaultPath string) (*VerifyResult, error) {
	res, obj, exists, err := s.resolveVerifyObject(ctx, vaultPath)
	if err != nil || !exists {
		if res != nil {
			if res.ObjectExists {
				res.DigestVerified = DigestVerifiedUnverified
			} else {
				res.DigestVerified = DigestVerifiedMismatch
			}
		}
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

	if res.ContentDigest == "" {
		// Backfill: persist the computed digest to the local row and the
		// sealed object metadata so future shallow verifies match without a
		// download. Best-effort: the content was already downloaded and
		// hashed successfully, so a transient re-pin or DB failure must not
		// discard the verification result.
		_ = s.backfillDigest(ctx, vaultPath, computedDigest, obj)
		res.ContentDigest = computedDigest
		res.DigestMatch = boolPtr(true)
		res.DigestVerified = DigestVerifiedVerified
		s.clearLostStatus(ctx, vaultPath)
		return res, nil
	}

	if computedDigest == res.ContentDigest {
		res.DigestMatch = boolPtr(true)
		res.DigestVerified = DigestVerifiedVerified
		s.clearLostStatus(ctx, vaultPath)
	} else {
		res.DigestMatch = boolPtr(false)
		res.DigestVerified = DigestVerifiedMismatch
	}
	return res, nil
}

// clearLostStatus resets a file's lifecycle status back to ok and clears its
// lost_reason after a successful verify. It is best-effort (a failed status
// write must not fail the verify operation itself); the next verify will
// re-derive state.
func (s *vaultService) clearLostStatus(ctx context.Context, vaultPath string) {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return
	}
	vp, err = RequireActiveProfile(vp)
	if err != nil {
		return
	}
	dirID, err := s.getDirectoryID(vp.Directory)
	if err != nil {
		return
	}
	// Scope to the EXACT current file (name within its directory), never
	// clearing lost state for same-named files living in other directories.
	if rec, err := s.findCurrentFile(vp.Name, dirID); err == nil {
		_ = s.db.Model(&File{}).
			Where("id = ?", rec.ID).
			Updates(map[string]any{
				"status":      FileStatusOK,
				"lost_reason": "",
			}).Error
	}
}

// DigestVerified tri-state values for VerifyResult.
const (
	DigestVerifiedVerified   = "verified"
	DigestVerifiedUnverified = "unverified"
	DigestVerifiedMismatch   = "mismatch"
)

// boolPtr returns a pointer to b for nullable boolean fields (e.g.
// VerifyResult.DigestMatch), keeping the tri-state "nil = no verdict"
// distinguishable from an explicit false.
func boolPtr(b bool) *bool { return &b }

// backfillDigest persists a computed SHA-256 digest onto the local File row
// and the sealed object metadata (re-pin). This is called from Get (after a
// successful download) and VerifyDeep (after hashing) when the row has no
// recorded digest — the expected state for an accepted share that nobody has
// decrypted yet.
//
// The re-pin updates the object's sealed FileMetadata so other devices
// syncing from the indexer also receive the digest. A failure here is
// best-effort from Get's perspective (the download already succeeded) but
// fatal from VerifyDeep's perspective (the caller asked for verification).
func (s *vaultService) backfillDigest(ctx context.Context, vaultPath, computedDigest string, obj siastorage.Object) error {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return err
	}
	vp, err = RequireActiveProfile(vp)
	if err != nil {
		return err
	}
	dirID, err := s.getDirectoryID(vp.Directory)
	if err != nil {
		return err
	}
	rec, err := s.findCurrentFile(vp.Name, dirID)
	if err != nil {
		return err
	}

	// Update the sealed object metadata so the digest syncs to other devices.
	var meta FileMetadata
	if raw := obj.Metadata(); len(raw) > 0 {
		if m, merr := ParseFileMetadata(raw); merr == nil {
			meta = m
		}
	}
	meta.ContentDigest = computedDigest
	metaJSON, err := meta.JSON()
	if err != nil {
		return err
	}
	obj.UpdateMetadata(metaJSON)

	sdk, err := s.ensureSDK()
	if err != nil {
		return err
	}
	if err := sdk.PinObject(ctx, obj); err != nil {
		return fmt.Errorf("failed to re-pin object with backfilled digest: %w", err)
	}

	// Update the local row.
	return s.db.Model(&File{}).
		Where("id = ?", rec.ID).
		Updates(map[string]any{
			"content_digest": computedDigest,
			"updated_at":     time.Now().UTC(),
		}).Error
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
			result.DigestMatch = boolPtr(false)
			// Mark the local row lost so the lifecycle state is visible in
			// vault_ls / vault_status / vault_stat even before anyone re-verifies.
			// A lost file stays listed (never tombstoned) so an agent can
			// enumerate and recover it. The lost_reason records the terminal
			// slab-unavailable detail.
			if uerr := s.db.Model(&File{}).
				Where("id = ?", record.ID).
				Updates(map[string]any{
					"status":      FileStatusLost,
					"lost_reason": slabs.ErrObjectNotFound.Error(),
					"updated_at":  time.Now().UTC(),
				}).Error; uerr != nil {
				// A failed status write must not mask the genuine verify result;
				// surface it as an error so the caller knows state diverged.
				return nil, siastorage.Object{}, false, fmt.Errorf("failed to mark file lost: %w", uerr)
			}
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
	// zero remaining live references and deletes; the object cannot be orphaned
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
		// about to tombstone; only the final remover sees zero.
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
	// is already gone; misleading the caller. Treat it as best-effort.
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

// Share generates a time-limited share URL for a file. The URL is an https://
// pre-signed indexer URL carrying the object's encryption key in its fragment
// (#encryption_key=…). It is consumable directly by ShareAccept.
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

	return shareURL, nil
}

// ShareAccept implements the A2A slab-reference pin primitive. The share URL
// is a time-limited, read-only bearer of a single object's content; accepting
// a share fetches only the slab metadata (no content download) and pins those
// slab references into the accepting profile's indexer account. The accepting
// profile owns an independent object pointing at the same Sia sectors — no
// data is transferred. A write-only audit row is appended to the share ledger.
func (s *vaultService) ShareAccept(ctx context.Context, vaultPath, shareURL, targetPrincipal string, metadata map[string]any) (*File, error) {
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
	if shareURL == "" {
		return nil, fmt.Errorf("share_url is required")
	}

	if rerr := s.CheckReady(ctx); rerr != nil {
		return nil, rerr
	}

	// Rewrite the agent-supplied share URL to the profile's configured indexer
	// origin (scheme + host). The path, query, and fragment (which carries
	// the encryption key) are preserved. This guarantees the HTTP GET issued
	// by app.Client.SharedObject always targets the trusted indexer,
	// eliminating SSRF regardless of what host the agent supplied.
	resolvedURL, err := resolveShareURL(shareURL, s.indexerURL)
	if err != nil {
		return nil, err
	}

	// Fetch the shared object's slab metadata + encryption key via a metadata-
	// only HTTP GET. app.SharedObject parses the #encryption_key fragment,
	// hits the shared URL (pre-signed), and returns the slab slices + 32-byte
	// key. No content is downloaded from Sia hosts.
	var sharedObj slabs.SharedObject
	var encryptionKey []byte
	if s.sharedObjectFn != nil {
		sharedObj, encryptionKey, err = s.sharedObjectFn(ctx, resolvedURL)
	} else {
		appClient := app.NewClient(s.indexerURL)
		sharedObj, encryptionKey, err = appClient.SharedObject(ctx, resolvedURL)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve shared object: %w", err)
	}
	if len(encryptionKey) != 32 {
		return nil, fmt.Errorf("invalid encryption key length: %d", len(encryptionKey))
	}
	var dataKey [32]byte
	copy(dataKey[:], encryptionKey)

	sdk, err := s.ensureSDK()
	if err != nil {
		return nil, fmt.Errorf("failed to create Sia SDK: %w", err)
	}

	// Refuse to silently overwrite an existing live file at the destination.
	if dirID, derr := s.getDirectoryID(vp.Directory); derr == nil {
		if _, ferr := s.findCurrentFile(vp.Name, dirID); ferr == nil {
			return nil, fmt.Errorf("destination already exists (refusing to overwrite): %s", vp.FullPath())
		}
	}

	// Build the Object from the shared slabs + data key. NewUnsafeObject
	// creates a fresh Object that references the SAME sectors on the SAME Sia
	// hosts as the shared object — no content is copied.
	obj := siastorage.NewUnsafeObject(dataKey, sharedObj.Slabs)
	size := int64(obj.Size())

	// Directory + media type + version identity (mirrors Put).
	dirID, err := s.getOrCreateDirectory(vp.Directory)
	if err != nil {
		return nil, err
	}
	mediaType := mime.TypeByExtension(filepath.Ext(vp.Name))

	fileID := ""
	if current, err := s.findCurrentFile(vp.Name, dirID); err == nil {
		fileID = current.UUID
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("failed to resolve current file for %s: %w", vp.Name, err)
	}
	mintedFresh := false
	if fileID == "" {
		fileID = uuid.NewString()
		mintedFresh = true
	}

	now := time.Now().UTC().Format(time.RFC3339)
	versionID := newVersionID()
	var curSeq uint
	s.db.Model(&File{}).Where("uuid = ?", fileID).Select("COALESCE(MAX(seq),0)").Scan(&curSeq)
	versionSeq := curSeq + 1

	putTags := tagsFromMetadata(metadata)
	fileMeta := FileMetadata{
		ID:        fileID,
		VersionID: versionID,
		Seq:       versionSeq,
		Name:      vp.Name,
		Directory: vp.Directory,
		MediaType: mediaType,
		Size:      size,
		CreatedAt: now,
		Metadata:  metadata,
		Status:    FileStatusOK,
	}
	if len(putTags) > 0 {
		if fileMeta.Metadata == nil {
			fileMeta.Metadata = map[string]any{}
		}
		fileMeta.Metadata["tags"] = putTags
	}

	// Stamp file metadata on the object before pinning so the sealed object
	// carries the same version_id/seq/tags as the local row (cross-device
	// sync-down reconstructs the row from this).
	metaJSON, err := fileMeta.JSON()
	if err != nil {
		return nil, err
	}
	obj.UpdateMetadata(metaJSON)

	// Pin slab references + object metadata into this profile's indexer
	// account. PinObject seals the object with the accepting profile's app
	// key, pins any unpinned slabs (metadata-only, no data transfer), and
	// records the object. This is idempotent by object ID.
	if err := sdk.PinObject(ctx, obj); err != nil {
		return nil, fmt.Errorf("failed to pin shared object: %w", err)
	}

	objectKey := obj.ID().String()

	// Create the local DB record. This mirrors Put's post-network path:
	// UUID minting, version tracking, concurrent-create resolution, tag
	// reconciliation, is_current promotion.
	userMetaJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}
	recSource, recHost, recAgent := WriteContextColumns(metadata)
	rec := File{
		UUID:        fileID,
		Name:        vp.Name,
		DirectoryID: dirID,
		Source:      recSource,
		Host:        recHost,
		Agent:       recAgent,
		IsCurrent:   mintedFresh,
		ObjectKey:   objectKey,
		Size:        size,
		MediaType:   mediaType,
		Metadata:    datatypes.JSON(userMetaJSON),
		Status:      FileStatusOK,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	const maxAdoptRetries = 4
	for attempt := 0; attempt < maxAdoptRetries; attempt++ {
		if mintedFresh {
			if adopted, aerr := s.adoptPreflight(ctx, &obj, &fileMeta, vp.Name, dirID, &rec); aerr != nil {
				return nil, fmt.Errorf("failed to adopt concurrent winner: %w", aerr)
			} else if adopted {
				fileID = rec.UUID
			}
		}

		err = s.db.Transaction(func(tx *gorm.DB) error {
			var maxSeq uint
			if err := tx.Model(&File{}).
				Where("uuid = ?", rec.UUID).
				Select("COALESCE(MAX(seq), 0)").
				Scan(&maxSeq).Error; err != nil {
				return fmt.Errorf("failed to compute next version seq: %w", err)
			}
			if versionSeq > maxSeq {
				maxSeq = versionSeq
			}
			rec.Seq = maxSeq + 1
			rec.VersionID = versionID

			if err := tx.Create(&rec).Error; err != nil {
				return err
			}
			if len(putTags) > 0 {
				if rerr := reconcileTagsTx(tx, rec.ID, putTags); rerr != nil {
					return rerr
				}
			}
			return promoteCurrent(tx, vp.Name, dirID, rec.ID)
		})
		if err == nil {
			break
		}
		if !isLiveNameConflict(err) {
			return nil, fmt.Errorf("failed to store file record: %w", err)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to store file record after %d attempts: %w", maxAdoptRetries, err)
	}

	if err := s.db.Create(&ShareLedger{
		SharedVaultPath: vp.FullPath(),
		ObjectKey:       objectKey,
		Expiry:          nil,
		TargetPrincipal: targetPrincipal,
		CreatedAt:       time.Now().UTC(),
	}).Error; err != nil {
		return nil, fmt.Errorf("failed to record share accept in ledger: %w", err)
	}

	return &rec, nil
}

// VersionList returns every live version row for the file at vaultPath, newest
// first. It resolves the current (live) winner for the path to establish the
// stable UUID group, then returns every non-tombstoned version row sharing that
// UUID. This is the read counterpart to the versioning write path: overwrites
// mint new version rows, so listing returns the full history.
func (s *vaultService) VersionList(ctx context.Context, vaultPath string) ([]*File, error) {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return nil, err
	}
	vp, err = RequireActiveProfile(vp)
	if err != nil {
		return nil, err
	}

	// Resolve the current winner to pin down the UUID group. A logical file
	// (one UUID) may have many version rows; listing a path returns that
	// UUID's history. A missing file (never existed) is an empty list, not an
	// error, matching List's semantics.
	current, err := s.resolveFile(vp)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	var rows []*File
	if err := s.db.Where("uuid = ? AND deleted_at IS NULL", current.UUID).
		Order("seq DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list versions for %s: %w", vaultPath, err)
	}
	return rows, nil
}

// resolveVersionGroup resolves the file at vaultPath to its UUID group and the
// requested version row (by version_id). It returns gorm.ErrRecordNotFound when
// the version is missing so callers can map it to ErrNotFound.
func (s *vaultService) resolveVersionGroup(vp *VaultPath, versionID string) (string, *File, error) {
	current, err := s.resolveFile(vp)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", nil, fmt.Errorf("%w: %s", ErrNotFound, vp.Raw)
		}
		return "", nil, err
	}
	var row File
	if err := s.db.Where("uuid = ? AND version_id = ? AND deleted_at IS NULL", current.UUID, versionID).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil, fmt.Errorf("%w: version %q of %s", ErrNotFound, versionID, vp.Raw)
		}
		return "", nil, fmt.Errorf("failed to resolve version %q of %s: %w", versionID, vp.Raw, err)
	}
	return current.UUID, &row, nil
}

// VersionGet returns the specific version record of the file at vaultPath.
func (s *vaultService) VersionGet(ctx context.Context, vaultPath string, versionID string) (*File, error) {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return nil, err
	}
	vp, err = RequireActiveProfile(vp)
	if err != nil {
		return nil, err
	}
	_, row, err := s.resolveVersionGroup(vp, versionID)
	if err != nil {
		return nil, err
	}
	return row, nil
}

// VersionDownload streams a specific version's content (by version_id) to the
// writer, downloading the version's own content-addressed ObjectKey. This lets a
// user/agent retrieve archival content that is no longer the live winner.
func (s *vaultService) VersionDownload(ctx context.Context, vaultPath string, versionID string, w io.Writer) error {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return err
	}
	vp, err = RequireActiveProfile(vp)
	if err != nil {
		return err
	}
	_, row, err := s.resolveVersionGroup(vp, versionID)
	if err != nil {
		return err
	}

	objHash, err := parseHash256(row.ObjectKey)
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

// VersionRestore retargets a specific historical version to be the NEW current
// version of the file at vaultPath. Unlike an overwrite, the historical
// version's slabs remain pinned on the Sia indexer from its original PIN, so a
// restore does NOT re-upload the bytes: it mints a fresh current version row
// pointing at the SAME ObjectKey as the historical version and promotes it,
// tombstoning the previous current winner. Version identity is computed up
// front and re-sealed onto the object's metadata (a metadata-only re-pin, no
// content transfer) BEFORE the DB commit, mirroring Put, so sync-down on other
// devices reconstructs the restored (uuid, version_id) row instead of the stale
// historical one. All prior version rows are preserved (restore-as-new-version,
// matching s3d's CopyObject semantics).
func (s *vaultService) VersionRestore(ctx context.Context, vaultPath string, versionID string) (*File, error) {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return nil, err
	}
	// Resolve the target version (validates it exists and supplies its
	// ObjectKey, digest, size, media type) and the logical file's stable UUID
	// group. An empty/broken version errors here before any write occurs.
	uuid, row, err := s.resolveVersionGroup(vp, versionID)
	if err != nil {
		return nil, err
	}
	if uuid == "" {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, vaultPath)
	}

	// Resolve the current live winner: its directory anchors the (name, dir)
	// unique-index scope, and its tags must be preserved onto the restored
	// winner row (otherwise the new row comes up with no tags, silently
	// dropping the label set).
	current, err := s.resolveFile(vp)
	if err != nil {
		return nil, err
	}
	var meta map[string]any
	if tags, err := s.currentTags(current.ID); err == nil && len(tags) > 0 {
		meta = map[string]any{"tags": tags}
	}
	putTags := tagsFromMetadata(meta)
	recSource, recHost, recAgent := WriteContextColumns(meta)

	now := time.Now().UTC()

	// The restored row's opaque metadata starts from the restored content's own
	// metadata and has the preserved tags stamped on top, so the local copy
	// matches what a fresh Put of that content would have sealed.
	userMeta := map[string]any{}
	if len(row.Metadata) > 0 {
		if err := json.Unmarshal(row.Metadata, &userMeta); err != nil {
			return nil, fmt.Errorf("failed to decode restored version metadata: %w", err)
		}
	}
	if len(putTags) > 0 {
		userMeta["tags"] = putTags
	}
	userMetaJSON, err := json.Marshal(userMeta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Compute the restored version identity UP FRONT so the object's sealed
	// metadata and the local row carry the same version_id/seq (Put does the
	// same: identity is stamped into the object before the row is committed,
	// and sync reconstructs rows from the sealed metadata).
	newVersionID := newVersionID()
	var curSeq uint
	if err := s.db.Model(&File{}).
		Where("uuid = ?", uuid).
		Select("COALESCE(MAX(seq), 0)").
		Scan(&curSeq).Error; err != nil {
		return nil, fmt.Errorf("failed to compute next version seq: %w", err)
	}
	versionSeq := curSeq + 1

	rec := File{
		UUID:          uuid,
		Name:          vp.Name,
		DirectoryID:   current.DirectoryID,
		Source:        recSource,
		Host:          recHost,
		Agent:         recAgent,
		IsCurrent:     false, // promoteCurrent demotes the prior current + promotes this row atomically
		ObjectKey:     row.ObjectKey,
		Size:          row.Size,
		MediaType:     row.MediaType,
		ContentDigest: row.ContentDigest,
		Status:        FileStatusOK,
		CreatedAt:     now,
		UpdatedAt:     now,
		Seq:           versionSeq,
		VersionID:     newVersionID,
		Metadata:      datatypes.JSON(userMetaJSON),
	}

	// Re-seal the object's metadata with the restored version identity and
	// re-pin (metadata-only — no content is transferred, so this is NOT the
	// content upload whose pin deadline killed restores). Without this, sync-down
	// reads the object's stale historical version_id/seq and reconstructs the
	// historical row, silently undoing the restore on other devices.
	objHash, err := parseHash256(row.ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse restored object key: %w", err)
	}
	sdk, err := s.ensureSDK()
	if err != nil {
		return nil, fmt.Errorf("failed to create Sia SDK: %w", err)
	}
	obj, err := sdk.Object(ctx, objHash)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch restored object from indexer: %w", err)
	}
	restoredMeta := FileMetadata{
		ID:            uuid,
		VersionID:     newVersionID,
		Seq:           versionSeq,
		Name:          vp.Name,
		Directory:     vp.Directory,
		MediaType:     row.MediaType,
		Size:          row.Size,
		CreatedAt:     now.Format(time.RFC3339),
		ContentDigest: row.ContentDigest,
		Metadata:      userMeta,
		Status:        FileStatusOK,
	}
	restoredMetaJSON, err := restoredMeta.JSON()
	if err != nil {
		return nil, fmt.Errorf("failed to encode restored object metadata: %w", err)
	}
	obj.UpdateMetadata(restoredMetaJSON)
	if err := sdk.PinObject(ctx, obj); err != nil {
		return nil, fmt.Errorf("failed to re-pin restored object: %w", err)
	}

	// Mint and promote the restored winner in one write transaction (fresh row
	// insert + tag reconcile + promoteCurrent). rec.VersionID/Seq were already
	// stamped into the object metadata before this commit, so the row uses the
	// SAME values — the vault DB is a single-connection SQLite (SetMaxOpenConns
	// 1), so write transactions serialize and there is no cross-writer seq race
	// to defend against here (matching how the object and row stay in lockstep).
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&rec).Error; err != nil {
			return err
		}
		if len(putTags) > 0 {
			if rerr := reconcileTagsTx(tx, rec.ID, putTags); rerr != nil {
				return rerr
			}
		}
		return promoteCurrent(tx, vp.Name, current.DirectoryID, rec.ID)
	}); err != nil {
		return nil, fmt.Errorf("failed to restore version %q of %s: %w", versionID, vaultPath, err)
	}
	return &rec, nil
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

	// Lost-file aggregate: count live current files flagged as lost so
	// vault_status surfaces how much content is unrecoverable.
	var lost int64
	if err := s.db.Model(&File{}).
		Where("is_current = 1 AND deleted_at IS NULL AND status = ?", FileStatusLost).
		Count(&lost).Error; err == nil {
		res.LostCount = lost
	}

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
// It takes a *gorm.DB so Sync can resolve the directory from an object's
// FileMetadata.Directory using the shared service DB BEFORE opening its write
// transaction (avoiding a single-connection deadlock).
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
// in place, including renames (same UUID row, new name) and resurrection of a
// previously-tombstoned object that re-appears). Marked current; the caller
// promotes it as the (name, dir) winner.
func upsertFromMeta(tx *gorm.DB, existing *File, meta FileMetadata, objectKey string, updatedAt time.Time, dirID *uint) error {
	existing.Name = meta.Name
	existing.DirectoryID = dirID // reflect a move/rename from the object metadata
	existing.ObjectKey = objectKey
	existing.VersionID = meta.VersionID // may change for legacy->versioned; keep in sync
	existing.Seq = meta.Seq
	existing.Size = meta.Size
	existing.MediaType = meta.MediaType
	existing.ContentDigest = meta.ContentDigest
	// Lifecycle status is deliberately left untouched on the existing-row path:
	// a file marked "lost" locally (Verify is a DB-only write that never re-pins
	// the object) must not be silently reset to "ok" when a later sync
	// re-processes the object, whose sealed metadata still carries the "ok"
	// stamped at Put time. Lost state is only cleared by an explicit
	// digest-matching Verify or a fresh Put — never by a passive re-sync. Fresh
	// rows (sync-down to a new device) still seed status from the object via
	// sync.go's create branch.
	existing.UpdatedAt = updatedAt
	if meta.Metadata != nil {
		// Persist the user metadata carried in the object's FileMetadata so the
		// local row matches what the remote object carries after a cache rebuild.
		metaJSON, err := json.Marshal(meta.Metadata)
		if err == nil {
			existing.Metadata = datatypes.JSON(metaJSON)
		}
		// Reconcile the normalized write-context columns from the object's
		// sealed metadata (same cache-as-remote pattern as tags), so a synced
		// copy reconstructs src/host/agent on every device.
		existing.Source, existing.Host, existing.Agent = WriteContextColumns(meta.Metadata)
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
	if err := tx.Save(existing).Error; err != nil {
		return err
	}
	// Reconcile the file_tags join from the object's authoritative sealed
	// Metadata['tags'] array in the same transaction. This is what makes tags
	// survive cache rebuilds and sync to every device: the local join is always
	// derived from the object metadata here, never an independent authority.
	var objTags []string
	if m, ok := meta.Metadata["tags"]; ok {
		switch v := m.(type) {
		case []string:
			objTags = v
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					objTags = append(objTags, s)
				}
			}
		}
	}
	return reconcileTagsTx(tx, existing.ID, normalizeTags(objTags))
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
// Used by Get, Verify, Remove, and Share.
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
// no-divergence invariant: the re-pin happens BEFORE any row is committed, so
// if it fails we return and nothing is persisted; Sync can never mint a
// duplicate from stale-metadata.
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
			// No concurrent winner yet; nothing to adopt.
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
	// idx_files_live_name_dir. The caller's user metadata (rec.Metadata) is
	// carried forward so the adopted row — the new current winner — surfaces the
	// same metadata as the overwrite branch and the re-pinned object.
	*rec = File{
		UUID:          current.UUID,
		Name:          name,
		DirectoryID:   dirID,
		ObjectKey:     obj.ID().String(),
		Size:          fileMeta.Size,
		MediaType:     fileMeta.MediaType,
		ContentDigest: fileMeta.ContentDigest,
		Metadata:      rec.Metadata,
		IsCurrent:     false, // promoteCurrent promotes this adopted row after insert
		Status:        FileStatusOK,
		UpdatedAt:     time.Now().UTC(),
	}
	// Re-derive the normalized write-context columns from the caller's metadata
	// map (carried forward into rec.Metadata above) so the adopted row keeps the
	// same source/host/agent projection as the overwrite branch and the object.
	rec.Source, rec.Host, rec.Agent = WriteContextColumns(fileMeta.Metadata)
	// Carry forward the prior row's created-at (stable identity's birth time).
	// The winner's prior ObjectKey is intentionally NOT carried: the adopted
	// object re-pin below stamps the new content, and the winner's old row
	// (now demoted by promoteCurrent) retains its own ObjectKey as history.
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
// promotes targetID. This is the write-side counterpart to findCurrentFile. It
// must be called on the same *gorm.DB (transaction) that wrote targetID so the
// promotion and the write are atomic.
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

// newVersionID returns a random, opaque version handle for a file version.
// It mirrors s3d's version-id model: random opaque hex (never sequential, never
// guessable, collision-free). Version IDs are the public handle callers pass to
// vault_version_show/restore --version <id>; seq (not version_id) is the
// canonical ordering within a UUID group.
func newVersionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand never fails on supported platforms; fall back to a
		// UUID-based handle so a (theoretical) RNG failure cannot wedge a Put.
		return uuid.NewString()
	}
	return hex.EncodeToString(b)
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

// ===========================================================================
// FTS5 name search
//
// The vault name search is backed by a SQLite FTS5 (trigram) index over
// files.name when it is available, with a LIKE fallback. `query` is always an
// opaque filename substring: it is never parsed into AND/OR/NOT, field:value,
// or phrase operators. The whole input is wrapped as a single FTS5 quoted
// phrase so any FTS5 special characters it contains are literal.
// ===========================================================================

// ftsMatchParam returns a safe, single-phrase FTS5 MATCH string for the given
// raw name substring. It reports false when trigram search is unusable for the
// input (<3 runes), in which case the caller must fall back to LIKE (where
// shorter substrings still match). Quoting the entire input prevents user
// input from ever acting as FTS5 operator syntax.
func ftsMatchParam(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) < 3 {
		return "", false
	}
	// A "..." phrase wraps the whole input; any internal double-quote is
	// escaped by doubling, matching FTS5's phrase-escaping rule.
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`, true
}

// ftsAvailable reports whether the FTS5 files_fts index is usable for name
// search: FTS5 must be compiled into the driver AND the external-content
// virtual table must exist. The result never changes for a service's lifetime,
// so it is probed once and cached.
func (s *vaultService) ftsAvailable() bool {
	s.ftsMu.Lock()
	defer s.ftsMu.Unlock()
	if s.ftsChecked {
		return s.ftsOK
	}
	s.ftsChecked = true

	var enabled int
	if err := s.db.Raw("SELECT sqlite_compileoption_used('ENABLE_FTS5')").Scan(&enabled).Error; err != nil || enabled != 1 {
		return false
	}
	var n int64
	if err := s.db.Table("sqlite_master").
		Where("type = 'table' AND name = 'files_fts'").
		Count(&n).Error; err != nil || n == 0 {
		return false
	}
	s.ftsOK = true
	return true
}

// ftsSearch runs an FTS5 MATCH over files_fts joined back to files, bounding
// results by limit and ordering by BM25 relevance then recency. It returns
// whether the MATCH query executed successfully; on failure the caller falls
// back to the LIKE path. Records is populated only on success (and may be empty
// for a legitimate zero-match query).
func (s *vaultService) ftsSearch(q *gorm.DB, param string, limit int, records *[]File) bool {
	ftsQ := q.Session(&gorm.Session{}).
		Joins("JOIN files_fts ON files_fts.rowid = files.id").
		Where("files_fts MATCH ?", param).
		Order("bm25(files_fts), files.created_at DESC").
		Limit(limit)
	if err := ftsQ.Find(records).Error; err != nil {
		return false
	}
	return true
}

// ===========================================================================
// First-class tagging
//
// Tags live durably in the Sia object's sealed FileMetadata under
// Metadata['tags'] as a planar []string. The local `file_tags` join is a CACHE
// of that array (reconciled on every durable tag mutation and on sync-down), so
// a cache rebuild can never clobber remote tags. Every durable mutation goes
// through the re-pin-and-write path: re-read the object -> decode FileMetadata
// -> merge Metadata['tags'] -> re-encode -> UpdateMetadata -> PinObject -> then
// reconcile the local join in one transaction. We NEVER edit only the join
// table.
// ===========================================================================

// normalizeTags lowercases and deduplicates a raw tag list, returning a stable
// (sorted) planar slice. Empty entries are dropped; a nil/empty input yields an
// empty (non-nil) slice so callers can distinguish "clear all" from "unchanged".
func normalizeTags(raw []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.TrimSpace(strings.ToLower(t))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// tagsFromMetadata extracts a planar tag list from a caller-supplied opaque
// metadata map's 'tags' key ([]string or []any of strings). Returns nil when
// absent or uncoercible. Used by Put to promote --tags at upload time.
func tagsFromMetadata(metadata map[string]any) []string {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata["tags"]
	if !ok || raw == nil {
		return nil
	}
	var str []string
	switch v := raw.(type) {
	case []string:
		str = v
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				str = append(str, s)
			}
		}
	default:
		// A single string is tolerated as a one-element tag list for ergonomics.
		if s, ok := raw.(string); ok && s != "" {
			str = []string{s}
		} else {
			return nil
		}
	}
	norm := normalizeTags(str)
	if len(norm) == 0 {
		return nil
	}
	return norm
}

// currentTags returns the file's live tag names (sorted), read from the local
// file_tags join. It is the local cache read used to seed mutations so a stale
// or empty object sidecar never drops tags that are already durably attached.
func (s *vaultService) currentTags(fileID uint) ([]string, error) {
	var names []string
	err := s.db.Table("file_tags").
		Joins("JOIN tags ON tags.id = file_tags.tag_id").
		Where("file_tags.file_id = ?", fileID).
		Order("tags.name ASC").
		Pluck("tags.name", &names).Error
	return names, err
}

// reconcileTagsTx reconciles the local file_tags join for fileID to EXACTLY the
// given (already-normalized, sorted) tags, in the given transaction. It creates
// missing tag rows, bumps used_at on every applied tag, inserts the file_tags
// joins, and prunes tag rows left with zero file_tags links. Callers only use
// this inside a write transaction (durable mutation paths and sync-down), never
// as a standalone writer.
func reconcileTagsTx(tx *gorm.DB, fileID uint, tags []string) error {
	// Existing joins for this file.
	var joins []FileTag
	var tagByName = map[string]uint{}
	var tagRows []Tag
	if err := tx.Model(&Tag{}).Where("id IN (SELECT tag_id FROM file_tags WHERE file_id = ?)", fileID).Find(&tagRows).Error; err != nil {
		return err
	}
	for _, tg := range tagRows {
		tagByName[tg.Name] = tg.ID
	}
	if err := tx.Where("file_id = ?", fileID).Find(&joins).Error; err != nil {
		return err
	}
	have := map[uint]struct{}{}
	for _, j := range joins {
		have[j.TagID] = struct{}{}
	}

	now := time.Now().UTC()

	// Resolve/create tag rows.
	tagIDFor := func(name string) (uint, error) {
		if id, ok := tagByName[name]; ok {
			return id, nil
		}
		// Case-insensitive unique by name (index COLLATE NOCASE): look it up
		// once more to handle a tag created outside this reconcile (e.g. by a
		// concurrent caller) with different case.
		var existing Tag
		if err := tx.Where("name = ? COLLATE NOCASE", name).First(&existing).Error; err == nil {
			tagByName[name] = existing.ID
			return existing.ID, nil
		}
		tg := Tag{Name: name, CreatedAt: now, UsedAt: now}
		if err := tx.Create(&tg).Error; err != nil {
			return 0, err
		}
		tagByName[name] = tg.ID
		return tg.ID, nil
	}

	// Insert missing joins + bump used_at for every tag that should remain.
	for _, name := range tags {
		tid, err := tagIDFor(name)
		if err != nil {
			return err
		}
		if _, ok := have[tid]; !ok {
			if err := tx.Create(&FileTag{FileID: fileID, TagID: tid, CreatedAt: now}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&Tag{}).Where("id = ?", tid).Update("used_at", now).Error; err != nil {
			return err
		}
	}

	// Delete joins no longer wanted, tracking whether anything was removed so
	// the (expensive) dead-tag prune below only runs when it can change state.
	removed := false
	for tid := range have {
		keep := false
		for _, name := range tags {
			if tgID, ok := tagByName[name]; ok && tgID == tid {
				keep = true
				break
			}
		}
		if !keep {
			if err := tx.Where("file_id = ? AND tag_id = ?", fileID, tid).Delete(&FileTag{}).Error; err != nil {
				return err
			}
			removed = true
		}
	}

	// Prune tags left with zero file_tags links (dead tags). Only when we
	// actually removed a join: running this on every reconcile (including the
	// common no-change sync-down) forces a full-table scan of tags per file.
	if removed {
		if err := tx.Exec(`DELETE FROM tags WHERE id NOT IN (SELECT DISTINCT tag_id FROM file_tags)`).Error; err != nil {
			return err
		}
	}
	return nil
}

// reconcileTags is the non-transactional wrapper around reconcileTagsTx, used
// by Put to seed the freshly-uploaded file's tag joins. It runs its own write
// transaction.
func (s *vaultService) reconcileTags(fileID uint, tags []string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		return reconcileTagsTx(tx, fileID, tags)
	})
}

// resolveTagsChange implements the shared durable re-pin-and-write path for the
// three tag mutations. It resolves the file, fetches the object, seeds the
// current Metadata['tags'] set (from the object sidecar, falling back to the
// local join for durability against a stale object), applies `mutate` to compute
// the new normalized set, re-encodes + re-pins the object metadata, and finally
// reconciles the local join in one transaction. `mutate` returns the new tag
// set. Returns the updated File record.
func (s *vaultService) resolveTagsChange(ctx context.Context, vp *VaultPath, mutate func(current []string) ([]string, error)) (*File, error) {
	record, err := s.resolveFile(vp)
	if err != nil {
		return nil, err
	}

	objHash, err := parseHash256(record.ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse object key: %w", err)
	}
	sdk, err := s.ensureSDK()
	if err != nil {
		return nil, fmt.Errorf("failed to create Sia SDK: %w", err)
	}
	obj, err := sdk.Object(ctx, objHash)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch object from indexer: %w", err)
	}

	// Seed current tags: prefer the object's sealed Metadata['tags'], falling
	// back to the local join (the durable local record) if the object sidecar
	// is empty or stale. This preserves tags across a re-pin even when the
	// indexer returns a cached/stale object.
	objTags, _ := tagsFromObjectMetadata(obj)
	current := objTags
	if len(current) == 0 {
		if local, lerr := s.currentTags(record.ID); lerr == nil {
			current = local
		}
	}

	newTags, err := mutate(current)
	if err != nil {
		return nil, err
	}
	newTags = normalizeTags(newTags)

	// Decode/re-seed the full FileMetadata so re-pinning does not drop other
	// fields (status, content digest...) carried by the object.
	var meta FileMetadata
	if raw := obj.Metadata(); len(raw) > 0 {
		if m, merr := ParseFileMetadata(raw); merr == nil {
			meta = m
		}
	}
	if meta.Metadata == nil {
		meta.Metadata = map[string]any{}
	}
	if len(newTags) > 0 {
		meta.Metadata["tags"] = newTags
	} else {
		delete(meta.Metadata, "tags")
	}
	metaJSON, err := meta.JSON()
	if err != nil {
		return nil, fmt.Errorf("failed to encode metadata: %w", err)
	}
	obj.UpdateMetadata(metaJSON)
	if perr := sdk.PinObject(ctx, obj); perr != nil {
		return nil, fmt.Errorf("failed to re-pin object with tags: %w", perr)
	}

	// Reconcile the local join in one transaction (tags here are authoritative).
	if terr := s.db.Transaction(func(tx *gorm.DB) error {
		return reconcileTagsTx(tx, record.ID, newTags)
	}); terr != nil {
		return nil, fmt.Errorf("failed to persist tags: %w", terr)
	}

	// Carry the authoritative resulting tag set back on the returned record so
	// catalog handlers can surface it without a redundant Stat round-trip.
	record.Tags = newTags

	return &record, nil
}

// tagsFromObjectMetadata extracts the planar tag list from an object's sealed
// FileMetadata.Metadata['tags'], returning nil when absent/unparsable.
func tagsFromObjectMetadata(obj siastorage.Object) ([]string, error) {
	raw := obj.Metadata()
	if len(raw) == 0 {
		return nil, nil
	}
	m, err := ParseFileMetadata(raw)
	if err != nil {
		return nil, err
	}
	return tagsFromMetadata(m.Metadata), nil
}

// TagList returns every distinct tag name currently in use, most-recently-used
// first (used_at DESC). Dead tags are pruned on mutation, so a name here maps
// to at least one file.
func (s *vaultService) TagList(_ context.Context) ([]string, error) {
	var names []string
	err := s.db.Model(&Tag{}).
		Order("used_at DESC, name ASC").
		Pluck("name", &names).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list tags: %w", err)
	}
	if names == nil {
		names = []string{}
	}
	return names, nil
}

// AddTags adds one or more tags to the file, durably (re-pin + local reconcile).
// Already-present tags are idempotent (used_at still bumped). Returns the
// updated record.
func (s *vaultService) AddTags(ctx context.Context, vaultPath string, tags []string) (*File, error) {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return nil, err
	}
	vp, err = RequireActiveProfile(vp)
	if err != nil {
		return nil, err
	}
	toAdd := normalizeTags(tags)
	if len(toAdd) == 0 {
		return nil, fmt.Errorf("AddTags: no valid tags supplied")
	}
	return s.resolveTagsChange(ctx, vp, func(current []string) ([]string, error) {
		combined := make([]string, 0, len(current)+len(toAdd))
		combined = append(combined, current...)
		combined = append(combined, toAdd...)
		return normalizeTags(combined), nil
	})
}

// RemoveTags removes one or more tags durably. Tags the file does not have are
// ignored. A tag left with zero file_tags links is pruned. Returns the updated
// record.
func (s *vaultService) RemoveTags(ctx context.Context, vaultPath string, tags []string) (*File, error) {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return nil, err
	}
	vp, err = RequireActiveProfile(vp)
	if err != nil {
		return nil, err
	}
	toRemove := normalizeTags(tags)
	if len(toRemove) == 0 {
		return s.resolveTagsChange(ctx, vp, func(current []string) ([]string, error) {
			return current, nil
		})
	}
	rm := map[string]struct{}{}
	for _, t := range toRemove {
		rm[t] = struct{}{}
	}
	return s.resolveTagsChange(ctx, vp, func(current []string) ([]string, error) {
		kept := make([]string, 0, len(current))
		for _, t := range current {
			if _, drop := rm[t]; !drop {
				kept = append(kept, t)
			}
		}
		return kept, nil
	})
}

// SetTags replaces the file's full tag set durably with exactly the given tags.
// Returns the updated record.
func (s *vaultService) SetTags(ctx context.Context, vaultPath string, tags []string) (*File, error) {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return nil, err
	}
	vp, err = RequireActiveProfile(vp)
	if err != nil {
		return nil, err
	}
	newSet := normalizeTags(tags)
	return s.resolveTagsChange(ctx, vp, func(_ []string) ([]string, error) {
		return newSet, nil
	})
}
