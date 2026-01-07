package io

import (
	"context"

	blockstore "github.com/ipfs/boxo/blockstore"
	"github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
)

// DualBlockstore wraps two blockstores and writes to both, but reads from the primary.
// This is useful for testing where you want to persist blocks in a map for later verification
// while also using an LRU blockstore for memory management.
type DualBlockstore struct {
	primary   blockstore.Blockstore
	secondary blockstore.Blockstore
}

// NewDualBlockstore creates a new DualBlockstore that writes to both blockstores
// but reads from the primary.
func NewDualBlockstore(primary, secondary blockstore.Blockstore) *DualBlockstore {
	return &DualBlockstore{
		primary:   primary,
		secondary: secondary,
	}
}

func (d *DualBlockstore) Put(ctx context.Context, blk blocks.Block) error {
	// Write to both blockstores
	if err := d.primary.Put(ctx, blk); err != nil {
		return err
	}
	return d.secondary.Put(ctx, blk)
}

func (d *DualBlockstore) Get(ctx context.Context, c cid.Cid) (blocks.Block, error) {
	// Read from primary
	return d.primary.Get(ctx, c)
}

func (d *DualBlockstore) GetSize(ctx context.Context, c cid.Cid) (int, error) {
	// Get size from primary
	return d.primary.GetSize(ctx, c)
}

func (d *DualBlockstore) Has(ctx context.Context, c cid.Cid) (bool, error) {
	// Check primary
	return d.primary.Has(ctx, c)
}

func (d *DualBlockstore) PutMany(ctx context.Context, blks []blocks.Block) error {
	// Write to both blockstores
	if err := d.primary.PutMany(ctx, blks); err != nil {
		return err
	}
	return d.secondary.PutMany(ctx, blks)
}

func (d *DualBlockstore) AllKeysChan(ctx context.Context) (<-chan cid.Cid, error) {
	// Get keys from primary
	return d.primary.AllKeysChan(ctx)
}

func (d *DualBlockstore) DeleteBlock(ctx context.Context, c cid.Cid) error {
	// Delete from both blockstores
	if err := d.primary.DeleteBlock(ctx, c); err != nil {
		return err
	}
	return d.secondary.DeleteBlock(ctx, c)
}
