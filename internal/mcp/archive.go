package mcp

import (
	"context"
	"fmt"
	"io"
	"io/fs"

	contentArchive "go.lumeweb.com/ipfs-content/archive"
)

// ArchiveMode mirrors the portal plugin's archive decision.
type ArchiveMode string

const (
	// ArchiveConvert extracts the archive and uploads its contents.
	ArchiveConvert ArchiveMode = "convert"
	// ArchivePreserve keeps the archive file as-is.
	ArchivePreserve ArchiveMode = "preserve"
)

// ParseArchiveMode parses "convert"/"preserve"; defaults to ArchiveConvert.
func ParseArchiveMode(s string) ArchiveMode {
	switch s {
	case "preserve":
		return ArchivePreserve
	case "convert":
		return ArchiveConvert
	default:
		return ArchiveConvert
	}
}

// SniffArchive sniffs a reader's container format and reports whether it is an
// extractable archive. It reuses contentArchive.DetectFormat. The reader must
// implement io.ReadSeeker (bytes.Reader and *os.File both do); detection seeks
// back to the start.
func SniffArchive(r io.Reader) (contentArchive.Format, bool, error) {
	f, err := contentArchive.DetectFormat(r)
	if err != nil {
		return f, false, err
	}
	return f, f.IsArchiveFormat(), nil
}

// readerAtSeeker is the interface contentArchive.CreateExtractor needs
// (io.Reader + io.ReaderAt + io.Seeker). *os.File and *bytes.Reader satisfy it.
type readerAtSeeker interface {
	io.Reader
	io.ReaderAt
	io.Seeker
}

// OpenArchiveFS opens an archive into an fs.FS view of its entries via
// contentArchive.CreateExtractor(...).Filesystem(ctx). The returned closer must
// be called to release extractor resources. reader must satisfy the
// ReaderAtSeeker interface (io.Reader + io.ReaderAt + io.Seeker); *os.File does.
func OpenArchiveFS(ctx context.Context, reader io.Reader) (fs.FS, func() error, error) {
	rsc, ok := reader.(readerAtSeeker)
	if !ok {
		return nil, nil, fmt.Errorf("reader does not implement archives.ReaderAtSeeker")
	}
	ext, err := contentArchive.CreateExtractor(rsc)
	if err != nil {
		return nil, nil, err
	}
	f, err := ext.Filesystem(ctx)
	if err != nil {
		_ = ext.Close()
		return nil, nil, err
	}
	return f, ext.Close, nil
}
