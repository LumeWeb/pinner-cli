package upload

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ipfs/boxo/blockstore"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs/importer/helpers"
	"github.com/ipfs/go-cid"
	format "github.com/ipfs/go-ipld-format"
	"github.com/samber/lo"
	"go.lumeweb.com/pinner-cli/pkg/upload/internal/carv1"
	"go.lumeweb.com/pinner-cli/pkg/upload/internal/encoding"
	uploadio "go.lumeweb.com/pinner-cli/pkg/upload/internal/io"
)

const ROOT = "ROOT"

// CARBuilder performs two-pass CAR generation:
// Pass 1: Walk filesystem and build summary with metadata
// Pass 2: Write CARv1 using the summary, regenerating blocks on demand
type CARBuilder struct {
	bs         blockstore.Blockstore
	dagService format.DAGService
	generator  UnixFSNodeGenerator
	filesystem fs.FS
	wrapInDir  bool
	summary    *TreeSummary
	chunkSize  int64
}

// TreeSummary contains metadata collected during pass 1.
type TreeSummary struct {
	RootCID     cid.Cid
	TotalSize   uint64
	BlockOrder  []cid.Cid
	BlockSizes  []uint64
	TreeEntries map[string]*TreeEntry
	CIDToEntry  map[cid.Cid]*TreeEntry // CID -> Entry map for O(1) lookups
	CARSize     uint64
}

// TreeEntry represents an entry in the filesystem tree.
type TreeEntry struct {
	Path      string
	Name      string
	IsDir     bool
	CID       cid.Cid
	Children  []string
	ChunkSize int64
}

// NewCARBuilder creates a new CARBuilder with the specified blockstore, DAG service, and UnixFS node generator.
// If bs or dagService is nil, a new LRU blockstore with the default memory limit is created.
func NewCARBuilder(bs blockstore.Blockstore, dagService format.DAGService, generator UnixFSNodeGenerator) *CARBuilder {
	if bs == nil || dagService == nil {
		bs, dagService = NewDAGServiceWithMemoryLimit(DefaultMemoryLimit)
	}

	return &CARBuilder{
		bs:         bs,
		dagService: dagService,
		generator:  generator,
		chunkSize:  1024 * 1024,
	}
}

// BuildSummary performs pass 1: walks the filesystem and builds the UnixFS DAG,
// collecting metadata without retaining blocks.
func (b *CARBuilder) BuildSummary(ctx context.Context, filesystem fs.FS, wrapInDir bool) (*TreeSummary, error) {
	b.filesystem = filesystem
	b.wrapInDir = wrapInDir

	summary := &TreeSummary{
		TreeEntries: make(map[string]*TreeEntry),
		CIDToEntry:  make(map[cid.Cid]*TreeEntry),
	}

	summary.TreeEntries[ROOT] = &TreeEntry{
		Name:  ROOT,
		IsDir: true,
		Path:  ROOT,
	}

	if err := fs.WalkDir(filesystem, ".", func(path string, d fs.DirEntry, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return err
		}

		if path == "" || path == ROOT {
			return nil
		}

		entry := &TreeEntry{
			Name:  d.Name(),
			IsDir: d.IsDir(),
		}

		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			entry.Path = path[:idx]
		} else {
			entry.Path = ""
		}

		if !d.IsDir() {
			file, err := filesystem.Open(path)
			if err != nil {
				return err
			}

			rootCID, blocks, blockSizes, err := b.createUnixFSBlocks(ctx, file)
			if closeErr := file.Close(); closeErr != nil {
				return closeErr
			}
			if err != nil {
				return err
			}

			entry.CID = rootCID
			entry.ChunkSize = b.chunkSize

			for i, blockCID := range blocks {
				summary.BlockOrder = append(summary.BlockOrder, blockCID)
				summary.BlockSizes = append(summary.BlockSizes, blockSizes[i])
				summary.TotalSize += uint64(blockCID.ByteLen())
			}
		}

		summary.TreeEntries[path] = entry
		if entry.CID != cid.Undef {
			summary.CIDToEntry[entry.CID] = entry
		}

		// Build parent-child relationship during walk
		if entry.Path != "" {
			parent := summary.TreeEntries[entry.Path]
			if parent != nil {
				parent.Children = append(parent.Children, path)
			}
		} else if !entry.IsDir {
			// Root-level file, add to ROOT
			rootChildren := summary.TreeEntries[ROOT].Children
			summary.TreeEntries[ROOT].Children = append(rootChildren, path)
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk failed: %w", err)
	}

	if !wrapInDir {
		var rootLevelFiles []string
		var hasDirectories bool

		for path, entry := range summary.TreeEntries {
			if path == ROOT {
				continue
			}
			if entry.IsDir {
				hasDirectories = true
				break
			}
			if entry.Path == "" {
				rootLevelFiles = append(rootLevelFiles, path)
			}
		}

		if !hasDirectories && len(rootLevelFiles) == 1 {
			fileEntry := summary.TreeEntries[rootLevelFiles[0]]
			if fileEntry.CID != cid.Undef {
				summary.RootCID = fileEntry.CID
				b.summary = summary
				return summary, nil
			}
		}
	}

	// Build directory blocks - already have parent-child relationships from walk
	// Collect only directories
	dirPaths := make([]string, 0, len(summary.TreeEntries))
	for path, entry := range summary.TreeEntries {
		if entry.IsDir {
			dirPaths = append(dirPaths, path)
		}
	}

	// Sort by depth descending (deepest first) to process children before parents
	sort.Slice(dirPaths, func(i, j int) bool {
		if dirPaths[i] == ROOT {
			return false
		}
		if dirPaths[j] == ROOT {
			return true
		}
		return pathDepth(dirPaths[i]) > pathDepth(dirPaths[j])
	})

	for _, path := range dirPaths {
		entry := summary.TreeEntries[path]

		dirCID, blockSize, err := b.createDirectoryBlock(ctx, entry, summary.TreeEntries)
		if err != nil {
			return nil, err
		}

		// Update CIDToEntry if CID changed
		if entry.CID != cid.Undef {
			delete(summary.CIDToEntry, entry.CID)
		}
		entry.CID = dirCID
		summary.CIDToEntry[dirCID] = entry

		summary.BlockOrder = append(summary.BlockOrder, dirCID)
		summary.BlockSizes = append(summary.BlockSizes, blockSize)
		summary.TotalSize += uint64(dirCID.ByteLen())
	}

	root := summary.TreeEntries[ROOT]
	if root == nil {
		return nil, fmt.Errorf("root not found")
	}

	summary.RootCID = root.CID

	if err := b.pruneEmptyDirectories(summary); err != nil {
		return nil, fmt.Errorf("prune empty directories: %w", err)
	}

	// After pruning, regenerate all directory blocks from bottom up
	// because removing children from a parent changes its content and thus its CID
	dirPaths = dirPaths[:0] // Reuse existing slice
	for path, entry := range summary.TreeEntries {
		if entry.IsDir {
			dirPaths = append(dirPaths, path)
		}
	}

	sort.Slice(dirPaths, func(i, j int) bool {
		return pathDepth(dirPaths[i]) > pathDepth(dirPaths[j])
	})

	for _, path := range dirPaths {
		entry := summary.TreeEntries[path]
		if entry == nil {
			continue
		}

		oldCID := entry.CID
		dirCID, blockSize, err := b.createDirectoryBlock(ctx, entry, summary.TreeEntries)
		if err != nil {
			return nil, fmt.Errorf("regenerate directory %s: %w", path, err)
		}

		// Update CIDToEntry
		if oldCID != cid.Undef {
			delete(summary.CIDToEntry, oldCID)
		}
		entry.CID = dirCID
		summary.CIDToEntry[dirCID] = entry

		// Update BlockOrder and BlockSizes
		found := false
		for i, blockCID := range summary.BlockOrder {
			if blockCID.Equals(oldCID) {
				summary.BlockOrder[i] = dirCID
				summary.BlockSizes[i] = blockSize
				found = true
				break
			}
		}

		if !found && oldCID != cid.Undef && len(entry.Children) > 0 {
			summary.BlockOrder = append(summary.BlockOrder, dirCID)
			summary.BlockSizes = append(summary.BlockSizes, blockSize)
		}
	}

	// Update RootCID to the final root CID after all regeneration
	summary.RootCID = root.CID

	b.summary = summary
	return summary, nil
}

// WriteCAR performs pass 2: writes CARv1 using the summary,
// regenerating blocks from the filesystem on the fly.
func (b *CARBuilder) WriteCAR(ctx context.Context, w io.Writer) error {
	if b.summary == nil {
		return fmt.Errorf("summary not built, call BuildSummary first")
	}

	v1Header := &carv1.CarHeader{
		Version: 1,
		Roots:   []cid.Cid{b.summary.RootCID},
	}
	if err := carv1.WriteHeader(v1Header, w); err != nil {
		return fmt.Errorf("write CARv1 header: %w", err)
	}

	for _, blockCID := range b.summary.BlockOrder {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := b.writeBlockToCAR(ctx, blockCID, w); err != nil {
			return fmt.Errorf("write block %s: %w", blockCID, err)
		}
	}

	return nil
}

// BuildAndWrite is a convenience method that performs both passes in sequence.
func (b *CARBuilder) BuildAndWrite(ctx context.Context, filesystem fs.FS, w io.Writer, wrapInDir bool) (cid.Cid, error) {
	summary, err := b.BuildSummary(ctx, filesystem, wrapInDir)
	if err != nil {
		return cid.Cid{}, err
	}

	rootCID := encoding.NormalizeCid(summary.RootCID)

	if err := b.WriteCAR(ctx, w); err != nil {
		return cid.Cid{}, err
	}

	return rootCID, nil
}

// GetSummary returns the tree summary after BuildSummary has been called.
func (b *CARBuilder) GetSummary() *TreeSummary {
	return b.summary
}

func (b *CARBuilder) createUnixFSBlocks(ctx context.Context, r io.Reader) (cid.Cid, []cid.Cid, []uint64, error) {
	if err := ctx.Err(); err != nil {
		return cid.Cid{}, nil, nil, err
	}

	nd, err := b.generator.CreateUnixFSNode(ctx, uploadio.NewReadSeekCloser(r), helpers.DefaultLinksPerBlock, b.chunkSize)
	if err != nil {
		return cid.Cid{}, nil, nil, fmt.Errorf("create unixfs node: %w", err)
	}

	pbNode, ok := nd.(*merkledag.ProtoNode)
	if !ok {
		return nd.Cid(), []cid.Cid{nd.Cid()}, []uint64{uint64(len(nd.RawData()))}, nil
	}

	var cids []cid.Cid
	var sizes []uint64

	cids = append(cids, nd.Cid())
	sizes = append(sizes, uint64(len(pbNode.RawData())))

	return nd.Cid(), cids, sizes, nil
}

func (b *CARBuilder) createDirectoryBlock(ctx context.Context, entry *TreeEntry, entries map[string]*TreeEntry) (cid.Cid, uint64, error) {
	if err := ctx.Err(); err != nil {
		return cid.Cid{}, 0, err
	}

	children := lo.FilterMap(entry.Children, func(childPath string, _ int) (DirectoryChild, bool) {
		child := entries[childPath]
		if child == nil || child.CID == cid.Undef {
			return DirectoryChild{}, false
		}
		return DirectoryChild{
			Name: child.Name,
			CID:  child.CID,
			Size: uint64(child.CID.ByteLen()),
		}, true
	})

	node, err := b.generator.CreateDirectoryWithLinks(ctx, children)
	if err != nil {
		return cid.Cid{}, 0, err
	}

	return node.Cid(), uint64(len(node.RawData())), nil
}

func (b *CARBuilder) writeBlockToCAR(ctx context.Context, blockCID cid.Cid, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Try to fetch from blockstore first (LRU might have evicted it)
	blk, err := b.bs.Get(ctx, blockCID)
	if err == nil {
		return carv1.WriteBlock(w, blockCID, blk.RawData())
	}

	// Block not in blockstore, regenerate it using the standard builders
	entry := b.summary.CIDToEntry[blockCID]
	if entry == nil {
		return fmt.Errorf("block not found in summary: %s", blockCID)
	}

	// Regenerate the block using the standard path (adds to blockstore)
	if entry.IsDir {
		_, _, err = b.createDirectoryBlock(ctx, entry, b.summary.TreeEntries)
	} else {
		filePath := filepath.Join(entry.Path, entry.Name)
		file, err := b.filesystem.Open(filePath)
		if err != nil {
			return err
		}
		defer file.Close()
		_, _, _, err = b.createUnixFSBlocks(ctx, file)
	}
	if err != nil {
		return fmt.Errorf("regenerate block %s: %w", blockCID, err)
	}

	// Fetch the regenerated block from blockstore
	blk, err = b.bs.Get(ctx, blockCID)
	if err != nil {
		return fmt.Errorf("fetch regenerated block %s: %w", blockCID, err)
	}

	return carv1.WriteBlock(w, blockCID, blk.RawData())
}

func (b *CARBuilder) pruneEmptyDirectories(summary *TreeSummary) error {
	emptyDirs := make(map[string]bool)

	for path, entry := range summary.TreeEntries {
		if !entry.IsDir || path == ROOT {
			continue
		}

		if b.isDirectoryEmpty(summary, path) {
			emptyDirs[path] = true
		}
	}

	for path := range emptyDirs {
		entry := summary.TreeEntries[path]
		parentPath := entry.Path
		if parentPath == "" {
			parentPath = ROOT
		}

		parentEntry := summary.TreeEntries[parentPath]
		if parentEntry == nil {
			continue
		}

		parentEntry.Children = removeString(parentEntry.Children, path)

		if entry.CID != cid.Undef {
			for i, blockCID := range summary.BlockOrder {
				if blockCID.Equals(entry.CID) {
					summary.BlockOrder = append(summary.BlockOrder[:i], summary.BlockOrder[i+1:]...)
					summary.BlockSizes = append(summary.BlockSizes[:i], summary.BlockSizes[i+1:]...)
					delete(summary.CIDToEntry, entry.CID)
					break
				}
			}
		}
	}

	return nil
}

func (b *CARBuilder) isDirectoryEmpty(summary *TreeSummary, path string) bool {
	entry := summary.TreeEntries[path]
	if entry == nil || !entry.IsDir {
		return false
	}

	if len(entry.Children) == 0 {
		return true
	}

	for _, childPath := range entry.Children {
		child := summary.TreeEntries[childPath]
		if child == nil {
			continue
		}

		if !child.IsDir {
			return false
		}

		if !b.isDirectoryEmpty(summary, childPath) {
			return false
		}
	}

	return true
}

func removeString(slice []string, s string) []string {
	for i, item := range slice {
		if item == s {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

func pathDepth(path string) int {
	return strings.Count(path, "/")
}
