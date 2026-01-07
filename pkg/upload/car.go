package upload

import (
	"context"
	"fmt"
	"io"
	"io/fs"

	"github.com/docker/go-units"
	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/blockstore"
	"github.com/ipfs/boxo/exchange/offline"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/go-cid"
	format "github.com/ipfs/go-ipld-format"
	"github.com/multiformats/go-varint"

	"go.lumeweb.com/pinner-cli/pkg/upload/internal/carv1"
	"go.lumeweb.com/pinner-cli/pkg/upload/internal/encoding"
)

// DefaultMemoryLimit is the default memory limit for LRU blockstore operations (100MB).
const DefaultMemoryLimit = 100 * units.MiB

// NewDAGServiceWithMemoryLimit creates a new LRU blockstore, blockservice, and DAG service trio
// with the specified memory limit. This is a convenience function for setting up the IPFS
// DAG infrastructure with memory-constrained block storage.
//
// The returned blockstore implements LRU eviction when the memory limit is exceeded,
// making it suitable for CAR file generation where you want to limit memory usage.
func NewDAGServiceWithMemoryLimit(memoryLimit uint64) (blockstore.Blockstore, format.DAGService) {
	bs := NewLRUBlockstore(memoryLimit)
	bsvc := blockservice.New(bs, offline.Exchange(bs))
	dagService := merkledag.NewDAGService(bsvc)
	return bs, dagService
}

// StreamCAR is a convenience function that builds a directory tree from the
// given filesystem and writes it as a CARv1 to the provided writer.
// If wrapInDir is true, the content will be wrapped in a root directory (default behavior).
// If wrapInDir is false and there's only one file, the file itself will be the root.
func StreamCAR(ctx context.Context, filesystem fs.FS, w io.Writer, maxMemory uint64, wrapInDir bool) (cid.Cid, error) {
	bs, dagService := NewDAGServiceWithMemoryLimit(maxMemory)
	generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))

	builder := NewCARBuilder(bs, dagService, generator)
	rootCID, err := builder.BuildAndWrite(ctx, filesystem, w, wrapInDir)
	if err != nil {
		return cid.Cid{}, fmt.Errorf("build: %w", err)
	}

	return rootCID, nil
}

// StreamCARWithSize builds a directory tree and returns the root CID and the total CAR file size.
// This is useful for TUS uploads where the size must be known upfront.
// If wrapInDir is true, the content will be wrapped in a root directory (default behavior).
// If wrapInDir is false and there's only one file, the file itself will be the root.
func StreamCARWithSize(ctx context.Context, filesystem fs.FS, w io.Writer, maxMemory uint64, wrapInDir bool) (cid.Cid, int64, error) {
	bs, dagService := NewDAGServiceWithMemoryLimit(maxMemory)
	generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))

	builder := NewCARBuilder(bs, dagService, generator)
	summary, err := builder.BuildSummary(ctx, filesystem, wrapInDir)
	if err != nil {
		return cid.Cid{}, 0, fmt.Errorf("build tree summary: %w", err)
	}

	// Normalize root CID to v1 format for consistency
	summary.RootCID = encoding.NormalizeCid(summary.RootCID)

	// Calculate CAR size with all overhead
	carSize, err := CalculateCARSize(summary)
	if err != nil {
		return cid.Cid{}, 0, err
	}

	// Write CAR to the provided writer
	if err = builder.WriteCAR(ctx, w); err != nil {
		return cid.Cid{}, 0, fmt.Errorf("write CAR: %w", err)
	}

	return summary.RootCID, carSize, nil
}

// CalculateCARSize computes the total CAR file size including header and all blocks.
// The CAR format is: [header] [block1] [block2] ... [blockN]
// Where each block is: [length] [CID bytes] [data bytes]
// The length is a varint encoding of (CID length + data length)
func CalculateCARSize(summary *TreeSummary) (int64, error) {
	// Calculate header size using the same method as WriteHeader
	v1Header := &carv1.CarHeader{
		Version: 1,
		Roots:   []cid.Cid{encoding.NormalizeCid(summary.RootCID)},
	}
	headerSize, err := carv1.HeaderSize(v1Header)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate CAR header size: %w", err)
	}

	// Calculate size for each block
	blocksSize := uint64(0)
	for i, blockCID := range summary.BlockOrder {
		normalizedCID := encoding.NormalizeCid(blockCID)

		cidLen := uint64(normalizedCID.ByteLen())
		dataLen := summary.BlockSizes[i]
		payloadSize := cidLen + dataLen
		// Each block: length varint + CID bytes + data bytes
		blocksSize += uint64(varint.UvarintSize(payloadSize)) + cidLen + dataLen
	}

	summary.CARSize = headerSize + blocksSize
	return int64(summary.CARSize), nil
}
