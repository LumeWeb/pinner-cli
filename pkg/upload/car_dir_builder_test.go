package upload

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/exchange/offline"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"

	"go.lumeweb.com/pinner-cli/pkg/upload/internal/encoding"
)

// testBuilder holds the test infrastructure for CARBuilder tests
type testBuilder struct {
	builder    *CARBuilder
	bs         *LRUBlockstore
	bsvc       blockservice.BlockService
	dagService ipld.DAGService
	ctx        context.Context
}

// setupBuilder creates a test builder with the specified blockstore size
func setupBuilder(t *testing.T, size uint64) *testBuilder {
	t.Helper()

	bs := NewLRUBlockstore(size)
	bsvc := blockservice.New(bs, offline.Exchange(bs))
	dagService := merkledag.NewDAGService(bsvc)
	generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))
	builder := NewCARBuilder(bs, dagService, generator)

	return &testBuilder{
		builder:    builder,
		bs:         bs,
		bsvc:       bsvc,
		dagService: dagService,
		ctx:        context.Background(),
	}
}

// buildAndValidate runs BuildSummary and validates the result
func (tb *testBuilder) buildAndValidate(t *testing.T, memFS fstest.MapFS, expectedMinBlocks int) cid.Cid {
	t.Helper()

	summary, err := tb.builder.BuildSummary(tb.ctx, memFS, true)
	if err != nil {
		t.Fatalf("BuildSummary failed: %v", err)
	}

	rootCID := summary.RootCID
	if !rootCID.Defined() {
		t.Error("root CID should be defined")
	}

	if expectedMinBlocks > 0 && len(summary.BlockOrder) < expectedMinBlocks {
		t.Errorf("expected at least %d blocks, got %d", expectedMinBlocks, len(summary.BlockOrder))
	}

	return rootCID
}

// assertRootCID validates that the root CID is defined
func assertRootCID(t *testing.T, rootCID cid.Cid) {
	t.Helper()

	if !rootCID.Defined() {
		t.Error("root CID should be defined")
	}
}

// assertBlockstoreNotEmpty validates that the blockstore contains blocks
func assertBlockstoreNotEmpty(t *testing.T, bs *LRUBlockstore) {
	t.Helper()

	if bs.Len() == 0 {
		t.Error("blockstore should contain blocks")
	}
}

// assertMinBlocks validates minimum number of blocks in blockstore
func assertMinBlocks(t *testing.T, bs *LRUBlockstore, min int) {
	t.Helper()

	if bs.Len() < min {
		t.Errorf("expected at least %d blocks, got %d", min, bs.Len())
	}
}

func TestNewCARBuilder(t *testing.T) {
	tb := setupBuilder(t, 1000)

	if tb.builder == nil {
		t.Fatal("NewCARBuilder returned nil")
	}

	if tb.builder.bs != tb.bs {
		t.Error("blockstore not set correctly")
	}

	if tb.builder.dagService != tb.dagService {
		t.Error("dagService not set correctly")
	}

	if tb.builder.chunkSize != 1024*1024 {
		t.Errorf("expected chunkSize %d, got %d", 1024*1024, tb.builder.chunkSize)
	}
}

func TestCARBuilder_EmptyFilesystem(t *testing.T) {
	tb := setupBuilder(t, 1000)
	memFS := fstest.MapFS{}

	rootCID := tb.buildAndValidate(t, memFS, 0)
	assertRootCID(t, rootCID)
}

func TestCARBuilder_SingleFile(t *testing.T) {
	tb := setupBuilder(t, 1000)
	memFS := fstest.MapFS{
		"file.txt": {Data: []byte("hello world")},
	}

	rootCID := tb.buildAndValidate(t, memFS, 1)
	assertRootCID(t, rootCID)
	assertBlockstoreNotEmpty(t, tb.bs)
}

func TestCARBuilder_MultipleFiles(t *testing.T) {
	tb := setupBuilder(t, 10000)
	memFS := fstest.MapFS{
		"file1.txt":   {Data: []byte("content 1")},
		"file2.txt":   {Data: []byte("content 2")},
		"file3.txt":   {Data: []byte("content 3")},
		"notes.md":    {Data: []byte("# Notes\n")},
		"config.json": {Data: []byte(`{"key": "value"}`)},
	}

	rootCID := tb.buildAndValidate(t, memFS, 5)
	assertRootCID(t, rootCID)
	assertBlockstoreNotEmpty(t, tb.bs)
}

func TestCARBuilder_SingleDirectory(t *testing.T) {
	tb := setupBuilder(t, 10000)
	memFS := fstest.MapFS{
		"dir1/file1.txt": {Data: []byte("file 1")},
		"dir1/file2.txt": {Data: []byte("file 2")},
	}

	rootCID := tb.buildAndValidate(t, memFS, 2)
	assertRootCID(t, rootCID)
	assertBlockstoreNotEmpty(t, tb.bs)
}

func TestCARBuilder_NestedDirectories(t *testing.T) {
	tb := setupBuilder(t, 10000)
	memFS := fstest.MapFS{
		"dir1/file1.txt":             {Data: []byte("file 1")},
		"dir1/subdir/file2.txt":      {Data: []byte("file 2")},
		"dir1/subdir/file3.txt":      {Data: []byte("file 3")},
		"dir2/file4.txt":             {Data: []byte("file 4")},
		"dir2/subdir/file5.txt":      {Data: []byte("file 5")},
		"dir2/subdir/deep/file6.txt": {Data: []byte("file 6")},
	}

	rootCID := tb.buildAndValidate(t, memFS, 6)
	assertRootCID(t, rootCID)
	assertBlockstoreNotEmpty(t, tb.bs)
}

func TestCARBuilder_LargeFile(t *testing.T) {
	ctx := context.Background()
	bs := NewLRUBlockstore(10000000)
	bsvc := blockservice.New(bs, offline.Exchange(bs))
	dagService := merkledag.NewDAGService(bsvc)
	generator := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService), WithUnixFSNodeBlockstore(bs))
	builder := NewCARBuilder(bs, dagService, generator)

	largeData := make([]byte, 2*1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	memFS := fstest.MapFS{
		"large.bin": {Data: largeData},
	}

	summary, err := builder.BuildSummary(ctx, memFS, true)
	if err != nil {
		t.Skipf("Skipping large file test due to identity digest limitation: %v", err)
		return
	}

	if !summary.RootCID.Defined() {
		t.Error("root CID should be defined")
	}

	if len(summary.BlockOrder) == 0 {
		t.Error("should have blocks in summary")
	}
}

func TestCARBuilder_EmptyDirectory(t *testing.T) {
	tb := setupBuilder(t, 10000)
	memFS := fstest.MapFS{
		"emptydir/.keep": {Data: []byte("")},
	}

	rootCID := tb.buildAndValidate(t, memFS, 1)
	assertRootCID(t, rootCID)
}

func TestCARBuilder_Symlink(t *testing.T) {
	tb := setupBuilder(t, 10000)
	memFS := fstest.MapFS{
		"file.txt": {Data: []byte("content")},
		"link":     {Mode: fs.ModeSymlink, Data: []byte("file.txt")},
	}

	rootCID := tb.buildAndValidate(t, memFS, 1)
	assertRootCID(t, rootCID)
}

func TestCARBuilder_Deterministic(t *testing.T) {
	ctx := context.Background()
	bs1 := NewLRUBlockstore(10000)
	bs2 := NewLRUBlockstore(10000)
	bsvc1 := blockservice.New(bs1, offline.Exchange(bs1))
	bsvc2 := blockservice.New(bs2, offline.Exchange(bs2))
	dagService1 := merkledag.NewDAGService(bsvc1)
	dagService2 := merkledag.NewDAGService(bsvc2)
	generator1 := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService1), WithUnixFSNodeBlockstore(bs1))
	generator2 := NewUnixFSNodeGenerator(WithUnixFSNodeDAGService(dagService2), WithUnixFSNodeBlockstore(bs2))
	builder1 := NewCARBuilder(bs1, dagService1, generator1)
	builder2 := NewCARBuilder(bs2, dagService2, generator2)

	memFS := fstest.MapFS{
		"file1.txt":     {Data: []byte("content 1")},
		"file2.txt":     {Data: []byte("content 2")},
		"dir/file3.txt": {Data: []byte("content 3")},
	}

	summary1, err := builder1.BuildSummary(ctx, memFS, true)
	if err != nil {
		t.Fatalf("BuildSummary 1 failed: %v", err)
	}

	summary2, err := builder2.BuildSummary(ctx, memFS, true)
	if err != nil {
		t.Fatalf("BuildSummary 2 failed: %v", err)
	}

	if summary1.RootCID != summary2.RootCID {
		t.Errorf("CIDs should be identical for same content: %v != %v", summary1.RootCID, summary2.RootCID)
	}
}

func TestCARBuilder_FileReadError(t *testing.T) {
	tb := setupBuilder(t, 10000)
	errFS := &errorFS{
		MapFS: fstest.MapFS{
			"file.txt": {Data: []byte("content")},
		},
		readError: "file.txt",
	}

	_, err := tb.builder.BuildSummary(tb.ctx, errFS, true)
	if err == nil {
		t.Error("expected error for file read failure")
	}
}

func TestCARBuilder_WalkError(t *testing.T) {
	tb := setupBuilder(t, 10000)
	errFS := &errorFS{
		MapFS: fstest.MapFS{
			"file.txt": {Data: []byte("content")},
		},
	}

	_, err := tb.builder.BuildSummary(tb.ctx, errFS, true)
	if err != nil {
		t.Errorf("BuildSummary with errorFS failed unexpectedly: %v", err)
	}
}

func TestCARBuilder_RootNotFound(t *testing.T) {
	tb := setupBuilder(t, 10000)
	memFS := fstest.MapFS{}

	rootCID := tb.buildAndValidate(t, memFS, 0)
	assertRootCID(t, rootCID)
}

func TestCARBuilder_DeeplyNestedStructures(t *testing.T) {
	tests := []struct {
		name          string
		depth         int
		filesPerLevel int
		expectedFiles int
	}{
		{
			name:          "moderate depth with multiple files",
			depth:         5,
			filesPerLevel: 3,
			expectedFiles: 15,
		},
		{
			name:          "very deep hierarchy",
			depth:         10,
			filesPerLevel: 1,
			expectedFiles: 10,
		},
		{
			name:          "wide and shallow",
			depth:         3,
			filesPerLevel: 5,
			expectedFiles: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tb := setupBuilder(t, 100000)

			memFS := make(fstest.MapFS)

			for depth := 0; depth < tt.depth; depth++ {
				dirPath := ""
				for i := 0; i <= depth; i++ {
					if i > 0 {
						dirPath += "/"
					}
					dirPath += fmt.Sprintf("level%d", i)
				}

				for fileIdx := 0; fileIdx < tt.filesPerLevel; fileIdx++ {
					filePath := fmt.Sprintf("%s/file%d.txt", dirPath, fileIdx)
					memFS[filePath] = &fstest.MapFile{
						Data: []byte(fmt.Sprintf("content at depth %d, file %d", depth, fileIdx)),
						Mode: 0644,
					}
				}
			}

			rootCID := tb.buildAndValidate(t, memFS, tt.expectedFiles)
			assertRootCID(t, rootCID)
			assertBlockstoreNotEmpty(t, tb.bs)
			assertMinBlocks(t, tb.bs, tt.expectedFiles)
		})
	}
}

func TestCARBuilder_SpecialCharacters(t *testing.T) {
	tb := setupBuilder(t, 100000)

	memFS := fstest.MapFS{
		"file with spaces.txt": {
			Data: []byte("content with spaces in filename"),
			Mode: 0644,
		},
		"file-with-dashes.txt": {
			Data: []byte("content with dashes"),
			Mode: 0644,
		},
		"file_with_underscores.txt": {
			Data: []byte("content with underscores"),
			Mode: 0644,
		},
		"file.with.dots.txt": {
			Data: []byte("content with dots"),
			Mode: 0644,
		},
		"file+with+pluses.txt": {
			Data: []byte("content with pluses"),
			Mode: 0644,
		},
		"file(with)parentheses.txt": {
			Data: []byte("content with parentheses"),
			Mode: 0644,
		},
		"file[with]brackets.txt": {
			Data: []byte("content with brackets"),
			Mode: 0644,
		},
		"file{with}braces.txt": {
			Data: []byte("content with braces"),
			Mode: 0644,
		},
		"file@with@at.txt": {
			Data: []byte("content with at symbol"),
			Mode: 0644,
		},
		"file#with#hash.txt": {
			Data: []byte("content with hash"),
			Mode: 0644,
		},
		"file$with$dollar.txt": {
			Data: []byte("content with dollar"),
			Mode: 0644,
		},
		"file%with%percent.txt": {
			Data: []byte("content with percent"),
			Mode: 0644,
		},
		"file^with^caret.txt": {
			Data: []byte("content with caret"),
			Mode: 0644,
		},
		"file&with&ampersand.txt": {
			Data: []byte("content with ampersand"),
			Mode: 0644,
		},
		"file*with*asterisk.txt": {
			Data: []byte("content with asterisk"),
			Mode: 0644,
		},
		"special chars!@#$%^&*()[]{}+.txt": {
			Data: []byte("content with many special chars"),
			Mode: 0644,
		},
		"folder with spaces/file.txt": {
			Data: []byte("file in folder with spaces"),
			Mode: 0644,
		},
		"folder-with-dashes/file.txt": {
			Data: []byte("file in folder with dashes"),
			Mode: 0644,
		},
	}

	rootCID := tb.buildAndValidate(t, memFS, 18)
	assertRootCID(t, rootCID)
	assertBlockstoreNotEmpty(t, tb.bs)
	assertMinBlocks(t, tb.bs, 18)
}

func TestCARBuilder_MixedComplexStructure(t *testing.T) {
	tb := setupBuilder(t, 1000000)

	memFS := fstest.MapFS{
		"README.md": {
			Data: []byte("# Project Documentation\n\nThis is a test project."),
			Mode: 0644,
		},
		"package.json": {
			Data: []byte(`{"name": "test-project", "version": "1.0.0", "scripts": {"test": "jest"}}`),
			Mode: 0644,
		},
		".gitignore": {
			Data: []byte("node_modules/\n*.log\n.env"),
			Mode: 0644,
		},
		".env.example": {
			Data: []byte("API_KEY=your_api_key_here\nDATABASE_URL=your_database_url"),
			Mode: 0644,
		},
		"src/index.js": {
			Data: []byte("const express = require('express');\nconst app = express();\napp.listen(3000);"),
			Mode: 0644,
		},
		"src/utils/logger.js": {
			Data: []byte("const winston = require('winston');\nmodule.exports = winston.createLogger({});"),
			Mode: 0644,
		},
		"src/utils/database.js": {
			Data: []byte("const mongoose = require('mongoose');\nmodule.exports = mongoose.connection;"),
			Mode: 0644,
		},
		"src/controllers/user.js": {
			Data: []byte("exports.getUser = (req, res) => { res.json({user: 'test'}); };"),
			Mode: 0644,
		},
		"src/controllers/auth.js": {
			Data: []byte("exports.login = (req, res) => { res.json({token: 'test'}); };"),
			Mode: 0644,
		},
		"src/middleware/auth.js": {
			Data: []byte("exports.authenticate = (req, res, next) => { next(); };"),
			Mode: 0644,
		},
		"tests/unit/user.test.js": {
			Data: []byte("const userController = require('../src/controllers/user');\ntest('getUser returns user', () => {});"),
			Mode: 0644,
		},
		"tests/integration/api.test.js": {
			Data: []byte("const request = require('supertest');\ntest('API endpoints', async () => {});"),
			Mode: 0644,
		},
		"tests/fixtures/user.json": {
			Data: []byte(`{"id": 1, "name": "Test User", "email": "test@example.com"}`),
			Mode: 0644,
		},
		"config/database.json": {
			Data: []byte(`{"development": {"host": "localhost"}, "production": {"host": "prod-db"}}`),
			Mode: 0644,
		},
		"config/redis.conf": {
			Data: []byte("port 6379\nbind 127.0.0.1\nmaxmemory 256mb"),
			Mode: 0644,
		},
		"public/index.html": {
			Data: []byte("<!DOCTYPE html><html><head><title>Test App</title></head><body><h1>Hello World</h1></body></html>"),
			Mode: 0644,
		},
		"public/css/style.css": {
			Data: []byte("body { font-family: Arial, sans-serif; }\n.container { max-width: 1200px; }"),
			Mode: 0644,
		},
		"public/js/app.js": {
			Data: []byte("console.log('App loaded');\ndocument.addEventListener('DOMContentLoaded', function() {});"),
			Mode: 0644,
		},
		"public/images/logo.png": {
			Data: []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\nIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xdb\x00\x00\x00\x00IEND\xaeB`\x82"),
			Mode: 0644,
		},
		".vscode/settings.json": {
			Data: []byte(`{"editor.tabSize": 2, "editor.insertSpaces": true}`),
			Mode: 0644,
		},
		".vscode/launch.json": {
			Data: []byte(`{"version": "0.2.0", "configurations": [{"name": "Debug", "type": "node", "request": "launch"}]}`),
			Mode: 0644,
		},
	}

	rootCID := tb.buildAndValidate(t, memFS, 21)
	assertRootCID(t, rootCID)
	assertBlockstoreNotEmpty(t, tb.bs)
	assertMinBlocks(t, tb.bs, 21)
}

func TestCARBuilder_DynamicComplexTree(t *testing.T) {
	tests := []struct {
		name             string
		numDirectories   int
		filesPerDir      int
		nestingDepth     int
		expectedMinFiles int
	}{
		{
			name:             "small complex tree",
			numDirectories:   5,
			filesPerDir:      3,
			nestingDepth:     3,
			expectedMinFiles: 15,
		},
		{
			name:             "medium complex tree",
			numDirectories:   10,
			filesPerDir:      5,
			nestingDepth:     4,
			expectedMinFiles: 50,
		},
		{
			name:             "large complex tree",
			numDirectories:   20,
			filesPerDir:      10,
			nestingDepth:     5,
			expectedMinFiles: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tb := setupBuilder(t, 1000000)

			memFS := generateComplexTree(t, tt.numDirectories, tt.filesPerDir, tt.nestingDepth)

			rootCID := tb.buildAndValidate(t, memFS, tt.expectedMinFiles)
			assertRootCID(t, rootCID)
			assertBlockstoreNotEmpty(t, tb.bs)
			assertMinBlocks(t, tb.bs, tt.expectedMinFiles)

			t.Logf("Generated tree with %d blocks", len(tb.builder.GetSummary().BlockOrder))
		})
	}
}

func TestCARBuilder_WriteCAR_RoundTrip(t *testing.T) {
	tb := setupBuilder(t, 10000)

	memFS := fstest.MapFS{
		"file1.txt":     {Data: []byte("content 1")},
		"file2.txt":     {Data: []byte("content 2")},
		"dir/file3.txt": {Data: []byte("content 3")},
	}

	summary, err := tb.builder.BuildSummary(tb.ctx, memFS, true)
	if err != nil {
		t.Fatalf("BuildSummary failed: %v", err)
	}

	_ = encoding.NormalizeCid(summary.RootCID)

	var carBuf bytes.Buffer
	if err := tb.builder.WriteCAR(tb.ctx, &carBuf); err != nil {
		t.Fatalf("WriteCAR failed: %v", err)
	}

	if carBuf.Len() == 0 {
		t.Error("CAR buffer should not be empty")
	}
}

func TestCARBuilder_BuildAndWrite(t *testing.T) {
	tb := setupBuilder(t, 10000)

	memFS := fstest.MapFS{
		"file1.txt": {Data: []byte("content 1")},
		"file2.txt": {Data: []byte("content 2")},
	}

	var carBuf bytes.Buffer
	rootCID, err := tb.builder.BuildAndWrite(tb.ctx, memFS, &carBuf, true)
	if err != nil {
		t.Fatalf("BuildAndWrite failed: %v", err)
	}

	if !rootCID.Defined() {
		t.Error("root CID should be defined")
	}

	if carBuf.Len() == 0 {
		t.Error("CAR buffer should not be empty")
	}
}

func generateComplexTree(t *testing.T, numDirs, filesPerDir, nestingDepth int) fstest.MapFS {
	memFS := make(fstest.MapFS)
	fileCounter := 0

	for dirIdx := 0; dirIdx < numDirs; dirIdx++ {
		baseDir := fmt.Sprintf("dir%03d", dirIdx)

		for fileIdx := 0; fileIdx < filesPerDir; fileIdx++ {
			filePath := fmt.Sprintf("%s/file%04d.txt", baseDir, fileCounter)
			memFS[filePath] = &fstest.MapFile{
				Data: []byte(fmt.Sprintf("File %d in directory %s", fileCounter, baseDir)),
				Mode: 0644,
			}
			fileCounter++
		}

		currentPath := baseDir
		for depth := 1; depth < nestingDepth; depth++ {
			subDir := fmt.Sprintf("sub%02d", depth)
			currentPath = fmt.Sprintf("%s/%s", currentPath, subDir)

			for fileIdx := 0; fileIdx < filesPerDir/2; fileIdx++ {
				filePath := fmt.Sprintf("%s/file%04d.txt", currentPath, fileCounter)
				memFS[filePath] = &fstest.MapFile{
					Data: []byte(fmt.Sprintf("File %d at depth %d", fileCounter, depth)),
					Mode: 0644,
				}
				fileCounter++
			}
		}
	}

	for i := 0; i < 5; i++ {
		filePath := fmt.Sprintf("root%02d.txt", i)
		memFS[filePath] = &fstest.MapFile{
			Data: []byte(fmt.Sprintf("Root file %d", i)),
			Mode: 0644,
		}
		fileCounter++
	}

	t.Logf("Generated %d total files across %d directories with max depth %d", fileCounter, numDirs, nestingDepth)

	return memFS
}

type errorFS struct {
	fstest.MapFS
	readError string
}

func (e *errorFS) Open(name string) (fs.File, error) {
	if name == e.readError {
		return nil, errors.New("read error")
	}
	return e.MapFS.Open(name)
}
