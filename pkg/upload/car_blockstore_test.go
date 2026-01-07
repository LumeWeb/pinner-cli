package upload

import (
	"context"
	"testing"

	"github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
)

func TestLRUBlockstore_Basic(t *testing.T) {
	bs := NewLRUBlockstore(100)
	ctx := context.Background()

	data1 := []byte("hello")
	data2 := []byte("world")

	c1, _ := cid.Prefix{Version: 1, Codec: 0x55, MhType: 0x12, MhLength: 32}.Sum(data1)
	c2, _ := cid.Prefix{Version: 1, Codec: 0x55, MhType: 0x12, MhLength: 32}.Sum(data2)

	blk1, _ := blocks.NewBlockWithCid(data1, c1)
	blk2, _ := blocks.NewBlockWithCid(data2, c2)

	if err := bs.Put(ctx, blk1); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if err := bs.Put(ctx, blk2); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if bs.Size() != 10 {
		t.Fatalf("Expected size 10, got %d", bs.Size())
	}
	if bs.Len() != 2 {
		t.Fatalf("Expected len 2, got %d", bs.Len())
	}
}

func TestLRUBlockstore_LRUEviction(t *testing.T) {
	bs := NewLRUBlockstore(10)
	ctx := context.Background()

	data1 := []byte("hello")
	data2 := []byte("world")
	data3 := []byte("test")

	c1, _ := cid.Prefix{Version: 1, Codec: 0x55, MhType: 0x12, MhLength: 32}.Sum(data1)
	c2, _ := cid.Prefix{Version: 1, Codec: 0x55, MhType: 0x12, MhLength: 32}.Sum(data2)
	c3, _ := cid.Prefix{Version: 1, Codec: 0x55, MhType: 0x12, MhLength: 32}.Sum(data3)

	blk1, _ := blocks.NewBlockWithCid(data1, c1)
	blk2, _ := blocks.NewBlockWithCid(data2, c2)
	blk3, _ := blocks.NewBlockWithCid(data3, c3)

	bs.Put(ctx, blk1)
	bs.Put(ctx, blk2)

	if bs.Size() != 10 {
		t.Fatalf("Expected size 10, got %d", bs.Size())
	}

	bs.Get(ctx, c1)

	if err := bs.Put(ctx, blk3); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if bs.Size() != 9 {
		t.Fatalf("Expected size 9, got %d", bs.Size())
	}
	if bs.Len() != 2 {
		t.Fatalf("Expected len 2, got %d", bs.Len())
	}

	has1, _ := bs.Has(ctx, c1)
	has2, _ := bs.Has(ctx, c2)
	has3, _ := bs.Has(ctx, c3)

	if !has1 {
		t.Error("c1 should exist (accessed recently)")
	}
	if has2 {
		t.Error("c2 should be evicted (least recently used)")
	}
	if !has3 {
		t.Error("c3 should exist")
	}
}

func TestLRUBlockstore_DeleteBlock(t *testing.T) {
	bs := NewLRUBlockstore(100)
	ctx := context.Background()

	data := []byte("hello")
	c, _ := cid.Prefix{Version: 1, Codec: 0x55, MhType: 0x12, MhLength: 32}.Sum(data)
	blk, _ := blocks.NewBlockWithCid(data, c)

	bs.Put(ctx, blk)

	if bs.Size() != 5 {
		t.Fatalf("Expected size 5, got %d", bs.Size())
	}

	if err := bs.DeleteBlock(ctx, c); err != nil {
		t.Fatalf("DeleteBlock failed: %v", err)
	}

	if bs.Size() != 0 {
		t.Fatalf("Expected size 0, got %d", bs.Size())
	}

	has, _ := bs.Has(ctx, c)
	if has {
		t.Error("Block should not exist after deletion")
	}
}

func TestLRUBlockstore_GetSize(t *testing.T) {
	bs := NewLRUBlockstore(100)
	ctx := context.Background()

	data := []byte("hello")
	c, _ := cid.Prefix{Version: 1, Codec: 0x55, MhType: 0x12, MhLength: 32}.Sum(data)
	blk, _ := blocks.NewBlockWithCid(data, c)

	bs.Put(ctx, blk)

	size, err := bs.GetSize(ctx, c)
	if err != nil {
		t.Fatalf("GetSize failed: %v", err)
	}
	if size != 5 {
		t.Fatalf("Expected size 5, got %d", size)
	}
}

func TestLRUBlockstore_AllKeysChan_MetadataSafety(t *testing.T) {
	bs := NewLRUBlockstore(10)
	ctx := context.Background()

	data1 := []byte("hello")
	data2 := []byte("world")
	data3 := []byte("test")

	c1, _ := cid.Prefix{Version: 1, Codec: 0x55, MhType: 0x12, MhLength: 32}.Sum(data1)
	c2, _ := cid.Prefix{Version: 1, Codec: 0x55, MhType: 0x12, MhLength: 32}.Sum(data2)
	c3, _ := cid.Prefix{Version: 1, Codec: 0x55, MhType: 0x12, MhLength: 32}.Sum(data3)

	blk1, _ := blocks.NewBlockWithCid(data1, c1)
	blk2, _ := blocks.NewBlockWithCid(data2, c2)
	blk3, _ := blocks.NewBlockWithCid(data3, c3)

	bs.Put(ctx, blk1)
	bs.Put(ctx, blk2)

	// Get keys - should return 2 CIDs
	keysChan, err := bs.AllKeysChan(ctx)
	if err != nil {
		t.Fatalf("AllKeysChan failed: %v", err)
	}

	keys := make([]cid.Cid, 0, 2)
	for c := range keysChan {
		keys = append(keys, c)
	}

	if len(keys) != 2 {
		t.Fatalf("Expected 2 keys, got %d", len(keys))
	}

	// Add third block, which will cause eviction of one block
	bs.Put(ctx, blk3)

	// Get keys again - should return all 3 CIDs (including evicted one)
	keysChan, err = bs.AllKeysChan(ctx)
	if err != nil {
		t.Fatalf("AllKeysChan failed: %v", err)
	}

	keys = make([]cid.Cid, 0, 3)
	for c := range keysChan {
		keys = append(keys, c)
	}

	if len(keys) != 3 {
		t.Fatalf("Expected 3 keys (including evicted), got %d", len(keys))
	}

	// Verify all original CIDs are present
	cidMap := make(map[cid.Cid]bool)
	for _, c := range keys {
		cidMap[c] = true
	}

	if !cidMap[c1] {
		t.Error("c1 should be in key list (metadata safety)")
	}
	if !cidMap[c2] {
		t.Error("c2 should be in key list (metadata safety)")
	}
	if !cidMap[c3] {
		t.Error("c3 should be in key list")
	}

	// Verify only 2 blocks are actually in the blockstore (one was evicted)
	if bs.Len() != 2 {
		t.Fatalf("Expected 2 blocks in blockstore, got %d", bs.Len())
	}
}

func TestLRUBlockstore_AllKeysChan_DuplicatePut(t *testing.T) {
	bs := NewLRUBlockstore(100)
	ctx := context.Background()

	data := []byte("hello")
	c, _ := cid.Prefix{Version: 1, Codec: 0x55, MhType: 0x12, MhLength: 32}.Sum(data)
	blk, _ := blocks.NewBlockWithCid(data, c)

	bs.Put(ctx, blk)
	bs.Put(ctx, blk) // Put same block twice

	keysChan, err := bs.AllKeysChan(ctx)
	if err != nil {
		t.Fatalf("AllKeysChan failed: %v", err)
	}

	keys := make([]cid.Cid, 0, 1)
	for c := range keysChan {
		keys = append(keys, c)
	}

	// Should only return 1 unique CID
	if len(keys) != 1 {
		t.Fatalf("Expected 1 unique key, got %d", len(keys))
	}

	if !keys[0].Equals(c) {
		t.Error("Expected CID not found")
	}

	// Blockstore should only have 1 block
	if bs.Len() != 1 {
		t.Fatalf("Expected 1 block in blockstore, got %d", bs.Len())
	}
}

func TestLRUBlockstore_AllKeysChan_DeleteBlock(t *testing.T) {
	bs := NewLRUBlockstore(100)
	ctx := context.Background()

	data1 := []byte("hello")
	data2 := []byte("world")

	c1, _ := cid.Prefix{Version: 1, Codec: 0x55, MhType: 0x12, MhLength: 32}.Sum(data1)
	c2, _ := cid.Prefix{Version: 1, Codec: 0x55, MhType: 0x12, MhLength: 32}.Sum(data2)

	blk1, _ := blocks.NewBlockWithCid(data1, c1)
	blk2, _ := blocks.NewBlockWithCid(data2, c2)

	bs.Put(ctx, blk1)
	bs.Put(ctx, blk2)

	// Delete one block
	bs.DeleteBlock(ctx, c1)

	// AllKeysChan should still return both CIDs (metadata safety)
	keysChan, err := bs.AllKeysChan(ctx)
	if err != nil {
		t.Fatalf("AllKeysChan failed: %v", err)
	}

	keys := make([]cid.Cid, 0, 2)
	for c := range keysChan {
		keys = append(keys, c)
	}

	if len(keys) != 2 {
		t.Fatalf("Expected 2 keys (including deleted), got %d", len(keys))
	}

	// Verify only 1 block is actually in the blockstore
	if bs.Len() != 1 {
		t.Fatalf("Expected 1 block in blockstore, got %d", bs.Len())
	}

	// Verify the deleted block is not accessible
	_, err = bs.Get(ctx, c1)
	if err == nil {
		t.Error("Expected error when getting deleted block")
	}
}
