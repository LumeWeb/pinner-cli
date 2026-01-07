package io

import (
	"io"
)

// ReadSeekCloser wraps an io.Reader to implement io.ReadSeekCloser.
// If the underlying reader supports Seek, it will be used. Otherwise,
// seeking is not supported and will return an error.
type ReadSeekCloser struct {
	r io.Reader
	s io.Seeker
}

// NewReadSeekCloser creates a new ReadSeekCloser wrapping the given reader.
func NewReadSeekCloser(r io.Reader) *ReadSeekCloser {
	s, _ := r.(io.Seeker)
	return &ReadSeekCloser{r: r, s: s}
}

func (rsc *ReadSeekCloser) Read(p []byte) (n int, err error) {
	return rsc.r.Read(p)
}

func (rsc *ReadSeekCloser) Seek(offset int64, whence int) (int64, error) {
	if rsc.s == nil {
		return 0, &ioError{msg: "seek not supported"}
	}
	return rsc.s.Seek(offset, whence)
}

func (rsc *ReadSeekCloser) Close() error {
	if c, ok := rsc.r.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// ioError provides a simple error type for io operations.
type ioError struct {
	msg string
}

func (e *ioError) Error() string {
	return e.msg
}
