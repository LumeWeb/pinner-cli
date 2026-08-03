package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestFmtBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1048576, "1.0 MiB"},
		{1073741824, "1.0 GiB"},
		{1099511627776, "1.0 TiB"},
	}
	for _, tt := range tests {
		got := fmtBytes(tt.input)
		if got != tt.want {
			t.Errorf("fmtBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFmtBytes_LargeValues(t *testing.T) {
	// Ensure no panic on large values
	got := fmtBytes(1 << 50)
	if !strings.HasSuffix(got, "iB") {
		t.Errorf("expected suffix 'iB', got %q", got)
	}
}

func TestFmtBytesPerSec(t *testing.T) {
	tests := []struct {
		bytes    int64
		elapsed  float64
		contains string
	}{
		{0, 1.0, "0 B/s"},
		{1000, 1.0, "B/s"},
		{1024, 1.0, "KB/s"},
		{1000000, 1.0, "MB/s"},
		{1000, 0, "—"},
		{1000, -1, "—"},
	}
	for _, tt := range tests {
		got := fmtBytesPerSec(tt.bytes, tt.elapsed)
		if !strings.Contains(got, tt.contains) {
			t.Errorf("fmtBytesPerSec(%d, %f) = %q, want it to contain %q", tt.bytes, tt.elapsed, got, tt.contains)
		}
	}
}

func TestProgressWriter_Write(t *testing.T) {
	var buf bytes.Buffer
	pw := newProgressWriter(&buf, 100, "test")
	defer pw.Close()

	data := []byte("hello world")
	n, err := pw.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("wrote %d bytes, want %d", n, len(data))
	}
	if pw.written != int64(len(data)) {
		t.Errorf("pw.written = %d, want %d", pw.written, int64(len(data)))
	}
	if buf.String() != string(data) {
		t.Errorf("buffer = %q, want %q", buf.String(), string(data))
	}
}

func TestProgressWriter_MultipleWrites(t *testing.T) {
	var buf bytes.Buffer
	pw := newProgressWriter(&buf, 100, "test")
	defer pw.Close()

	chunks := [][]byte{[]byte("foo"), []byte("bar"), []byte("baz")}
	for _, chunk := range chunks {
		n, err := pw.Write(chunk)
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
		if n != len(chunk) {
			t.Errorf("wrote %d bytes, want %d", n, len(chunk))
		}
	}
	if pw.written != 9 {
		t.Errorf("total written = %d, want 9", pw.written)
	}
	if buf.String() != "foobarbaz" {
		t.Errorf("buffer = %q, want %q", buf.String(), "foobarbaz")
	}
}

func TestProgressReader_Read(t *testing.T) {
	src := bytes.NewReader([]byte("hello world"))
	pr := newProgressReader(src, int64(src.Len()), "test")
	defer pr.Close()

	buf := make([]byte, 32)
	n, err := pr.Read(buf)
	if err != nil && err.Error() != "EOF" {
		t.Fatalf("Read failed: %v", err)
	}
	if n != 11 {
		t.Errorf("read %d bytes, want 11", n)
	}
	if pr.read != 11 {
		t.Errorf("pr.read = %d, want 11", pr.read)
	}
}

func TestProgressReader_MultipleReads(t *testing.T) {
	src := bytes.NewReader([]byte("abcdefghij"))
	pr := newProgressReader(src, int64(src.Len()), "test")
	defer pr.Close()

	buf := make([]byte, 4)
	totalRead := 0
	for {
		n, err := pr.Read(buf)
		totalRead += n
		if err != nil {
			break
		}
	}
	if totalRead != 10 {
		t.Errorf("total read = %d, want 10", totalRead)
	}
	if pr.read != 10 {
		t.Errorf("pr.read = %d, want 10", pr.read)
	}
}
