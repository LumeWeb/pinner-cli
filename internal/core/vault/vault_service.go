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
	DownloadSharedObject(ctx context.Context, sharedURL string, opts ...siastorage.DownloadOption) (io.ReadCloser, error)
	Close() error
	AppKey() types.PrivateKey
}

// byteCounter is an io.Writer that tallies the total bytes written to it. It
// is used in ShareAccept to measure the true size of a streamed shared object
// (which the Sia SDK exposes only as an io.ReadCloser) without buffering it in
// memory.
type byteCounter struct {
	n *int64
}

func (c *byteCounter) Write(p []byte) (int, error) {
	*c.n += int64(len(p))
	return len(p), nil
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
		return fmt.Errorf("account is not ready yet: the indexer is still propagating registration on the network; try again in a few seconds")
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
		ID:          fileID,
		VersionID:   versionID,
		Seq:         versionSeq,
		Name:        vp.Name,
		Directory:   vp.Directory,
		MediaType:   mediaType,
		Size:        size,
		CreatedAt:   now,
		Metadata:    metadata,
		Status:      FileStatusOK,
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
	var record *File
	nowTs := time.Now().UTC()
	// Persist the user-supplied metadata map on the local File row so it
	// survives cache rebuilds and is returned by Stat. The Sia object already
	// carries it in its encrypted metadata; this is the local copy.
	userMetaJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}
	// Project the well-known write-context keys onto the normalized search
	// columns (source/host/agent) from the metadata map being stamped. The
	// object's sealed metadata remains the durable source; these columns are
	// the local searchable cache, reconciled on sync-down like tags.
	recSource, recHost, recAgent := WriteContextColumns(metadata)
	rec := File{
		UUID:          fileID,
		Name:          vp.Name,
		DirectoryID:   dirID,
		Source:        recSource,
		Host:          recHost,
		Agent:         recAgent,
		// A freshly-minted path inserts as is_current=true so the partial
		// unique index idx_files_live_name_dir (which only constrains
		// is_current=1) still fires when a concurrent writer claims the same
		// brand-new path, letting the retry/adopt loop converge instead of
		// leaving an orphaned invisible version. An overwrite path is NOT
		// current at insert: setting is_current would collide with the
		// existing winner's own live row, so promoteCurrent below does that
		// final demote+promote. (adoptPreflight resets this to false on the
		// adopt path so the adopted row never races the winner's live row.)
		IsCurrent: mintedFresh, // promoteCurrent demotes the prior current + promotes this row atomically
		ObjectKey:     objectKey.String(),
		Size:          size,
		MediaType:     mediaType,
		ContentDigest: contentDigest,
		Metadata:      datatypes.JSON(userMetaJSON),
		Status:        FileStatusOK,
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
	// remote object and the committed row share the adopted identity; a re-pin
	// failure returns before anything is committed, so Sync can never mint a
	// duplicate from stale-metadata (the no-divergence invariant, now enforced
	// without holding the connection).
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
			// Versioning: EVERY Put inserts a fresh version row and promotes it
			// to the live current winner for its (name, dir). The prior current
			// row is demoted by promoteCurrent but RETAINS its ObjectKey, so its
			// content survives as an older version. We never mutate a version
			// row in place: that is what previously destroyed history.
			//
			// version_id comes from the encrypted object metadata (set up front,
			// so the local row matches what other devices sync down). seq is
			// re-derived inside the single-connection write transaction so two
			// concurrent Puts to the same logical file can't race to the same
			// seq (take the max of the up-front read and the committed max, in
			// case a cross-process writer landed between).
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

			// Insert the new version row. On a concurrent-create conflict
			// (partial unique index on (name, dir, is_current)) the caller's
			// retry loop re-runs adoptPreflight OUTSIDE the transaction, adopts
			// the winner's UUID, and re-inserts as a new version of that group.
			if err := tx.Create(&rec).Error; err != nil {
				return err
			}
			// Reconcile the freshly-Put file's tag joins inside the SAME write
			// transaction as the row insert, so a tag-cache write failure
			// cannot leave the object pinned + row committed with the caller
			// seeing a failed Put (which would prompt a duplicate-version
			// retry). Tags are authoritative in the sealed object metadata; the
			// local file_tags join is a cache seeded here so vault_tag_ls /
			// search are correct immediately after an upload with --tags.
			if len(putTags) > 0 {
				if rerr := reconcileTagsTx(tx, rec.ID, putTags); rerr != nil {
					return rerr
				}
			}
			return promoteCurrent(tx, vp.Name, dirID, rec.ID)
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
	// Only a matching digest proves the object is present and correct; a
	// divergence (present-but-corrupt object) must NOT clear lost state, or a
	// recovering-but-still-broken file would drop out of vault_status --lost.
	if res.DigestMatch {
		s.clearLostStatus(ctx, vaultPath)
	}
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
	// Only a matching digest proves the bytes are present and correct; a
	// divergence must NOT clear lost state, or a recovering-but-still-broken
	// file would drop out of vault_status --lost.
	if res.DigestMatch {
		s.clearLostStatus(ctx, vaultPath)
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

// ShareAccept implements the A2A copy-once pin-to-indexer primitive. The SDK's
// share URL is a time-limited, read-only bearer of a single object's content;
// local SQLite cannot gate a permissionless Sia blob, so accepting a share
// means resolving it and pinning a SELF-CONTAINED copy into this profile's
// vault (never a reference). A write-only audit row is appended to the share
// ledger recording the accept.
func (s *vaultService) ShareAccept(ctx context.Context, vaultPath, shareURL, targetPrincipal string) (*File, error) {
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

	sdk, err := s.ensureSDK()
	if err != nil {
		return nil, fmt.Errorf("failed to create Sia SDK: %w", err)
	}

	// Resolve the share URL. The URL embeds the decryption key, so the
	// accepting SDK needs no extra secrets.
	rc, err := sdk.DownloadSharedObject(ctx, shareURL)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve shared object: %w", err)
	}
	defer rc.Close()

	// Refuse to silently overwrite an existing live file at the destination.
	// Accepting into an existing path would demote its current content + full
	// version history with no confirmation; the caller must remove the target
	// first. A missing destination directory is fine (Put creates it).
	if dirID, derr := s.getDirectoryID(vp.Directory); derr == nil {
		if _, ferr := s.findCurrentFile(vp.Name, dirID); ferr == nil {
			return nil, fmt.Errorf("destination already exists (refusing to overwrite): %s", vp.FullPath())
		}
	}

	// Stream the shared content straight into Put via a pipe rather than
	// materializing the whole object in RAM (mirrors VersionRestore), so a
	// large share cannot OOM the process. The shared object's true size is
	// unknown until it streams (the Sia SDK exposes DownloadSharedObject only
	// as an io.ReadCloser), so we count bytes as they pass through the pipe
	// and reconcile the stored Size on the row + object after the upload. A
	// failed download is propagated via CloseWithError so Put refuses the
	// object instead of pinning partial/empty content.

	// Pin a self-contained copy into this profile: a NEW object + NEW file
	// row, fully owned by the accepting profile (never shared by reference).
	// The shared object's true size is unknown until it streams (the Sia SDK
	// surfaces DownloadSharedObject only as an io.ReadCloser), so bytes are
	// counted as they pass through the pipe and the resulting Size is written
	// back onto the row after the upload; a failed download is propagated via
	// CloseWithError so Put refuses the object instead of pinning partial or
	// empty content.
	pr, pw := io.Pipe()
	var n int64
	counter := &byteCounter{n: &n}
	go func() {
		// Copy the shared stream into the pipe while tallying the real byte
		// count (TeeReader feeds the counter as bytes pass through). A failed
		// download is propagated via CloseWithError so Put refuses the object
		// rather than pinning partial or empty content.
		var werr error
		_, werr = io.Copy(pw, io.TeeReader(rc, counter))
		pw.CloseWithError(werr)
	}()
	f, err := s.Put(ctx, pr, 0, vp.FullPath(), nil)
	if err != nil {
		// Put bailed before draining the pipe (e.g. upload failure): the
		// writer goroutine would block forever in pw.Write holding the Sia
		// download open. Unblock it by failing the reader side.
		pr.CloseWithError(err)
		return nil, fmt.Errorf("failed to pin shared content copy: %w", err)
	}

	// Reconcile the true size (counted during the stream) onto BOTH the local
	// row and the sealed object metadata, so stat/ls and cross-device sync all
	// report the actual byte count rather than the 0 placeholder Put sealed at
	// upload time (the shared object's size is unknown until it streams).
	if n > 0 {
		if f.Size != n {
			if uerr := s.db.Model(&File{}).Where("id = ?", f.ID).Update("size", n).Error; uerr == nil {
				f.Size = n
			}
		}
		if oerr := s.resealObjectSize(ctx, sdk, f.ObjectKey, n); oerr != nil {
			return nil, fmt.Errorf("failed to update shared copy size in object metadata: %w", oerr)
		}
	}

	// Append a write-only audit row to the share ledger.
	if err := s.db.Create(&ShareLedger{
		SharedVaultPath: vp.FullPath(),
		ObjectKey:       f.ObjectKey,
		Expiry:          nil,
		TargetPrincipal: targetPrincipal,
		CreatedAt:       time.Now().UTC(),
	}).Error; err != nil {
		return nil, fmt.Errorf("failed to record share accept in ledger: %w", err)
	}

	return f, nil
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

// VersionRestore re-uploads a specific version's content as a NEW current
// version of the file at vaultPath. The historical version's bytes are copied
// via a fresh Put, which mints a new version row (new ObjectKey + version_id)
// and promotes it to current; all prior version rows are preserved. This is a
// restore-as-new-version (not an in-place pointer flip), matching s3d's
// CopyObject semantics.
func (s *vaultService) VersionRestore(ctx context.Context, vaultPath string, versionID string) (*File, error) {
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return nil, err
	}
	// Resolve the target version up front (validates it exists and gives its
	// declared Size, which Put streams against). An empty/broken version
	// errors here before any write occurs.
	_, row, err := s.resolveVersionGroup(vp, versionID)
	if err != nil {
		return nil, err
	}

	// Preserve the live file's tags onto the restored winner row. A restore
	// mints a NEW current version; without this the new row would come up with
	// no tags, silently dropping the label set.
	var meta map[string]any
	if rec, err := s.resolveFile(vp); err == nil {
		if tags, err := s.currentTags(rec.ID); err == nil && len(tags) > 0 {
			meta = map[string]any{"tags": tags}
		}
	}

	// Stream the historical version's bytes into Put via a pipe, rather than
	// buffering the whole object in RAM. Put consumes the reader once
	// (io.TeeReader -> sdk.Upload) and mints a new version row that reuses
	// the same logical file's UUID group (findCurrentFile resolves the
	// current winner by path). A failed/truncated historical download is
	// propagated via CloseWithError so Put's io.Copy surfaces it as a read
	// error and the restore aborts instead of committing a partial/empty
	// version as the new current winner.
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(s.VersionDownload(ctx, vp.Raw, versionID, pw))
	}()
	return s.Put(ctx, pr, row.Size, vp.Raw, meta)
}

// resealObjectSize re-seals a just-accepted object's FileMetadata with the
// real byte count and re-pins it in place. ShareAccept streams the shared
// content into Put with a size placeholder (the Sia SDK exposes the shared
// object's size only once it streams), so after the stream completes this
// corrects the sealed Size so cross-device sync-down reports the true size
// instead of the placeholder.
func (s *vaultService) resealObjectSize(ctx context.Context, sdk sdkClient, objectKeyHex string, size int64) error {
	objHash, err := parseHash256(objectKeyHex)
	if err != nil {
		return fmt.Errorf("failed to parse object key: %w", err)
	}
	obj, err := sdk.Object(ctx, objHash)
	if err != nil {
		return fmt.Errorf("failed to fetch object from indexer: %w", err)
	}
	var meta FileMetadata
	if raw := obj.Metadata(); len(raw) > 0 {
		if m, merr := ParseFileMetadata(raw); merr == nil {
			meta = m
		}
	}
	meta.Size = size
	metaJSON, err := meta.JSON()
	if err != nil {
		return fmt.Errorf("failed to encode metadata: %w", err)
	}
	obj.UpdateMetadata(metaJSON)
	if perr := sdk.PinObject(ctx, obj); perr != nil {
		return fmt.Errorf("failed to re-pin object with corrected size: %w", perr)
	}
	return nil
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

