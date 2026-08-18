package ieo

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

// fileDataURIOption is a parsed RFC 2397 data: URI carrying a file payload.
type fileDataURIOption struct {
	MIMEType string
	Name     string
	Size     int64
}

// ParseFileDataURI parses an RFC 2397 data: URI in the SEP-2356 file-input
// wire form: `data:;name=<pct-encoded>;size=<n>;mime=<type>;base64,<b64>`.
// Only the base64 form is accepted (files are binary; the text form is not
// used by the file-input drafts). It returns a reader that streams the decoded
// bytes so the full payload is never materialized in memory; the reader enforces
// the declared size and the hard cap as it is drained.
func ParseFileDataURI(uri string, maxBytes int64) (io.Reader, fileDataURIOption, error) {
	if !strings.HasPrefix(uri, "data:") {
		return nil, fileDataURIOption{}, fmt.Errorf("%w: not a data: URI", ErrInvalidFileReference)
	}
	comma := strings.IndexByte(uri, ',')
	if comma < 0 {
		return nil, fileDataURIOption{}, fmt.Errorf("%w: data URI missing comma", ErrInvalidFileReference)
	}
	meta := uri[len("data:"):comma]
	payload := uri[comma+1:]

	var opt fileDataURIOption
	base64Encoded := false
	if meta != "" {
		for _, part := range strings.Split(meta, ";") {
			part = strings.TrimSpace(part)
			switch {
			case part == "base64":
				base64Encoded = true
			case strings.HasPrefix(part, "name="):
				opt.Name, _ = url.QueryUnescape(strings.TrimPrefix(part, "name="))
			case strings.HasPrefix(part, "size="):
				if n, err := strconv.ParseInt(strings.TrimPrefix(part, "size="), 10, 64); err == nil {
					opt.Size = n
				}
			case strings.HasPrefix(part, "mime=") || strings.Contains(part, "/"):
				opt.MIMEType = part
			case strings.HasPrefix(part, "charset="):
				// ignore
			}
		}
	}
	if !base64Encoded {
		// Drafts always use base64; reject the naive text form to avoid
		// ambiguity between binary and text payloads.
		return nil, fileDataURIOption{}, fmt.Errorf("%w: data URI is not base64-encoded", ErrInvalidFileReference)
	}

	// Base64 expands to ~3/4 of the encoded length; reject absurdly large
	// encodings before allocating a decoder. The streaming reader enforces the
	// exact cap during drainage so the encoded cap check here is just a cheap
	// pre-filter.
	if int64(len(payload)) > maxBytes*2 {
		return nil, fileDataURIOption{}, fmt.Errorf("%w: data URI declares %d encoded bytes (cap %d)", ErrFileTooLarge, len(payload), maxBytes)
	}
	// Derive the exact decoded length from the base64 payload length (RFC 4648:
	// 4 encoded chars -> 3 decoded bytes, minus padding), so callers that need
	// content-length/size see the true size even absent a size= declaration.
	decodedLen := base64DecodedLen(payload)
	if opt.Size == 0 {
		opt.Size = decodedLen
	} else if opt.Size != decodedLen {
		// Validate up front, not only on stream drain: a handler may read
		// exactly the declared byte count and stop, so a mismatch must be
		// rejected before any bytes are handed out.
		return nil, fileDataURIOption{}, fmt.Errorf("%w: declared size %d does not match payload %d", ErrInvalidFileReference, opt.Size, decodedLen)
	}
	if opt.Size > maxBytes {
		return nil, fileDataURIOption{}, fmt.Errorf("%w: declared size %d exceeds cap %d", ErrFileTooLarge, opt.Size, maxBytes)
	}

	dec := base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload))
	r := &boundedDataReader{
		reader:    dec,
		remaining: maxBytes,
		declared:  opt,
	}
	return r, opt, nil
}

// base64DecodedLen returns the number of decoded bytes an RFC 4648 base64
// payload yields, handling line-wrapped input. Newline bytes (\r, \n) are
// skipped (as the base64 decoder does), and base64.StdEncoding.DecodedLen
// supplies the stdlib n/4*3 math; trailing '=' padding is subtracted for an
// exact count.
func base64DecodedLen(encoded string) int64 {
	n := 0
	for i := 0; i < len(encoded); i++ {
		if encoded[i] != '\n' && encoded[i] != '\r' {
			n++
		}
	}
	if n == 0 {
		return 0
	}
	padding := 0
	// Count trailing '=' padding (must be last chars per spec). Skip any
	// trailing CR/LF first: for line-wrapped RFC 4648 input the terminating
	// newline follows the padding, and the decoder skips it, so a payload like
	// "…=="+'\n' must still count its two '=' chars or the decoded size is
	// overestimated and a valid data: URI is wrongly rejected.
	for i := len(encoded) - 1; i >= 0; i-- {
		if encoded[i] == '\r' || encoded[i] == '\n' {
			continue
		}
		if encoded[i] != '=' {
			break
		}
		padding++
	}
	return int64(base64.StdEncoding.DecodedLen(n) - padding)
}

// boundedDataReader streams base64-decoded bytes while enforcing the hard cap
// and the declared size, so an oversized or mismatched payload is rejected as
// it is drained rather than after a full in-memory allocation.
type boundedDataReader struct {
	reader    io.Reader
	remaining int64 // bytes left until hard cap
	read      int64
	done      bool
	declared  fileDataURIOption
}

func (r *boundedDataReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	// When at the cap, probe one byte from the decoder before declaring
	// overflow: a payload whose decoded length exactly equals the cap must be
	// accepted (EOF after the cap byte), only a larger one rejected.
	if r.remaining <= 0 {
		var probe [1]byte
		n, err := r.reader.Read(probe[:])
		if n > 0 {
			r.done = true
			return 0, fmt.Errorf("%w: decoded size exceeds cap", ErrFileTooLarge)
		}
		// The decoder reached EOF exactly at the cap boundary; still validate
		// the declared size against the actual decoded byte count.
		if r.declared.Size > 0 && r.read != r.declared.Size {
			r.done = true
			return 0, fmt.Errorf("%w: declared size %d does not match payload %d", ErrInvalidFileReference, r.declared.Size, r.read)
		}
		r.done = true
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.read += int64(n)
	r.remaining -= int64(n)
	if err == io.EOF {
		if r.declared.Size > 0 && r.read != r.declared.Size {
			r.done = true
			return n, fmt.Errorf("%w: declared size %d does not match payload %d", ErrInvalidFileReference, r.declared.Size, r.read)
		}
		r.done = true
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("%w: data URI has invalid base64: %v", ErrInvalidFileReference, err)
	}
	return n, nil
}

// readDataURI fully drains a parsed data: URI into memory. It is a convenience
// for small callers/tests that need the concrete payload; production handlers
// should stream from the reader returned by ParseFileDataURI.
func readDataURI(uri string, maxBytes int64) ([]byte, fileDataURIOption, error) {
	r, opt, err := ParseFileDataURI(uri, maxBytes)
	if err != nil {
		return nil, opt, err
	}
	buf, err := io.ReadAll(r)
	if err != nil {
		return nil, opt, err
	}
	return buf, opt, nil
}
