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
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"go.sia.tech/siastorage"
)

// DefaultDiskUsageTimeout bounds how long Put waits for staging disk space to
// free before returning ErrSlowDown. Mirrors s3d's diskUsageTimeout.
const DefaultDiskUsageTimeout = 2 * time.Minute

// DefaultUploadWastePct is the maximum tolerated slab waste before a group of
// staged files is uploaded in one packed upload. Mirrors s3d's default: groups
// whose padding would exceed this are held back to batch with future files.
const DefaultUploadWastePct = 0.1

// DefaultMaxGroupSize caps the total bytes in a single packed upload group.
const DefaultMaxGroupSize = 1 << 30 // 1 GiB

// optimalSlabSize is the packing heuristic slab (data portion) used to decide
// how to group staged files and when a group has enough bytes to upload. The
// SDK's actual shard/slab geometry is internal to UploadPacked; 40 MiB matches
// Sia's default data-shard geometry (10 data shards x 4 MiB sector).
const optimalSlabSize = 40 << 20

// ErrSlowDown is returned by Put when the vault staging directory reached its
// configured disk limit and no space freed within diskUsageTimeout. Callers
// should surface it as "storage is full, retry later".
var ErrSlowDown = errors.New("vault staged storage full: retry later")

// flushLockMu guards the flushLocks map. Two different sync.Mutex values must
// not be handed out for the same profile concurrently.
var flushLockMu sync.Mutex

// flushLocks is a process-wide set of per-profile flush locks. A single
// process can hold several VaultService instances for the same profile (the MCP
// server's background upload loop owns one continuously; a vault_flush invoke
// or the CLI opens a fresh one per operation). Each instance used to carry its
// own flushMu, so two instances could snapshot the same staged rows and run two
// UploadPacked uploads that race (one finalizes and deletes a staged buffer
// while the other mid-reads it), leaving files stuck pending. Keying the lock
// by profile makes all flush work for a profile mutually exclusive regardless
// of which service instance drives it.
var flushLocks = map[string]*sync.Mutex{}

// profileFlushLock returns the process-wide flush lock for the named profile.
func profileFlushLock(profile string) *sync.Mutex {
	flushLockMu.Lock()
	defer flushLockMu.Unlock()
	l, ok := flushLocks[profile]
	if !ok {
		l = &sync.Mutex{}
		flushLocks[profile] = l
	}
	return l
}

// stagedObject is a single not-yet-durable File row (its staged buffer).
type stagedObject struct {
	rec *File
}

// uploadGroup bundles staged objects that will be packed into shared slabs and
// uploaded together. Mirrors s3d's uploadGroup.
type uploadGroup struct {
	slabSize       int64
	maxGroupSize   int64
	uploadWastePct float64
	objects        []stagedObject
	totalSize      int64
}

func (g *uploadGroup) remainingSpace() int64 {
	if g.totalSize == 0 {
		return g.slabSize
	}
	remainder := g.totalSize % g.slabSize
	if remainder == 0 {
		return 0
	}
	return g.slabSize - remainder
}

func (g *uploadGroup) wastePct() float64 {
	if g.totalSize == 0 {
		return 1
	}
	remainder := g.totalSize % g.slabSize
	if remainder == 0 {
		return 0
	}
	waste := g.slabSize - remainder
	return float64(waste) / float64(g.totalSize+waste)
}

func (g *uploadGroup) tryAdd(obj stagedObject) bool {
	newTotal := g.totalSize + obj.rec.Size
	maxSize := newTotal > g.maxGroupSize
	if maxSize || g.wastePct() < g.uploadWastePct {
		var newWaste float64
		if remainder := newTotal % g.slabSize; remainder != 0 {
			waste := g.slabSize - remainder
			newWaste = float64(waste) / float64(newTotal+waste)
		}
		reducesWaste := newWaste < g.wastePct()
		fitsLastSlab := obj.rec.Size <= g.remainingSpace()
		if maxSize && !fitsLastSlab {
			return false
		} else if !fitsLastSlab && !reducesWaste {
			return false
		}
	}
	g.objects = append(g.objects, obj)
	g.totalSize += obj.rec.Size
	return true
}

// prepareUploads places staged objects into packing groups using first-fit
// placement (mirrors s3d's prepareUploads). Every staged object is included so
// a single object is never stranded silently; the grouping heuristic only
// decides how files are batched, not whether they get uploaded.
func prepareUploads(objects []stagedObject, maxGroupSize int64, wastePct float64) []uploadGroup {
	var groups []uploadGroup
	for _, obj := range objects {
		var added bool
		for i := range groups {
			if added = groups[i].tryAdd(obj); added {
				break
			}
		}
		if !added {
			g := uploadGroup{slabSize: optimalSlabSize, maxGroupSize: maxGroupSize, uploadWastePct: wastePct}
			g.objects = []stagedObject{obj}
			g.totalSize = obj.rec.Size
			groups = append(groups, g)
		}
	}
	return groups
}

// Put accepts a file into the vault at the given vault path.
//
// With a staging directory configured (production), Put buffers the bytes to
// local disk, records the File row in status "pending", and returns immediately
// WITHOUT uploading to Sia — durability happens asynchronously via Flush (the
// MCP background engine, `vault flush`, or a share forced-flush). This removes
// the historical multi-second-to-minute in-request blocking of a full host-set
// upload+pin, which is what made even a 7-byte write take ~100s and time out on
// slow/missing hosts.
//
// Without a staging directory (e.g. unit tests injecting a fake SDK directly),
// Put falls back to a synchronous durable write via putImmediate.
func (s *vaultService) Put(ctx context.Context, r io.Reader, size int64, vaultPath string, metadata map[string]any) (*File, error) {
	if s.uploadsDir == "" {
		return s.putImmediate(ctx, r, size, vaultPath, metadata)
	}
	return s.stage(ctx, r, size, vaultPath, metadata)
}

// stage buffers a vault write to local disk and records it as a "pending" File
// row, returning immediately. The staged plaintext + SHA-256 are written in a
// single pass; the background flush (Flush) later packs several such files into
// shared slabs and pins them, at which point the staged buffer is deleted.
func (s *vaultService) stage(ctx context.Context, r io.Reader, size int64, vaultPath string, metadata map[string]any) (*File, error) {
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

	if err := s.addDiskUsage(ctx, size); err != nil {
		return nil, err
	}
	stagedPath := ""
	releaseIO := true
	defer func() {
		if releaseIO {
			s.releaseDiskUsage(size)
			if stagedPath != "" {
				_ = os.Remove(stagedPath)
			}
		}
	}()

	if err := os.MkdirAll(s.uploadsDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create vault staging dir: %w", err)
	}
	f, err := os.CreateTemp(s.uploadsDir, "staged-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create staged buffer: %w", err)
	}
	stagedPath = f.Name()

	hasher := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hasher), r)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to stage bytes: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to sync staged buffer: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("failed to close staged buffer: %w", err)
	}
	if size >= 0 && n != size {
		return nil, fmt.Errorf("staged byte count mismatch: expected %d, got %d", size, n)
	}
	contentDigest := hex.EncodeToString(hasher.Sum(nil))

	rec, err := s.buildPendingRecord(vp, metadata, n, contentDigest)
	if err != nil {
		return nil, err
	}
	rec.StagedPath = stagedPath

	committed, err := s.commitFileRecord(ctx, vp, rec.DirectoryID, rec, recTags(metadata), true, s.stagedAdopt(vp, rec))
	if err != nil {
		return nil, err
	}
	stagedPath = ""
	releaseIO = false
	return committed, nil
}

// buildPendingRecord assembles the "pending" File row for a staged write,
// mirroring the rec construction in putImmediate but with Status pending and no
// ObjectKey yet.
func (s *vaultService) buildPendingRecord(vp *VaultPath, metadata map[string]any, size int64, contentDigest string) (*File, error) {
	dirID, err := s.getOrCreateDirectory(vp.Directory)
	if err != nil {
		return nil, err
	}

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

	versionID := newVersionID()
	var curSeq uint
	s.db.Model(&File{}).Where("uuid = ?", fileID).Select("COALESCE(MAX(seq),0)").Scan(&curSeq)
	versionSeq := curSeq + 1

	putTags := tagsFromMetadata(metadata)
	mergedMeta := metadata
	if len(putTags) > 0 {
		mergedMeta = cloneMeta(metadata)
		mergedMeta["tags"] = putTags
	}
	userMetaJSON, err := json.Marshal(mergedMeta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}
	recSource, recHost, recAgent := WriteContextColumns(metadata)
	nowTs := time.Now().UTC()

	return &File{
		UUID:          fileID,
		VersionID:     versionID,
		Seq:           versionSeq,
		Name:          vp.Name,
		DirectoryID:   dirID,
		IsCurrent:     mintedFresh,
		Size:          size,
		MediaType:     mime.TypeByExtension(filepath.Ext(vp.Name)),
		ContentDigest: contentDigest,
		Metadata:      datatypes.JSON(userMetaJSON),
		Source:        recSource,
		Host:          recHost,
		Agent:         recAgent,
		Status:        FileStatusPending,
		CreatedAt:     nowTs,
		UpdatedAt:     nowTs,
	}, nil
}

func recTags(metadata map[string]any) []string { return tagsFromMetadata(metadata) }

func cloneMeta(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// commitFileRecord persists + promotes a File row inside the bounded
// create-conflict retry loop. It never runs network I/O inside the single-
// connection SQLite write transaction. adopt, when non-nil and the row was
// freshly minted, runs BEFORE each transaction attempt to resolve a concurrent
// same-path winner (adopting its UUID).
func (s *vaultService) commitFileRecord(ctx context.Context, vp *VaultPath, dirID *uint, rec *File, putTags []string, mintedFresh bool, adopt func() (bool, error)) (*File, error) {
	const maxAdoptRetries = 4
	var lastErr error
	for attempt := 0; attempt < maxAdoptRetries; attempt++ {
		if mintedFresh && adopt != nil {
			if _, aerr := adopt(); aerr != nil {
				return nil, aerr
			}
		}
		err := s.db.Transaction(func(tx *gorm.DB) error {
			var maxSeq uint
			if err := tx.Model(&File{}).
				Where("uuid = ?", rec.UUID).
				Select("COALESCE(MAX(seq), 0)").
				Scan(&maxSeq).Error; err != nil {
				return fmt.Errorf("failed to compute next version seq: %w", err)
			}
			if rec.Seq > maxSeq {
				maxSeq = rec.Seq
			}
			rec.Seq = maxSeq + 1
			if err := tx.Create(rec).Error; err != nil {
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
			return rec, nil
		}
		lastErr = err
		if !isLiveNameConflict(err) {
			return nil, fmt.Errorf("failed to store file record: %w", err)
		}
	}
	return nil, fmt.Errorf("failed to store file record after %d attempts: %w", maxAdoptRetries, lastErr)
}

// stagedAdopt re-resolves a concurrent same-path winner for a staged write and
// adopts its identity (no object to re-pin, unlike the durable path).
func (s *vaultService) stagedAdopt(vp *VaultPath, rec *File) func() (bool, error) {
	return func() (bool, error) {
		if rec.DirectoryID == nil {
			return false, nil
		}
		if w, err := s.findCurrentFile(vp.Name, rec.DirectoryID); err == nil {
			rec.UUID = w.UUID
			return true, nil
		}
		return false, nil
	}
}

// Flush drains every staged ("pending") File to durable storage: it packs the
// staged buffers into shared slabs via the SDK's UploadPacked, pins each object,
// transitions rows to "ok", and deletes the staged buffers. Returns the number
// of files flushed. Used by the MCP background engine, the `vault flush` CLI
// command, and as the forced-flush primitive for share links on pending objects.
func (s *vaultService) Flush(ctx context.Context) (int, error) {
	lock := profileFlushLock(s.profile)
	lock.Lock()
	defer lock.Unlock()
	rows, err := s.stagedRows(ctx)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	// The worker has (actually) started on this batch: mark each still-staged
	// row flushing and count a flush attempt, so a polling agent can tell a
	// flush in progress (flushing, rising flush_attempts, no error) from one
	// that never started (staged, zero attempts). Marked before packing.
	for i := range rows {
		s.markFlushing(rows[i].rec)
	}
	groups := prepareUploads(rows, DefaultMaxGroupSize, DefaultUploadWastePct)
	return s.flushGroups(ctx, groups)
}

// FlushPath flushes a single vault path to durable storage (used by CLI
// `--flush` and share's forced flush). It is a no-op if the file is already ok.
func (s *vaultService) FlushPath(ctx context.Context, vaultPath string) error {
	lock := profileFlushLock(s.profile)
	lock.Lock()
	defer lock.Unlock()
	vp, err := ParseVaultPath(vaultPath)
	if err != nil {
		return err
	}
	vp, err = RequireActiveProfile(vp)
	if err != nil {
		return err
	}
	rec, err := s.resolveFile(vp)
	if err != nil {
		return err
	}
	if rec.StagedPath == "" {
		return nil // already durable
	}
	s.markFlushing(&rec)
	g := uploadGroup{slabSize: optimalSlabSize, maxGroupSize: DefaultMaxGroupSize, uploadWastePct: 0}
	g.objects = []stagedObject{{rec: &rec}}
	g.totalSize = rec.Size
	_, err = s.flushGroups(ctx, []uploadGroup{g})
	return err
}

// stagedRows returns every live File row (pending or uploaded) that still has a
// staged buffer awaiting upload.
func (s *vaultService) stagedRows(ctx context.Context) ([]stagedObject, error) {
	var rows []File
	if err := s.db.Where("staged_path <> '' AND deleted_at IS NULL").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to query staged files: %w", err)
	}
	out := make([]stagedObject, 0, len(rows))
	for i := range rows {
		out = append(out, stagedObject{rec: &rows[i]})
	}
	return out, nil
}

// flushGroups uploads + pins each group, marking rows durable.
func (s *vaultService) flushGroups(ctx context.Context, groups []uploadGroup) (int, error) {
	sdk, err := s.ensureSDK()
	if err != nil {
		return 0, fmt.Errorf("failed to create Sia SDK: %w", err)
	}
	flushed := 0
	var firstErr error
	for i := range groups {
		n, ferr := s.uploadPackedGroup(ctx, sdk, &groups[i])
		flushed += n
		if ferr != nil && firstErr == nil {
			firstErr = ferr
		}
	}
	return flushed, firstErr
}

// uploadPackedGroup packs one group of staged files and pins each produced
// object, finalizing the rows.
func (s *vaultService) uploadPackedGroup(ctx context.Context, sdk sdkClient, g *uploadGroup) (int, error) {
	packed, err := sdk.UploadPacked()
	if err != nil {
		return 0, fmt.Errorf("failed to create packed upload: %w", err)
	}
	defer packed.Close()

	type member struct {
		rec *File
		obj siastorage.Object
	}
	members := make([]member, 0, len(g.objects))
	for _, o := range g.objects {
		f, err := os.Open(o.rec.StagedPath)
		if err != nil {
			s.recordFlushFailure(o.rec, err)
			return 0, fmt.Errorf("failed to open staged file %s: %w", o.rec.StagedPath, err)
		}
		if _, err := packed.Add(ctx, f); err != nil {
			_ = f.Close()
			s.recordFlushFailure(o.rec, err)
			return 0, fmt.Errorf("failed to add staged file to packed upload: %w", err)
		}
		_ = f.Close()
		members = append(members, member{rec: o.rec})
	}

	results, err := packed.Finalize(ctx)
	if err != nil {
		// The whole packed upload failed; every member is still staged, so flag
		// them all so the pending state is not silently stuck.
		for i := range members {
			s.recordFlushFailure(members[i].rec, err)
		}
		return 0, fmt.Errorf("packed upload failed: %w", err)
	}
	if len(results) != len(members) {
		fnErr := fmt.Errorf("packed upload returned %d objects, expected %d", len(results), len(members))
		for i := range members {
			s.recordFlushFailure(members[i].rec, fnErr)
		}
		return 0, fnErr
	}

	done := 0
	for i := range members {
		m := &members[i]
		meta := FileMetadata{
			ID:            m.rec.UUID,
			VersionID:     m.rec.VersionID,
			Seq:           m.rec.Seq,
			Name:          m.rec.Name,
			Directory:     s.directoryPathOf(m.rec),
			MediaType:     m.rec.MediaType,
			Size:          m.rec.Size,
			CreatedAt:     m.rec.CreatedAt.Format(time.RFC3339),
			Metadata:      metadataMapOf(m.rec),
			Status:        FileStatusOK,
			ContentDigest: m.rec.ContentDigest,
		}
		metaJSON, err := meta.JSON()
		if err != nil {
			return done, fmt.Errorf("failed to encode object metadata: %w", err)
		}
		m.obj = results[i]
		m.obj.UpdateMetadata(metaJSON)
		if err := sdk.PinObject(ctx, m.obj); err != nil {
			s.recordFlushFailure(m.rec, err)
			return done, fmt.Errorf("failed to pin object for %s: %w", m.rec.Name, err)
		}
		if err := s.finalizeDurable(m.rec, m.obj.ID().String()); err != nil {
			s.recordFlushFailure(m.rec, err)
			return done, err
		}
		done++
	}
	return done, nil
}

func (s *vaultService) directoryPathOf(rec *File) string {
	if rec.DirectoryID == nil {
		return "/"
	}
	var d Directory
	if err := s.db.First(&d, *rec.DirectoryID).Error; err == nil {
		return d.Path
	}
	return "/"
}

func metadataMapOf(rec *File) map[string]any {
	if len(rec.Metadata) == 0 {
		return nil
	}
	m := map[string]any{}
	if err := json.Unmarshal(rec.Metadata, &m); err != nil {
		return nil
	}
	return m
}

// finalizeDurable promotes a row from staged to durable "ok": it sets ObjectKey +
// markFlushing transitions a staged row into the "flushing" lifecycle state and
// counts one flush attempt at the moment the worker actually starts on it (see
// requirement: flush_attempts increments when the worker starts). flush_attempts
// is therefore a monotonically rising count of worker starts — not of failures —
// so the typical signal is: staged + 0 attempts = never started; flushing +
// rising attempts + error = failing. Best-effort (a DB error must not block the
// flush).
func (s *vaultService) markFlushing(rec *File) {
	if rec == nil {
		return
	}
	// Record when THIS attempt began so a polling swarm can measure how long a
	// file has been stuck in "flushing" (elapsed = now - flush_started_at) and
	// fail a hung pin, even though flush_attempts only moved once.
	_ = s.db.Model(&File{}).Where("id = ?", rec.ID).Updates(map[string]any{
		"status":           FileStatusFlushing,
		"flush_attempts":   gorm.Expr("flush_attempts + 1"),
		"flush_started_at": time.Now().UTC().Format(time.RFC3339),
	}).Error
}

// recordFlushFailure surface-marks a file whose durability flush failed so a
// stuck row is visible: it sets the lifecycle state to "failed" and persists the
// most recent error (flush_attempts is NOT re-incremented — markFlushing already
// counted the worker start). A failed row keeps its staged buffer so a later
// vault_flush/work er retries it. Best-effort: a DB error here (e.g. SQLite
// contention) must not mask the original flush failure, so it is swallowed.
func (s *vaultService) recordFlushFailure(rec *File, err error) {
	if rec == nil || err == nil {
		return
	}
	msg := err.Error()
	if len(msg) > 512 {
		msg = msg[:512]
	}
	id := rec.ID
	_ = s.db.Model(&File{}).Where("id = ?", id).Updates(map[string]any{
		"status":      FileStatusFailed,
		"flush_error": msg,
	}).Error
}

// status ok + clears StagedPath, and (best-effort) deletes the staged buffer +
// releases its disk reservation.
func (s *vaultService) finalizeDurable(rec *File, objectKey string) error {
	staged := rec.StagedPath
	var digest string
	if len(rec.ContentDigest) == 0 {
		digest = ""
	} else {
		digest = rec.ContentDigest
	}
	if err := s.db.Model(rec).Updates(map[string]any{
		"object_key":       objectKey,
		"status":           FileStatusOK,
		"staged_path":      "",
		"content_digest":   digest,
		"flush_attempts":   0,
		"flush_error":      "",
		"flush_started_at": "",
	}).Error; err != nil {
		return fmt.Errorf("failed to mark file durable: %w", err)
	}
	if staged != "" {
		s.releaseDiskUsage(rec.Size)
		_ = os.Remove(staged)
	}
	return nil
}

// --- Disk backpressure (mirrors s3d's addDiskUsage) ---

func (s *vaultService) addDiskUsage(ctx context.Context, size int64) error {
	if s.diskUsageLimit <= 0 || size <= 0 {
		return nil
	}
	for {
		s.diskUsageMu.Lock()
		if s.diskUsage+size <= s.diskUsageLimit {
			s.diskUsage += size
			s.diskUsageMu.Unlock()
			return nil
		}
		// The wake channel is created lazily the first time a Put backs up (so
		// a service that never hits the limit allocates nothing); it must be
		// non-nil before we select on it, otherwise the waiter blocks forever
		// and the "space freed" wake never fires.
		if s.diskWake == nil {
			s.diskWake = make(chan struct{})
		}
		wake := s.diskWake
		s.diskUsageMu.Unlock()

		timer := time.NewTimer(s.diskUsageTimeout)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-wake:
			timer.Stop()
		case <-timer.C:
			timer.Stop()
			return ErrSlowDown
		}
	}
}

func (s *vaultService) releaseDiskUsage(size int64) {
	if size <= 0 {
		return
	}
	s.diskUsageMu.Lock()
	if s.diskUsage >= size {
		s.diskUsage -= size
	}
	if s.diskWake != nil {
		close(s.diskWake)
		s.diskWake = nil
	}
	s.diskUsageMu.Unlock()
}

// copyStaged streams a staged buffer's contents to w (used by Get/Cat for
// not-yet-durable objects).
func copyStaged(w io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open staged buffer: %w", err)
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}
