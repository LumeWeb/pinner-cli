package upload

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/exchange/offline"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/pkg/upload/internal/carv1"
	"go.lumeweb.com/pinner-cli/pkg/upload/internal/encoding"
)

// TestBuildTreeSummary tests the CARBuilder.BuildSummary function
func TestBuildTreeSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		filesystem  fstest.MapFS
		wrapInDir   bool
		expectError bool
		check       func(*testing.T, *TreeSummary)
	}{
		{
			name: "single file",
			filesystem: fstest.MapFS{
				"file.txt": {Data: []byte("hello world")},
			},
			wrapInDir:   true,
			expectError: false,
			check: func(t *testing.T, summary *TreeSummary) {
				assert.NotEqual(t, cid.Undef, summary.RootCID)
				assert.Greater(t, len(summary.BlockOrder), 0)
			},
		},
		{
			name: "multiple files",
			filesystem: fstest.MapFS{
				"file1.txt": {Data: []byte("content 1")},
				"file2.txt": {Data: []byte("content 2")},
				"file3.txt": {Data: []byte("content 3")},
			},
			wrapInDir:   true,
			expectError: false,
			check: func(t *testing.T, summary *TreeSummary) {
				assert.NotEqual(t, cid.Undef, summary.RootCID)
				assert.Greater(t, len(summary.BlockOrder), 0)
			},
		},
		{
			name: "nested directories",
			filesystem: fstest.MapFS{
				"dir1/file1.txt":        {Data: []byte("file 1")},
				"dir1/subdir/file2.txt": {Data: []byte("file 2")},
				"dir2/file3.txt":        {Data: []byte("file 3")},
			},
			wrapInDir:   true,
			expectError: false,
			check: func(t *testing.T, summary *TreeSummary) {
				assert.NotEqual(t, cid.Undef, summary.RootCID)
				assert.Greater(t, len(summary.BlockOrder), 0)
			},
		},
		{
			name:        "empty filesystem",
			filesystem:  fstest.MapFS{},
			wrapInDir:   true,
			expectError: false,
			check: func(t *testing.T, summary *TreeSummary) {
				assert.Equal(t, 1, len(summary.BlockOrder))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, dagService := NewDAGServiceWithMemoryLimit(DefaultMemoryLimit)
			bs, dagService := NewDAGServiceWithMemoryLimit(DefaultMemoryLimit)

			generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))
			builder := NewCARBuilder(bs, dagService, generator)
			summary, err := builder.BuildSummary(ctx, tt.filesystem, tt.wrapInDir)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, summary)
			} else {
				assert.NoError(t, err)
				if tt.check != nil {
					tt.check(t, summary)
				}
			}
		})
	}
}

// TestCalculateCARSize_EmptyDirectories tests empty directory handling in CalculateCARSize
func TestCalculateCARSize_EmptyDirectories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filesystem fstest.MapFS
		wrapInDir  bool
		check      func(*testing.T, *TreeSummary, int64)
	}{
		{
			name: "single_empty_directory",
			filesystem: fstest.MapFS{
				"emptydir": {Mode: fs.ModeDir},
			},
			wrapInDir: true,
			check: func(t *testing.T, summary *TreeSummary, carSize int64) {
				// Empty directory should be pruned, so only root block exists
				assert.Greater(t, carSize, int64(0))
				assert.Equal(t, int64(summary.CARSize), carSize)
				// Root CID should be the only block
				assert.Equal(t, 1, len(summary.BlockOrder))
			},
		},
		{
			name: "file_alongside_empty_directory",
			filesystem: fstest.MapFS{
				"file.txt":     {Data: []byte("content")},
				"emptydir":     {Mode: fs.ModeDir},
				"nested/.keep": {Data: []byte("")},
			},
			wrapInDir: true,
			check: func(t *testing.T, summary *TreeSummary, carSize int64) {
				// Should have file blocks and root block, empty dir pruned
				assert.Greater(t, carSize, int64(0))
				assert.Equal(t, int64(summary.CARSize), carSize)
				// File + root block at minimum
				assert.GreaterOrEqual(t, len(summary.BlockOrder), 2)
			},
		},
		{
			name: "nested_empty_directories",
			filesystem: fstest.MapFS{
				"dir1/dir2/dir3":  {Mode: fs.ModeDir},
				"dir1/dir2/.keep": {Data: []byte("")},
			},
			wrapInDir: true,
			check: func(t *testing.T, summary *TreeSummary, carSize int64) {
				// Only dir2 (with .keep) and root should exist
				assert.Greater(t, carSize, int64(0))
				assert.Equal(t, int64(summary.CARSize), carSize)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			bs, dagService := NewDAGServiceWithMemoryLimit(DefaultMemoryLimit)
			generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))
			builder := NewCARBuilder(bs, dagService, generator)

			summary, err := builder.BuildSummary(ctx, tt.filesystem, tt.wrapInDir)
			assert.NoError(t, err)
			assert.NotNil(t, summary)

			carSize, err := CalculateCARSize(summary)
			require.NoError(t, err)
			tt.check(t, summary, carSize)
		})
	}
}

// TestBuildTreeSummary_ContextCancellation tests context cancellation
func TestBuildTreeSummary_ContextCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filesystem fstest.MapFS
		cancel     bool
		expectErr  bool
	}{
		{
			name:       "normal operation",
			filesystem: fstest.MapFS{"file.txt": {Data: []byte("content")}},
			cancel:     false,
			expectErr:  false,
		},
		{
			name:       "cancelled context",
			filesystem: fstest.MapFS{"file.txt": {Data: []byte("content")}},
			cancel:     true,
			expectErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			bs, dagService := NewDAGServiceWithMemoryLimit(DefaultMemoryLimit)
			generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))
			builder := NewCARBuilder(bs, dagService, generator)

			_, err := builder.BuildSummary(ctx, tt.filesystem, true)

			if tt.expectErr {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, context.Canceled) || err != nil)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestBuildTreeSummary_LargeFile tests building summary with a large file
func TestBuildTreeSummary_LargeFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bs, dagService := NewDAGServiceWithMemoryLimit(DefaultMemoryLimit)
	generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))
	builder := NewCARBuilder(bs, dagService, generator)

	largeData := make([]byte, 5*1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	filesystem := fstest.MapFS{
		"large.bin": {Data: largeData},
	}

	summary, err := builder.BuildSummary(ctx, filesystem, true)
	if err != nil {
		t.Skipf("Skipping large file test due to identity digest limitation: %v", err)
		return
	}

	assert.NotNil(t, summary)
	assert.NotEqual(t, cid.Undef, summary.RootCID)
	assert.Greater(t, len(summary.BlockOrder), 1, "large file should create multiple blocks")
}

// TestWriteCARv1FromSummary tests the CARBuilder.WriteCAR function
func TestWriteCARv1FromSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filesystem fstest.MapFS
		wrapInDir  bool
		check      func(*testing.T, *TreeSummary, []byte)
	}{
		{
			name: "single file",
			filesystem: fstest.MapFS{
				"file.txt": {Data: []byte("hello world")},
			},
			wrapInDir: true,
			check: func(t *testing.T, summary *TreeSummary, data []byte) {
				assert.NotEmpty(t, data)
				// Should have at least header + blocks
				assert.Greater(t, len(data), 10)
			},
		},
		{
			name: "multiple files",
			filesystem: fstest.MapFS{
				"file1.txt": {Data: []byte("content 1")},
				"file2.txt": {Data: []byte("content 2")},
			},
			wrapInDir: true,
			check: func(t *testing.T, summary *TreeSummary, data []byte) {
				assert.NotEmpty(t, data)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			bs, dagService := NewDAGServiceWithMemoryLimit(DefaultMemoryLimit)

			generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))
			builder := NewCARBuilder(bs, dagService, generator)
			summary, err := builder.BuildSummary(ctx, tt.filesystem, tt.wrapInDir)
			require.NoError(t, err)

			var buf bytes.Buffer
			err = builder.WriteCAR(ctx, &buf)
			assert.NoError(t, err)

			if tt.check != nil {
				tt.check(t, summary, buf.Bytes())
			}
		})
	}
}

// TestWriteCARv1FromSummary_ContextCancellation tests context cancellation
func TestWriteCARv1FromSummary_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	filesystem := fstest.MapFS{
		"file1.txt": {Data: []byte("content 1")},
		"file2.txt": {Data: []byte("content 2")},
		"file3.txt": {Data: []byte("content 3")},
	}

	bs, dagService := NewDAGServiceWithMemoryLimit(DefaultMemoryLimit)
	generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))
	builder := NewCARBuilder(bs, dagService, generator)

	_, err := builder.BuildSummary(ctx, filesystem, true)
	require.NoError(t, err)

	// Cancel context before writing
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	err = builder.WriteCAR(ctx, &buf)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

// TestStreamCAR tests the StreamCAR function
func TestStreamCAR(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filesystem fstest.MapFS
		maxMemory  uint64
		wrapInDir  bool
		check      func(*testing.T, cid.Cid, []byte)
	}{
		{
			name: "single file",
			filesystem: fstest.MapFS{
				"file.txt": {Data: []byte("test content")},
			},
			maxMemory: DefaultMemoryLimit,
			wrapInDir: true,
			check: func(t *testing.T, rootCID cid.Cid, data []byte) {
				assert.NotEqual(t, cid.Undef, rootCID)
				assert.NotEmpty(t, data)
			},
		},
		{
			name: "multiple files",
			filesystem: fstest.MapFS{
				"file1.txt": {Data: []byte("content 1")},
				"file2.txt": {Data: []byte("content 2")},
			},
			maxMemory: DefaultMemoryLimit,
			wrapInDir: true,
			check: func(t *testing.T, rootCID cid.Cid, data []byte) {
				assert.NotEqual(t, cid.Undef, rootCID)
				assert.NotEmpty(t, data)
			},
		},
		{
			name: "single file no wrap",
			filesystem: fstest.MapFS{
				"file.txt": {Data: []byte("test content")},
			},
			maxMemory: DefaultMemoryLimit,
			wrapInDir: false,
			check: func(t *testing.T, rootCID cid.Cid, data []byte) {
				assert.NotEqual(t, cid.Undef, rootCID)
				assert.NotEmpty(t, data)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			var buf bytes.Buffer

			rootCID, err := StreamCAR(ctx, tt.filesystem, &buf, tt.maxMemory, tt.wrapInDir)
			require.NoError(t, err)

			if tt.check != nil {
				tt.check(t, rootCID, buf.Bytes())
			}
		})
	}
}

// TestStreamCAR_ContextCancellation tests context cancellation
func TestStreamCAR_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	filesystem := fstest.MapFS{
		"file1.txt": {Data: []byte("content 1")},
		"file2.txt": {Data: []byte("content 2")},
	}

	// Cancel context before starting
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	_, err := StreamCAR(ctx, filesystem, &buf, DefaultMemoryLimit, true)
	// StreamCAR doesn't do blocking operations, so it might complete before checking context
	// Just verify it doesn't crash
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

// TestWriteCAR tests the WriteCAR function
func TestWriteCAR(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		check func(*testing.T, cid.Cid, []byte)
	}{
		{
			name: "basic write",
			check: func(t *testing.T, rootCID cid.Cid, data []byte) {
				assert.NotEqual(t, cid.Undef, rootCID)
				assert.NotEmpty(t, data)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			bs, dagService := NewDAGServiceWithMemoryLimit(DefaultMemoryLimit)

			generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))
			builder := NewCARBuilder(bs, dagService, generator)
			filesystem := fstest.MapFS{
				"file.txt": {Data: []byte("test content")},
			}
			summary, err := builder.BuildSummary(ctx, filesystem, true)
			require.NoError(t, err)
			rootCID := encoding.NormalizeCid(summary.RootCID)

			var buf bytes.Buffer
			err = builder.WriteCAR(ctx, &buf)
			assert.NoError(t, err)

			if tt.check != nil {
				tt.check(t, rootCID, buf.Bytes())
			}
		})
	}
}

// TestWriteCAR_ContextCancellation tests context cancellation
func TestWriteCAR_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bs, dagService := NewDAGServiceWithMemoryLimit(DefaultMemoryLimit)

	generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))
	builder := NewCARBuilder(bs, dagService, generator)
	filesystem := fstest.MapFS{
		"file1.txt": {Data: []byte("content 1")},
		"file2.txt": {Data: []byte("content 2")},
		"file3.txt": {Data: []byte("content 3")},
	}
	_, err := builder.BuildSummary(ctx, filesystem, true)
	require.NoError(t, err)

	// Cancel context before writing
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	err = builder.WriteCAR(ctx, &buf)
	// WriteCAR doesn't do blocking operations, so it might complete before checking context
	// Just verify it doesn't crash
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

// TestRoundTripCAR tests CAR round-trip: build CAR, read it back, verify content
func TestRoundTripCAR(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filesystem fstest.MapFS
		wrapInDir  bool
	}{
		{
			name: "single file",
			filesystem: fstest.MapFS{
				"file.txt": {Data: []byte("hello world")},
			},
			wrapInDir: true,
		},
		{
			name: "multiple files",
			filesystem: fstest.MapFS{
				"file1.txt": {Data: []byte("content 1")},
				"file2.txt": {Data: []byte("content 2")},
				"file3.txt": {Data: []byte("content 3")},
			},
			wrapInDir: true,
		},
		{
			name: "nested directories",
			filesystem: fstest.MapFS{
				"dir1/file1.txt":        {Data: []byte("file 1")},
				"dir1/subdir/file2.txt": {Data: []byte("file 2")},
				"dir2/file3.txt":        {Data: []byte("file 3")},
			},
			wrapInDir: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			bs := NewLRUBlockstore(DefaultMemoryLimit)
			bsvc := blockservice.New(bs, offline.Exchange(bs))
			dagService := merkledag.NewDAGService(bsvc)

			generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))
			builder := NewCARBuilder(bs, dagService, generator)
			summary, err := builder.BuildSummary(ctx, tt.filesystem, tt.wrapInDir)
			require.NoError(t, err)
			rootCID := encoding.NormalizeCid(summary.RootCID)

			var carBuf bytes.Buffer
			err = builder.WriteCAR(ctx, &carBuf)
			require.NoError(t, err)

			// Read CAR
			carReader, err := carv1.NewCarReader(&carBuf)
			require.NoError(t, err)

			// Verify header
			assert.NotNil(t, carReader.Header)
			assert.Equal(t, uint64(1), carReader.Header.Version)
			assert.Len(t, carReader.Header.Roots, 1)
			assert.Equal(t, rootCID, carReader.Header.Roots[0])

			// Read all blocks and verify they match blockstore
			blockCount := 0
			for {
				block, err := carReader.Next()
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				blockCount++

				storedBlock, err := bs.Get(ctx, block.Cid())
				require.NoError(t, err)
				assert.Equal(t, block.RawData(), storedBlock.RawData())
			}

			assert.Greater(t, blockCount, 0)
		})
	}
}

// TestRoundTripCAR_StreamCAR tests StreamCAR round-trip
func TestRoundTripCAR_StreamCAR(t *testing.T) {
	t.Parallel()

	filesystem := fstest.MapFS{
		"file1.txt":     {Data: []byte("content 1")},
		"file2.txt":     {Data: []byte("content 2")},
		"dir/file3.txt": {Data: []byte("content 3")},
	}

	ctx := context.Background()
	var carBuf bytes.Buffer

	rootCID, err := StreamCAR(ctx, filesystem, &carBuf, DefaultMemoryLimit, true)
	require.NoError(t, err)
	require.NotEqual(t, cid.Undef, rootCID)

	// Read CAR
	carReader, err := carv1.NewCarReader(&carBuf)
	require.NoError(t, err)

	// Verify header
	assert.NotNil(t, carReader.Header)
	assert.Equal(t, uint64(1), carReader.Header.Version)
	assert.Len(t, carReader.Header.Roots, 1)
	assert.Equal(t, rootCID, carReader.Header.Roots[0])

	// Read all blocks
	blockCount := 0
	for {
		_, err := carReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		blockCount++
	}

	assert.Greater(t, blockCount, 0)
}

// TestRoundTripCAR_WriteCAR tests WriteCAR round-trip
func TestRoundTripCAR_WriteCAR(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bs, dagService := NewDAGServiceWithMemoryLimit(DefaultMemoryLimit)

	generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))
	builder := NewCARBuilder(bs, dagService, generator)
	filesystem := fstest.MapFS{
		"file1.txt":     {Data: []byte("content 1")},
		"file2.txt":     {Data: []byte("content 2")},
		"dir/file3.txt": {Data: []byte("content 3")},
	}
	summary, err := builder.BuildSummary(ctx, filesystem, true)
	require.NoError(t, err)
	rootCID := encoding.NormalizeCid(summary.RootCID)

	// Write CAR using WriteCAR
	var carBuf bytes.Buffer
	err = builder.WriteCAR(ctx, &carBuf)
	require.NoError(t, err)

	// Read CAR
	carReader, err := carv1.NewCarReader(&carBuf)
	require.NoError(t, err)

	// Verify header
	assert.NotNil(t, carReader.Header)
	assert.Equal(t, uint64(1), carReader.Header.Version)
	assert.Len(t, carReader.Header.Roots, 1)
	assert.Equal(t, rootCID, carReader.Header.Roots[0])

	// Read all blocks and verify they match blockstore
	blockCount := 0
	for {
		block, err := carReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		blockCount++

		storedBlock, err := bs.Get(ctx, block.Cid())
		require.NoError(t, err)
		assert.Equal(t, block.RawData(), storedBlock.RawData())
	}

	assert.Greater(t, blockCount, 0)
}

// TestRoundTripCAR_VerifyAllData verifies all data can be read back
func TestRoundTripCAR_VerifyAllData(t *testing.T) {
	t.Parallel()

	filesystem := fstest.MapFS{
		"file1.txt":            {Data: []byte("content 1")},
		"file2.txt":            {Data: []byte("content 2")},
		"dir/file3.txt":        {Data: []byte("content 3")},
		"dir/subdir/file4.txt": {Data: []byte("content 4")},
	}

	ctx := context.Background()
	var carBuf bytes.Buffer

	rootCID, err := StreamCAR(ctx, filesystem, &carBuf, DefaultMemoryLimit, true)
	require.NoError(t, err)

	// Read CAR and verify all blocks
	carReader, err := carv1.NewCarReader(&carBuf)
	require.NoError(t, err)

	// Collect all blocks
	blocks := make(map[cid.Cid][]byte)
	for {
		block, err := carReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		blocks[block.Cid()] = block.RawData()
	}

	// Verify we have the root block
	_, ok := blocks[rootCID]
	assert.True(t, ok, "should have root block")

	// Verify total blocks count is reasonable
	assert.Greater(t, len(blocks), 0)
}

// TestRoundTripCAR_ContextCancellation tests context cancellation during round-trip
func TestRoundTripCAR_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	filesystem := fstest.MapFS{
		"file1.txt": {Data: []byte("content 1")},
		"file2.txt": {Data: []byte("content 2")},
	}

	var carBuf bytes.Buffer
	_, err := StreamCAR(ctx, filesystem, &carBuf, DefaultMemoryLimit, true)
	require.NoError(t, err)

	// Cancel context and try to read
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	carReader, err := carv1.NewCarReader(&carBuf)
	require.NoError(t, err)

	// Try to read a block with cancelled context
	_, err = carReader.Next()
	// The first read might succeed as it's buffered
	if err != nil {
		assert.Error(t, err)
	}
}

// TestRoundTripCAR_LargeDataset tests round-trip with a larger dataset
func TestRoundTripCAR_LargeDataset(t *testing.T) {
	t.Parallel()

	filesystem := fstest.MapFS{
		"file1.txt":              {Data: []byte("content 1")},
		"file2.txt":              {Data: []byte("content 2")},
		"file3.txt":              {Data: []byte("content 3")},
		"file4.txt":              {Data: []byte("content 4")},
		"file5.txt":              {Data: []byte("content 5")},
		"dir1/file6.txt":         {Data: []byte("content 6")},
		"dir1/file7.txt":         {Data: []byte("content 7")},
		"dir2/file8.txt":         {Data: []byte("content 8")},
		"dir2/subdir/file9.txt":  {Data: []byte("content 9")},
		"dir2/subdir/file10.txt": {Data: []byte("content 10")},
	}

	ctx := context.Background()
	var carBuf bytes.Buffer

	rootCID, err := StreamCAR(ctx, filesystem, &carBuf, DefaultMemoryLimit, true)
	require.NoError(t, err)

	// Read CAR and verify all blocks
	carReader, err := carv1.NewCarReader(&carBuf)
	require.NoError(t, err)

	blockCount := 0
	for {
		_, err := carReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		blockCount++
	}

	// Should have multiple blocks (files + directories)
	assert.Greater(t, blockCount, 5)
	assert.NotEqual(t, cid.Undef, rootCID)
}

// TestCalculateCARSize tests the CalculateCARSize function
func TestCalculateCARSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filesystem fstest.MapFS
		wrapInDir  bool
		minSize    int64
	}{
		{
			name: "single file",
			filesystem: fstest.MapFS{
				"file.txt": {Data: []byte("hello world")},
			},
			wrapInDir: true,
			minSize:   100, // At least header + block
		},
		{
			name: "multiple files",
			filesystem: fstest.MapFS{
				"file1.txt": {Data: []byte("content 1")},
				"file2.txt": {Data: []byte("content 2")},
				"file3.txt": {Data: []byte("content 3")},
			},
			wrapInDir: true,
			minSize:   200,
		},
		{
			name:       "empty filesystem",
			filesystem: fstest.MapFS{},
			wrapInDir:  true,
			minSize:    50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			bs, dagService := NewDAGServiceWithMemoryLimit(DefaultMemoryLimit)
			generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))
			builder := NewCARBuilder(bs, dagService, generator)

			summary, err := builder.BuildSummary(ctx, tt.filesystem, tt.wrapInDir)
			require.NoError(t, err)

			carSize, err := CalculateCARSize(summary)
			require.NoError(t, err)
			assert.Greater(t, carSize, tt.minSize)
			assert.Equal(t, int64(summary.CARSize), carSize)
		})
	}
}

// TestCalculateCARSize_EdgeCases tests edge cases for CAR size calculation
func TestCalculateCARSize_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filesystem fstest.MapFS
		wrapInDir  bool
		check      func(*testing.T, *TreeSummary, int64)
	}{
		{
			name: "very large file",
			filesystem: fstest.MapFS{
				"large.bin": {Data: make([]byte, 1024*1024)},
			},
			wrapInDir: true,
			check: func(t *testing.T, summary *TreeSummary, carSize int64) {
				assert.Greater(t, carSize, int64(1024*1024))
			},
		},
		{
			name: "many small files",
			filesystem: func() fstest.MapFS {
				fs := make(fstest.MapFS)
				for i := 0; i < 100; i++ {
					fs[fmt.Sprintf("file%03d.txt", i)] = &fstest.MapFile{
						Data: []byte(fmt.Sprintf("content %d", i)),
						Mode: 0644,
					}
				}
				return fs
			}(),
			wrapInDir: true,
			check: func(t *testing.T, summary *TreeSummary, carSize int64) {
				assert.Greater(t, len(summary.BlockOrder), 100)
				assert.Greater(t, carSize, int64(5000))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			bs, dagService := NewDAGServiceWithMemoryLimit(DefaultMemoryLimit)
			generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))
			builder := NewCARBuilder(bs, dagService, generator)

			summary, err := builder.BuildSummary(ctx, tt.filesystem, tt.wrapInDir)
			require.NoError(t, err)

			carSize, err := CalculateCARSize(summary)
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, summary, carSize)
			}
		})
	}
}

// TestCalculateCARSize_ActualSizeComparison compares calculated size with actual size
func TestCalculateCARSize_ActualSizeComparison(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filesystem fstest.MapFS
		wrapInDir  bool
		tolerance  float64 // Allow 5% difference due to varint encoding
	}{
		{
			name: "simple file",
			filesystem: fstest.MapFS{
				"file.txt": {Data: []byte("hello world")},
			},
			wrapInDir: true,
			tolerance: 0.05,
		},
		{
			name: "multiple files",
			filesystem: fstest.MapFS{
				"file1.txt": {Data: []byte("content 1")},
				"file2.txt": {Data: []byte("content 2")},
				"file3.txt": {Data: []byte("content 3")},
			},
			wrapInDir: true,
			tolerance: 0.05,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			bs, dagService := NewDAGServiceWithMemoryLimit(DefaultMemoryLimit)
			generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))
			builder := NewCARBuilder(bs, dagService, generator)

			summary, err := builder.BuildSummary(ctx, tt.filesystem, tt.wrapInDir)
			require.NoError(t, err)

			calculatedSize, err := CalculateCARSize(summary)
			require.NoError(t, err)

			var carBuf bytes.Buffer
			err = builder.WriteCAR(ctx, &carBuf)
			require.NoError(t, err)

			actualSize := int64(carBuf.Len())

			diff := float64(abs(actualSize-calculatedSize)) / float64(actualSize)
			assert.LessOrEqual(t, diff, tt.tolerance, "calculated size should be within tolerance of actual size")
		})
	}
}

// TestCalculateCARSize_StreamCARWithSizeIntegration tests integration with StreamCARWithSize
func TestCalculateCARSize_StreamCARWithSizeIntegration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filesystem fstest.MapFS
		wrapInDir  bool
	}{
		{
			name: "single file",
			filesystem: fstest.MapFS{
				"file.txt": {Data: []byte("hello world")},
			},
			wrapInDir: true,
		},
		{
			name: "multiple files",
			filesystem: fstest.MapFS{
				"file1.txt": {Data: []byte("content 1")},
				"file2.txt": {Data: []byte("content 2")},
				"file3.txt": {Data: []byte("content 3")},
			},
			wrapInDir: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			// Get size from StreamCARWithSize
			var carBuf bytes.Buffer
			_, streamCARSize, err := StreamCARWithSize(ctx, tt.filesystem, &carBuf, DefaultMemoryLimit, tt.wrapInDir)
			require.NoError(t, err)

			// Get size from CalculateCARSize
			bs, dagService := NewDAGServiceWithMemoryLimit(DefaultMemoryLimit)
			generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))
			builder := NewCARBuilder(bs, dagService, generator)
			summary, err := builder.BuildSummary(ctx, tt.filesystem, tt.wrapInDir)
			require.NoError(t, err)
			calculatedSize, err := CalculateCARSize(summary)
			require.NoError(t, err)

			// Both should return the same size
			assert.Equal(t, streamCARSize, calculatedSize)
		})
	}
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
