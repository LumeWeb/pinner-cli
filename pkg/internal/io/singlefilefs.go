package io

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"
)

// SingleFileFS is a filesystem implementation for a single file at the root.
// It holds an os.File handle instead of buffering the entire file in memory.
type SingleFileFS struct {
	file     *os.File
	filePath string
	name     string
	fileInfo fs.FileInfo
}

// NewSingleFileFS creates a SingleFileFS from a file path.
// The file will be mapped to the root of the filesystem with the given name.
func NewSingleFileFS(filePath, name string) (*SingleFileFS, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}

	fileInfo, err := file.Stat()
	if err != nil {
		closeErr := file.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("failed to stat file: %w, and failed to close file: %v", err, closeErr)
		}
		return nil, err
	}

	return &SingleFileFS{
		file:     file,
		filePath: filePath,
		name:     name,
		fileInfo: fileInfo,
	}, nil
}

// Close closes the underlying file handle.
func (s *SingleFileFS) Close() error {
	if s.file != nil {
		return s.file.Close()
	}
	return nil
}

// Open opens the named file for reading.
func (s *SingleFileFS) Open(name string) (fs.File, error) {
	if name == "." || name == "" {
		return &singleDirReader{
			entries: []fs.DirEntry{&singleDirEntry{
				name:  s.name,
				mode:  s.fileInfo.Mode(),
				isDir: false,
			}},
		}, nil
	}
	if name != s.name {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}

	_, err := s.file.Seek(0, io.SeekStart)
	if err != nil {
		return nil, err
	}

	return &singleFileReader{
		file: s.file,
		info: s.fileInfo,
	}, nil
}

// Stat returns a FileInfo describing the named file.
func (s *SingleFileFS) Stat(name string) (fs.FileInfo, error) {
	if name == "." || name == "" {
		// Return directory info for root
		return &singleFileInfo{
			name:    ".",
			size:    0,
			mode:    fs.ModeDir | 0755,
			modTime: time.Now(),
			isDir:   true,
		}, nil
	}
	if name != s.name {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	return s.fileInfo, nil
}

// ReadDir reads the directory named by name and returns a list of directory entries.
func (s *SingleFileFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "." || name == "" {
		return []fs.DirEntry{&singleDirEntry{
			name:  s.name,
			mode:  s.fileInfo.Mode(),
			isDir: false,
		}}, nil
	}
	return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
}

// singleFileReader implements fs.File for reading from SingleFileFS.
type singleFileReader struct {
	file *os.File
	info fs.FileInfo
}

func (r *singleFileReader) Read(p []byte) (int, error) {
	return r.file.Read(p)
}

func (r *singleFileReader) Seek(offset int64, whence int) (int64, error) {
	return r.file.Seek(offset, whence)
}

func (r *singleFileReader) Close() error {
	return nil
}

func (r *singleFileReader) Stat() (fs.FileInfo, error) {
	return r.info, nil
}

// singleDirReader implements fs.File for reading directory entries from SingleFileFS.
type singleDirReader struct {
	entries []fs.DirEntry
	index   int
}

func (r *singleDirReader) Read(p []byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: ".", Err: fs.ErrInvalid}
}

func (r *singleDirReader) Seek(offset int64, whence int) (int64, error) {
	return 0, &fs.PathError{Op: "seek", Path: ".", Err: fs.ErrInvalid}
}

func (r *singleDirReader) Close() error {
	return nil
}

func (r *singleDirReader) Stat() (fs.FileInfo, error) {
	return &singleFileInfo{
		name:    ".",
		size:    0,
		mode:    fs.ModeDir | 0755,
		modTime: time.Now(),
		isDir:   true,
	}, nil
}

func (r *singleDirReader) ReadDir(n int) ([]fs.DirEntry, error) {
	if n <= 0 {
		remaining := r.entries[r.index:]
		r.index = len(r.entries)
		return remaining, nil
	}

	if r.index >= len(r.entries) {
		return nil, io.EOF
	}

	end := r.index + n
	if end > len(r.entries) {
		end = len(r.entries)
	}

	entries := r.entries[r.index:end]
	r.index = end
	return entries, nil
}

// singleDirEntry implements fs.DirEntry for SingleFileFS.
type singleDirEntry struct {
	name  string
	mode  fs.FileMode
	isDir bool
}

func (de *singleDirEntry) Name() string      { return de.name }
func (de *singleDirEntry) IsDir() bool       { return de.isDir }
func (de *singleDirEntry) Type() fs.FileMode { return de.mode & fs.ModeType }
func (de *singleDirEntry) Info() (fs.FileInfo, error) {
	return &singleFileInfo{
		name:    de.name,
		mode:    de.mode,
		isDir:   de.isDir,
		size:    0,
		modTime: time.Time{},
	}, nil
}

// singleFileInfo implements fs.FileInfo for SingleFileFS.
type singleFileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
	isDir   bool
}

func (fi *singleFileInfo) Name() string       { return fi.name }
func (fi *singleFileInfo) Size() int64        { return fi.size }
func (fi *singleFileInfo) Mode() fs.FileMode  { return fi.mode }
func (fi *singleFileInfo) ModTime() time.Time { return fi.modTime }
func (fi *singleFileInfo) IsDir() bool        { return fi.isDir }
func (fi *singleFileInfo) Sys() interface{}   { return nil }
