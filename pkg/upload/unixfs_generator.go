package upload

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ipfs/boxo/blockstore"
	chunker "github.com/ipfs/boxo/chunker"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs"
	"github.com/ipfs/boxo/ipld/unixfs/importer/balanced"
	"github.com/ipfs/boxo/ipld/unixfs/importer/helpers"
	unixfsio "github.com/ipfs/boxo/ipld/unixfs/io"
	"github.com/ipfs/boxo/verifcid"
	"github.com/ipfs/go-cid"
	format "github.com/ipfs/go-ipld-format"
	"github.com/multiformats/go-multicodec"
)

// DirectoryChild represents a child entry in a directory.
type DirectoryChild struct {
	Name string
	CID  cid.Cid
	Size uint64
}

// UnixFSNodeGenerator defines the interface for creating UnixFS nodes from readers.
type UnixFSNodeGenerator interface {
	// CreateNode creates a UnixFS node from a reader using default parameters.
	CreateNode(ctx context.Context, reader io.ReadSeekCloser) (format.Node, error)

	// CreateUnixFSNode creates a UnixFS node from a reader with specified parameters.
	CreateUnixFSNode(ctx context.Context, r io.ReadSeekCloser, maxlinks int, chunkSize int64) (format.Node, error)

	// CreateDAGFromReader creates a DAG from a reader with the given parameters.
	CreateDAGFromReader(ctx context.Context, reader io.Reader, maxlinks int, chunkSize int64, rawLeaves bool) (format.Node, error)

	// CreateDirectory creates an empty UnixFS directory.
	CreateDirectory() (unixfsio.Directory, error)

	// CreateDirectoryWithLinks creates a UnixFS directory node with the specified child links.
	CreateDirectoryWithLinks(ctx context.Context, children []DirectoryChild) (format.Node, error)

	// GetDAGService returns the underlying DAG service.
	GetDAGService() format.DAGService

	// GetBlockstore returns the underlying blockstore.
	GetBlockstore() blockstore.Blockstore
}

// UnixFSNodeGeneratorOptions holds configuration options for UnixFSNodeGenerator.
type UnixFSNodeGeneratorOptions struct {
	DAGService format.DAGService
	Blockstore blockstore.Blockstore
}

// UnixFSNodeGeneratorOption is a function that configures UnixFSNodeGeneratorOptions.
type UnixFSNodeGeneratorOption func(*UnixFSNodeGeneratorOptions)

// WithUnixFSNodeDAGService sets the DAG service for the node generator.
func WithUnixFSNodeDAGService(dagService format.DAGService) UnixFSNodeGeneratorOption {
	return func(opts *UnixFSNodeGeneratorOptions) {
		opts.DAGService = dagService
	}
}

// WithUnixFSNodeBlockstore sets the blockstore for the node generator.
func WithUnixFSNodeBlockstore(blockstore blockstore.Blockstore) UnixFSNodeGeneratorOption {
	return func(opts *UnixFSNodeGeneratorOptions) {
		opts.Blockstore = blockstore
	}
}

// IPFSUnixFSNodeGenerator implements the UnixFSNodeGenerator interface using IPFS libraries.
type IPFSUnixFSNodeGenerator struct {
	dagService format.DAGService
	blockstore blockstore.Blockstore
}

// NewUnixFSNodeGeneratorWithOptions creates a new UnixFSNodeGenerator instance with configurable options.
func NewUnixFSNodeGenerator(options ...UnixFSNodeGeneratorOption) UnixFSNodeGenerator {
	opts := &UnixFSNodeGeneratorOptions{}
	for _, option := range options {
		option(opts)
	}

	return &IPFSUnixFSNodeGenerator{
		dagService: opts.DAGService,
		blockstore: opts.Blockstore,
	}
}

// CreateDirectory implements UnixFSNodeGenerator.CreateDirectory.
func (gen *IPFSUnixFSNodeGenerator) CreateDirectory() (unixfsio.Directory, error) {
	return unixfsio.NewDirectory(gen.dagService)
}

// CreateDirectoryWithLinks implements UnixFSNodeGenerator.CreateDirectoryWithLinks.
func (gen *IPFSUnixFSNodeGenerator) CreateDirectoryWithLinks(ctx context.Context, children []DirectoryChild) (format.Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	pbNode := merkledag.NodeWithData(unixfs.FolderPBData())

	if err := pbNode.SetCidBuilder(cid.V1Builder{Codec: cid.DagProtobuf, MhType: uint64(multicodec.Sha2_256)}); err != nil {
		return nil, err
	}

	for _, child := range children {
		if err := pbNode.AddRawLink(child.Name, &format.Link{
			Cid:  child.CID,
			Name: child.Name,
			Size: child.Size,
		}); err != nil {
			return nil, err
		}
	}

	if err := gen.dagService.Add(ctx, pbNode); err != nil {
		return nil, fmt.Errorf("add directory block to DAG service: %w", err)
	}

	return pbNode, nil
}

// CreateNode implements UnixFSNodeGenerator.CreateNode.
func (gen *IPFSUnixFSNodeGenerator) CreateNode(ctx context.Context, r io.ReadSeekCloser) (format.Node, error) {
	return gen.CreateUnixFSNode(ctx, r, helpers.DefaultLinksPerBlock, 0)
}

// CreateUnixFSNode implements UnixFSNodeGenerator.CreateUnixFSNode.
func (gen *IPFSUnixFSNodeGenerator) CreateUnixFSNode(ctx context.Context, r io.ReadSeekCloser, maxlinks int, chunkSize int64) (format.Node, error) {
	// Check for context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// First attempt with rawLeaves=false
	node, err := gen.CreateDAGFromReader(ctx, r, maxlinks, chunkSize, false)
	if err != nil && strings.Contains(err.Error(), verifcid.ErrDigestTooLarge.Error()) {
		// Retry with rawLeaves=true for large content
		// Seek back to start for retry
		_, seekErr := r.Seek(0, io.SeekStart)
		if seekErr != nil {
			return nil, fmt.Errorf("failed to seek to start for retry: %w", seekErr)
		}
		node, err = gen.CreateDAGFromReader(ctx, r, maxlinks, chunkSize, true)
	}

	return node, err
}

// CreateDAGFromReader implements UnixFSNodeGenerator.CreateDAGFromReader.
func (gen *IPFSUnixFSNodeGenerator) CreateDAGFromReader(ctx context.Context, reader io.Reader, maxlinks int, chunkSize int64, rawLeaves bool) (format.Node, error) {
	// Check for context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if reader == nil {
		return nil, fmt.Errorf("reader cannot be nil")
	}

	if maxlinks == 0 {
		maxlinks = helpers.DefaultLinksPerBlock
	}

	if chunkSize == 0 {
		chunkSize = 1024 * 1024 // 1MB default chunk size
	}

	codec := uint64(cid.DagProtobuf)
	if rawLeaves {
		codec = uint64(cid.Raw)
	}
	builder := cid.V1Builder{Codec: codec, MhType: uint64(multicodec.Sha2_256)}

	dbp := &helpers.DagBuilderParams{
		Dagserv:    gen.dagService,
		RawLeaves:  rawLeaves,
		Maxlinks:   maxlinks,
		CidBuilder: builder,
	}

	spl := chunker.NewSizeSplitter(reader, chunkSize)
	db, err := dbp.New(spl)
	if err != nil {
		return nil, fmt.Errorf("failed to create dag builder: %w", err)
	}

	// Check for context cancellation before layout
	if err = ctx.Err(); err != nil {
		return nil, err
	}

	nd, err := balanced.Layout(db)
	if err != nil {
		return nil, fmt.Errorf("failed to build balanced layout: %w", err)
	}

	return nd, nil
}

// GetDAGService implements UnixFSNodeGenerator.GetDAGService.
func (gen *IPFSUnixFSNodeGenerator) GetDAGService() format.DAGService {
	return gen.dagService
}

// GetBlockstore implements UnixFSNodeGenerator.GetBlockstore.
func (gen *IPFSUnixFSNodeGenerator) GetBlockstore() blockstore.Blockstore {
	return gen.blockstore
}
