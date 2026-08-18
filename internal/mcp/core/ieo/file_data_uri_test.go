package ieo

import (
	"encoding/base64"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func encodeDataURI(payload []byte, name string) string {
	m := ";name=" + url.QueryEscape(name) + ";size=" + strconv.Itoa(len(payload)) + ";base64,"
	return "data:" + m + base64.StdEncoding.EncodeToString(payload)
}

func TestParseFileDataURI(t *testing.T) {
	payload := []byte("hello world")
	uri := encodeDataURI(payload, "report.txt")
	decoded, opt, err := readDataURI(uri, 1<<20)
	require.NoError(t, err)
	require.Equal(t, payload, decoded)
	require.Equal(t, "report.txt", opt.Name)
	require.EqualValues(t, len(payload), opt.Size)
}

func TestParseFileDataURIRejectsNonBase64(t *testing.T) {
	uri := "data:;name=a.txt," + base64.StdEncoding.EncodeToString([]byte("x"))
	_, _, err := readDataURI(uri, 1<<20)
	require.ErrorIs(t, err, ErrInvalidFileReference)
}

func TestParseFileDataURIRejectsOversize(t *testing.T) {
	payload := []byte("12345678901234567890")
	uri := encodeDataURI(payload, "b.bin")
	_, _, err := readDataURI(uri, 5)
	require.ErrorIs(t, err, ErrFileTooLarge)
}

func TestParseFileDataURIRejectsNotData(t *testing.T) {
	_, _, err := readDataURI("https://example.com/x", 1<<20)
	require.ErrorIs(t, err, ErrInvalidFileReference)
}

func TestParseFileDataURIMissingComma(t *testing.T) {
	_, _, err := readDataURI("data:;base64", 1<<20)
	require.ErrorIs(t, err, ErrInvalidFileReference)
}

func TestParseFileDataURIMismatchedSize(t *testing.T) {
	payload := []byte("abc")
	uri := "data:;size=999;base64," + base64.StdEncoding.EncodeToString(payload)
	_, _, err := readDataURI(uri, 1<<20)
	require.ErrorIs(t, err, ErrInvalidFileReference)
}

func TestParseFileDataURIDerivesSizeWhenUnspecified(t *testing.T) {
	payload := []byte("a payload of some length")
	uri := "data:;name=x.txt;base64," + base64.StdEncoding.EncodeToString(payload)
	_, opt, err := ParseFileDataURI(uri, 1<<20)
	require.NoError(t, err)
	// Without a size= declaration, the decoded length must be derived exactly.
	require.EqualValues(t, len(payload), opt.Size)
}

func TestBase64DecodedLen(t *testing.T) {
	require.Equal(t, int64(0), base64DecodedLen(""))         // empty
	require.Equal(t, int64(3), base64DecodedLen("YWJj"))     // abc  (3 bytes, no pad)
	require.Equal(t, int64(2), base64DecodedLen("YWI="))     // ab   (2 bytes, 1 pad)
	require.Equal(t, int64(1), base64DecodedLen("YQ=="))     // a    (1 byte, 2 pad)
	require.Equal(t, int64(3), base64DecodedLen("YWIy"))     // ab2  (3 bytes, no pad)
	require.Equal(t, int64(5), base64DecodedLen("Y2xpY2s=")) // click (5 bytes, 1 pad)
	require.Equal(t, int64(4), base64DecodedLen("YWJjZA==")) // abcd (4 bytes, 2 pad)
	// Embedded newlines are skipped by the base64 decoder; length must ignore them.
	require.Equal(t, int64(10), base64DecodedLen("YWJj\nZGVm\r\nZ2hp"+"jA==")) // abcdef... 10 bytes
	// Trailing newline AFTER the padding is also skipped by the decoder, so the
	// padding must still be counted (regression: a valid line-wrapped payload
	// ending in "…==\n" used to have its padding under-counted to 0 and be
	// rejected as a size mismatch).
	require.Equal(t, int64(2), base64DecodedLen("YWI=\n"))   // ab, \n after 1 pad
	require.Equal(t, int64(1), base64DecodedLen("YQ==\r\n")) // a, \r\n after 2 pads
}

func TestDataUIExactCapAccepted(t *testing.T) {
	// A payload whose decoded length exactly equals the cap is valid and must
	// stream to EOF without an overflow error.
	payload := make([]byte, 1024)
	for i := range payload {
		payload[i] = byte(i)
	}
	uri := "data:;name=cap.bin;base64," + base64.StdEncoding.EncodeToString(payload)
	buf, opt, err := readDataURI(uri, 1024)
	require.NoError(t, err)
	require.Equal(t, payload, buf)
	require.EqualValues(t, 1024, opt.Size)
}

func TestDataUIOverCapRejected(t *testing.T) {
	payload := make([]byte, 1025)
	uri := "data:;name=over.bin;base64," + base64.StdEncoding.EncodeToString(payload)
	_, _, err := readDataURI(uri, 1024)
	require.ErrorIs(t, err, ErrFileTooLarge)
}

func TestDataUIExactCapMismatchedDeclaredSizeRejected(t *testing.T) {
	// A payload that exactly fills the cap but declares a smaller size must be
	// rejected for the size mismatch even though it reaches EOF at the boundary.
	payload := make([]byte, 1024)
	uri := "data:;size=100;base64," + base64.StdEncoding.EncodeToString(payload)
	_, _, err := readDataURI(uri, 1024)
	require.ErrorIs(t, err, ErrInvalidFileReference)
}

func TestDataUIMismatchedDeclaredSizeRejected(t *testing.T) {
	payload := []byte("hello world")
	uri := "data:;size=5;base64," + base64.StdEncoding.EncodeToString(payload)
	_, _, err := readDataURI(uri, 1<<20)
	require.ErrorIs(t, err, ErrInvalidFileReference)
}
