package io

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"
)

// StdinFS is a filesystem implementation for reading data from stdin.
// It reads all data from stdin into memory and presents it as a single file.
// This is useful for piping data directly to upload commands.
type StdinFS struct {
	name     string
	content  []byte
	fileInfo fs.FileInfo
}

// NewStdinFS creates a StdinFS by reading all data from stdin.
// The data will be presented as a file with the given name.
// The caller is responsible for ensuring stdin is a pipe and not a terminal.
func NewStdinFS(name string) (*StdinFS, error) {
	content, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("failed to read from stdin: %w", err)
	}

	if len(content) == 0 {
		return nil, fmt.Errorf("no data received from stdin")
	}

	fileInfo := &stdinFileInfo{
		name:    name,
		size:    int64(len(content)),
		mode:    0644,
		modTime: time.Now(),
		isDir:   false,
	}

	return &StdinFS{
		name:     name,
		content:  content,
		fileInfo: fileInfo,
	}, nil
}

// Close is a no-op for StdinFS since we don't hold any open handles.
func (s *StdinFS) Close() error {
	return nil
}

// Open opens the named file for reading.
func (s *StdinFS) Open(name string) (fs.File, error) {
	if name == "." || name == "" {
		return &stdinDirReader{
			entries: []fs.DirEntry{&stdinDirEntry{
				name:  s.name,
				mode:  s.fileInfo.Mode(),
				isDir: false,
			}},
		}, nil
	}
	if name != s.name {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}

	return &stdinFileReader{
		reader: bytes.NewReader(s.content),
		info:   s.fileInfo,
	}, nil
}

// Stat returns a FileInfo describing the named file.
func (s *StdinFS) Stat(name string) (fs.FileInfo, error) {
	if name == "." || name == "" {
		// Return directory info for root
		return &stdinFileInfo{
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
func (s *StdinFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "." || name == "" {
		return []fs.DirEntry{&stdinDirEntry{
			name:  s.name,
			mode:  s.fileInfo.Mode(),
			isDir: false,
		}}, nil
	}
	return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
}

// stdinFileReader implements fs.File for reading from StdinFS.
type stdinFileReader struct {
	reader *bytes.Reader
	info   fs.FileInfo
}

func (r *stdinFileReader) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *stdinFileReader) Close() error {
	return nil
}

func (r *stdinFileReader) Stat() (fs.FileInfo, error) {
	return r.info, nil
}

// stdinDirReader implements fs.File for reading directory entries from StdinFS.
type stdinDirReader struct {
	entries  []fs.DirEntry
	position int
}

func (r *stdinDirReader) Read(p []byte) (int, error) {
	return 0, io.EOF
}

func (r *stdinDirReader) Close() error {
	return nil
}

func (r *stdinDirReader) Stat() (fs.FileInfo, error) {
	return &stdinFileInfo{
		name:    ".",
		size:    0,
		mode:    fs.ModeDir | 0755,
		modTime: time.Now(),
		isDir:   true,
	}, nil
}

func (r *stdinDirReader) ReadDir(n int) ([]fs.DirEntry, error) {
	if n <= 0 {
		remaining := r.entries[r.position:]
		r.position = len(r.entries)
		return remaining, nil
	}

	if r.position >= len(r.entries) {
		return nil, io.EOF
	}

	end := r.position + n
	if end > len(r.entries) {
		end = len(r.entries)
	}

	entries := r.entries[r.position:end]
	r.position = end
	return entries, nil
}

// stdinDirEntry implements fs.DirEntry for StdinFS.
type stdinDirEntry struct {
	name  string
	mode  fs.FileMode
	isDir bool
}

func (de *stdinDirEntry) Name() string      { return de.name }
func (de *stdinDirEntry) IsDir() bool       { return de.isDir }
func (de *stdinDirEntry) Type() fs.FileMode { return de.mode & fs.ModeType }
func (de *stdinDirEntry) Info() (fs.FileInfo, error) {
	return &stdinFileInfo{
		name:    de.name,
		size:    0,
		mode:    de.mode,
		modTime: time.Now(),
		isDir:   de.isDir,
	}, nil
}

// stdinFileInfo implements fs.FileInfo for StdinFS.
type stdinFileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
	isDir   bool
}

func (fi *stdinFileInfo) Name() string       { return fi.name }
func (fi *stdinFileInfo) Size() int64        { return fi.size }
func (fi *stdinFileInfo) Mode() fs.FileMode  { return fi.mode }
func (fi *stdinFileInfo) ModTime() time.Time { return fi.modTime }
func (fi *stdinFileInfo) IsDir() bool        { return fi.isDir }
func (fi *stdinFileInfo) Sys() interface{}   { return nil }
