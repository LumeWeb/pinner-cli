package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadCIDsFromFile(t *testing.T) {
	t.Run("reads CIDs from file", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "cids.txt")

		content := "QmHash1\nQmHash2\nQmHash3\n"
		err := os.WriteFile(testFile, []byte(content), 0644)
		require.NoError(t, err)

		cids, err := readCIDsFromFile(testFile)
		require.NoError(t, err)
		assert.Equal(t, []string{"QmHash1", "QmHash2", "QmHash3"}, cids)
	})

	t.Run("ignores empty lines", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "cids.txt")

		content := "QmHash1\n\nQmHash2\n\n\nQmHash3\n"
		err := os.WriteFile(testFile, []byte(content), 0644)
		require.NoError(t, err)

		cids, err := readCIDsFromFile(testFile)
		require.NoError(t, err)
		assert.Equal(t, []string{"QmHash1", "QmHash2", "QmHash3"}, cids)
	})

	t.Run("ignores comments (lines starting with #)", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "cids.txt")

		content := "QmHash1\n# This is a comment\nQmHash2\n# Another comment\nQmHash3\n"
		err := os.WriteFile(testFile, []byte(content), 0644)
		require.NoError(t, err)

		cids, err := readCIDsFromFile(testFile)
		require.NoError(t, err)
		assert.Equal(t, []string{"QmHash1", "QmHash2", "QmHash3"}, cids)
	})

	t.Run("handles whitespace trimming", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "cids.txt")

		content := "  QmHash1  \n\tQmHash2\t\n  QmHash3  \n"
		err := os.WriteFile(testFile, []byte(content), 0644)
		require.NoError(t, err)

		cids, err := readCIDsFromFile(testFile)
		require.NoError(t, err)
		assert.Equal(t, []string{"QmHash1", "QmHash2", "QmHash3"}, cids)
	})

	t.Run("returns empty list for empty file", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "cids.txt")

		err := os.WriteFile(testFile, []byte(""), 0644)
		require.NoError(t, err)

		cids, err := readCIDsFromFile(testFile)
		require.NoError(t, err)
		assert.Empty(t, cids)
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		_, err := readCIDsFromFile("/non/existent/file.txt")
		require.Error(t, err)
	})
}

func TestReadLinesFromStdin(t *testing.T) {
	t.Run("reads lines from stdin", func(t *testing.T) {
		// Save original stdin
		origStdin := os.Stdin
		defer func() { os.Stdin = origStdin }()

		// Create pipe
		r, w, err := os.Pipe()
		require.NoError(t, err)

		os.Stdin = r

		// Write to stdin in goroutine
		go func() {
			defer func() { _ = w.Close() }()
			_, _ = w.WriteString("QmHash1\nQmHash2\nQmHash3\n")
		}()

		lines, err := readLinesFromStdin()
		require.NoError(t, err)
		assert.Equal(t, []string{"QmHash1", "QmHash2", "QmHash3"}, lines)
	})

	t.Run("ignores comments from stdin", func(t *testing.T) {
		origStdin := os.Stdin
		defer func() { os.Stdin = origStdin }()

		r, w, err := os.Pipe()
		require.NoError(t, err)

		os.Stdin = r

		go func() {
			defer func() { _ = w.Close() }()
			_, _ = w.WriteString("QmHash1\n# Comment\nQmHash2\n")
		}()

		lines, err := readLinesFromStdin()
		require.NoError(t, err)
		assert.Equal(t, []string{"QmHash1", "QmHash2"}, lines)
	})
}

func TestIsStdinPipe(t *testing.T) {
	t.Run("returns false for TTY", func(t *testing.T) {
		// When running tests, stdin is typically not a pipe
		// This test verifies the function doesn't panic
		result := isStdinPipe()
		assert.False(t, result)
	})
}

func TestNewUnpinResult(t *testing.T) {
	t.Run("creates UnpinResult with CID", func(t *testing.T) {
		result := NewUnpinResult("QmTest123")
		assert.Equal(t, "QmTest123", result.CID)
	})
}

func TestNewPinResult(t *testing.T) {
	t.Run("creates PinResult with all fields", func(t *testing.T) {
		result := NewPinResult("QmTest123", "req-456", "queued")
		assert.Equal(t, "QmTest123", result.CID)
		assert.Equal(t, "req-456", result.RequestID)
		assert.Equal(t, "queued", result.Status)
	})
}

func TestBatchOptions(t *testing.T) {
	t.Run("creates BatchOptions with defaults", func(t *testing.T) {
		opts := BatchOptions{}
		assert.Equal(t, 0, opts.Parallel)
		assert.False(t, opts.ContinueOn)
		assert.False(t, opts.Wait)
		assert.False(t, opts.Progress)
	})

	t.Run("creates BatchOptions with values", func(t *testing.T) {
		opts := BatchOptions{
			Parallel:   5,
			ContinueOn: true,
			Wait:       true,
			Progress:   true,
		}
		assert.Equal(t, 5, opts.Parallel)
		assert.True(t, opts.ContinueOn)
		assert.True(t, opts.Wait)
		assert.True(t, opts.Progress)
	})
}

func TestBatchResult(t *testing.T) {
	t.Run("creates empty BatchResult", func(t *testing.T) {
		result := &BatchResult{}
		assert.Equal(t, 0, result.Total)
		assert.Empty(t, result.Succeeded)
		assert.Empty(t, result.Failed)
		assert.Empty(t, result.Skipped)
		assert.Equal(t, time.Duration(0), result.Duration)
	})

	t.Run("creates populated BatchResult", func(t *testing.T) {
		result := &BatchResult{
			Total: 5,
			Succeeded: []OperationResult{
				{CID: "QmHash1", RequestID: "req-1", Status: "pinned"},
				{CID: "QmHash2", RequestID: "req-2", Status: "pinned"},
			},
			Failed: []OperationError{
				{CID: "QmHash3", Error: "invalid CID"},
			},
			Skipped:  []string{"QmHash4"},
			Duration: time.Second * 10,
		}
		assert.Equal(t, 5, result.Total)
		assert.Len(t, result.Succeeded, 2)
		assert.Len(t, result.Failed, 1)
		assert.Len(t, result.Skipped, 1)
		assert.Equal(t, time.Second*10, result.Duration)
	})
}

func TestOperationResult(t *testing.T) {
	t.Run("creates OperationResult", func(t *testing.T) {
		result := OperationResult{
			CID:       "QmTest123",
			RequestID: "req-456",
			Status:    "pinned",
		}
		assert.Equal(t, "QmTest123", result.CID)
		assert.Equal(t, "req-456", result.RequestID)
		assert.Equal(t, "pinned", result.Status)
	})
}

func TestOperationError(t *testing.T) {
	t.Run("creates OperationError", func(t *testing.T) {
		err := OperationError{
			CID:   "QmTest123",
			Error: "invalid CID",
		}
		assert.Equal(t, "QmTest123", err.CID)
		assert.Equal(t, "invalid CID", err.Error)
	})
}

func TestFormatStatusWithColor(t *testing.T) {
	t.Run("colors pinned status", func(t *testing.T) {
		result := formatStatusWithColor("pinned")
		assert.NotEmpty(t, result)
	})

	t.Run("colors queued status", func(t *testing.T) {
		result := formatStatusWithColor("queued")
		assert.NotEmpty(t, result)
	})

	t.Run("colors pinning status", func(t *testing.T) {
		result := formatStatusWithColor("pinning")
		assert.NotEmpty(t, result)
	})

	t.Run("colors failed status", func(t *testing.T) {
		result := formatStatusWithColor("failed")
		assert.NotEmpty(t, result)
	})

	t.Run("returns unknown status unchanged", func(t *testing.T) {
		result := formatStatusWithColor("unknown")
		assert.Equal(t, "unknown", result)
	})
}

func TestDryRunOption(t *testing.T) {
	t.Run("creates option entry", func(t *testing.T) {
		opt := DryRunOption("key", "value")
		require.Len(t, opt, 1)
		assert.Equal(t, "value", opt["key"])
	})
}

func TestRenderDryRun(t *testing.T) {
	t.Run("renders dry run with items", func(t *testing.T) {
		var buf bytes.Buffer
		output := newTestOutput()
		output.SetWriter(&buf)

		RenderDryRun(output, DryRunPreview{
			Operation: "test operation",
			Endpoint:  "https://api.example.com",
			Items:     []string{"item1", "item2", "item3"},
			ItemLabel: "Test items",
			Options: map[string]string{
				"Name":   "test",
				"Wait":   "yes",
				"Option": "value",
			},
		})

		result := buf.String()
		assert.Contains(t, result, "[DRY RUN] Preview of test operation:")
		assert.Contains(t, result, "Endpoint: https://api.example.com")
		assert.Contains(t, result, "Test items: 3")
		assert.Contains(t, result, "- item1")
		assert.Contains(t, result, "- item2")
		assert.Contains(t, result, "- item3")
		assert.Contains(t, result, "Name: test")
		assert.Contains(t, result, "Wait: yes")
		assert.Contains(t, result, "Option: value")
		assert.Contains(t, result, "[DRY RUN] No changes were made. Remove --dry-run to execute.")
	})

	t.Run("renders dry run with truncated items", func(t *testing.T) {
		var buf bytes.Buffer
		output := newTestOutput()
		output.SetWriter(&buf)

		items := make([]string, 15)
		for i := 0; i < 15; i++ {
			items[i] = strings.Repeat("x", 10)
		}

		RenderDryRun(output, DryRunPreview{
			Operation: "test operation",
			Items:     items,
			ItemLabel: "Test items",
			MaxItems:  10,
		})

		result := buf.String()
		assert.Contains(t, result, "Test items: 15")
		assert.Contains(t, result, "... and 5 more")
	})

	t.Run("renders dry run without items", func(t *testing.T) {
		var buf bytes.Buffer
		output := newTestOutput()
		output.SetWriter(&buf)

		RenderDryRun(output, DryRunPreview{
			Operation: "test operation",
			Options: map[string]string{
				"Key": "value",
			},
		})

		result := buf.String()
		assert.Contains(t, result, "[DRY RUN] Preview of test operation:")
		assert.Contains(t, result, "Key: value")
		assert.NotContains(t, result, "Test items:")
	})

	t.Run("renders dry run with custom max items", func(t *testing.T) {
		var buf bytes.Buffer
		output := newTestOutput()
		output.SetWriter(&buf)

		RenderDryRun(output, DryRunPreview{
			Operation: "test operation",
			Items:     []string{"item1", "item2", "item3", "item4", "item5"},
			ItemLabel: "Test items",
			MaxItems:  3,
		})

		result := buf.String()
		assert.Contains(t, result, "Test items: 5")
		assert.Contains(t, result, "... and 2 more")
	})
}
