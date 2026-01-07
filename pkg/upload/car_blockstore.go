package upload

import (
	"context"
	"fmt"
	"sync"

	"github.com/ipfs/boxo/blockstore"
	"github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
)

// ErrNotFound is returned when a block is not found.
var ErrNotFound = fmt.Errorf("block not found")

// lruNode represents a node in the LRU doubly-linked list.
type lruNode struct {
	cid  cid.Cid
	data []byte
	prev *lruNode
	next *lruNode
}

// LRUBlockstore is a thread-safe blockstore with LRU eviction based on total size in bytes.
// When the size limit is exceeded, the least recently used blocks are evicted to make room.
// This is useful for limiting memory usage during CAR file generation.
type LRUBlockstore struct {
	mu     sync.RWMutex
	blocks map[cid.Cid]*lruNode
	size   uint64
	limit  uint64
	head   *lruNode
	tail   *lruNode
}

// NewLRUBlockstore creates a new LRU blockstore with the given size limit (in bytes).
// The sizeLimit parameter determines the maximum total size of all blocks that can be stored.
// When adding blocks would exceed this limit, the least recently used blocks are evicted.
func NewLRUBlockstore(sizeLimit uint64) *LRUBlockstore {
	return &LRUBlockstore{
		blocks: make(map[cid.Cid]*lruNode),
		limit:  sizeLimit,
	}
}

// Put adds a block to the blockstore. If the block already exists, it is moved to the front of the LRU list.
// If adding the block would exceed the size limit, the least recently used blocks are evicted.
func (b *LRUBlockstore) Put(_ context.Context, blk blocks.Block) error {
	c := blk.Cid()
	data := blk.RawData()
	blockSize := uint64(len(data))

	b.mu.Lock()
	defer b.mu.Unlock()

	if node, exists := b.blocks[c]; exists {
		b.moveToFront(node)
		return nil
	}

	for b.size+blockSize > b.limit && len(b.blocks) > 0 {
		b.evictOne()
	}

	node := &lruNode{
		cid:  c,
		data: data,
	}
	b.addToFront(node)
	b.blocks[c] = node
	b.size += blockSize
	return nil
}

func (b *LRUBlockstore) evictOne() {
	if b.tail == nil {
		return
	}

	delete(b.blocks, b.tail.cid)
	b.size -= uint64(len(b.tail.data))

	if b.head == b.tail {
		b.head = nil
		b.tail = nil
	} else {
		b.tail = b.tail.prev
		b.tail.next = nil
	}
}

func (b *LRUBlockstore) moveToFront(node *lruNode) {
	if node == b.head {
		return
	}

	if node.prev != nil {
		node.prev.next = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	}
	if node == b.tail {
		b.tail = node.prev
	}

	b.addToFront(node)
}

func (b *LRUBlockstore) addToFront(node *lruNode) {
	node.prev = nil
	node.next = b.head

	if b.head != nil {
		b.head.prev = node
	}
	b.head = node

	if b.tail == nil {
		b.tail = node
	}
}

// Get retrieves a block by CID. If found, the block is moved to the front of the LRU list.
// Returns ErrNotFound if the block does not exist.
func (b *LRUBlockstore) Get(_ context.Context, c cid.Cid) (blocks.Block, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	node, ok := b.blocks[c]
	if !ok {
		return nil, ErrNotFound
	}

	b.moveToFront(node)
	block, _ := blocks.NewBlockWithCid(node.data, c)
	return block, nil
}

// GetSize returns the size of a block in bytes. Returns -1 and ErrNotFound if the block does not exist.
func (b *LRUBlockstore) GetSize(_ context.Context, c cid.Cid) (int, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	node, ok := b.blocks[c]
	if !ok {
		return -1, ErrNotFound
	}
	return len(node.data), nil
}

// Has checks if a block exists in the blockstore. Does not affect LRU order.
func (b *LRUBlockstore) Has(_ context.Context, c cid.Cid) (bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	_, ok := b.blocks[c]
	return ok, nil
}

// PutMany adds multiple blocks to the blockstore. Each block is added individually with LRU eviction.
func (b *LRUBlockstore) PutMany(ctx context.Context, blocks []blocks.Block) error {
	for _, blk := range blocks {
		if err := b.Put(ctx, blk); err != nil {
			return err
		}
	}
	return nil
}

func (b *LRUBlockstore) AllKeysChan(_ context.Context) (<-chan cid.Cid, error) {
	b.mu.RLock()
	keys := make([]cid.Cid, 0, len(b.blocks))
	for c := range b.blocks {
		keys = append(keys, c)
	}
	b.mu.RUnlock()

	ch := make(chan cid.Cid, len(keys))
	go func() {
		defer close(ch)
		for _, c := range keys {
			ch <- c
		}
	}()
	return ch, nil
}

// DeleteBlock removes a block from the blockstore. No-op if the block does not exist.
func (b *LRUBlockstore) DeleteBlock(_ context.Context, c cid.Cid) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	node, ok := b.blocks[c]
	if !ok {
		return nil
	}

	b.size -= uint64(len(node.data))
	delete(b.blocks, c)

	if node.prev != nil {
		node.prev.next = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	}
	if node == b.head {
		b.head = node.next
	}
	if node == b.tail {
		b.tail = node.prev
	}

	return nil
}

// Close is a no-op for this blockstore.
func (b *LRUBlockstore) Close() error {
	return nil
}

// Size returns the total size of all blocks in bytes.
func (b *LRUBlockstore) Size() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size
}

// Len returns the number of blocks in the blockstore.
func (b *LRUBlockstore) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.blocks)
}

// compile-time check that LRUBlockstore implements blockstore.Blockstore
var _ blockstore.Blockstore = (*LRUBlockstore)(nil)
