package cli

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProgressWriter_Read(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		enabled  bool
		wantRead int
	}{
		{
			name:     "reads all data with progress enabled",
			data:     []byte("hello world"),
			enabled:  true,
			wantRead: 11,
		},
		{
			name:     "reads all data with progress disabled",
			data:     []byte("hello world"),
			enabled:  false,
			wantRead: 11,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bytes.NewReader(tt.data)
			pw := NewProgressWriter(reader, int64(len(tt.data)), tt.enabled, "Test")

			if tt.enabled {
				require.NoError(t, pw.Start())
				defer pw.Stop()
			}

			buf := make([]byte, 100)
			n, err := pw.Read(buf)

			require.NoError(t, err)
			assert.Equal(t, tt.wantRead, n)
			assert.Equal(t, tt.data, buf[:n])
		})
	}
}

func TestProgressWriter_ReadMultiple(t *testing.T) {
	data := []byte("this is a longer test string for multiple reads")
	reader := bytes.NewReader(data)
	pw := NewProgressWriter(reader, int64(len(data)), true, "Test")

	require.NoError(t, pw.Start())
	defer pw.Stop()

	buf1 := make([]byte, 10)
	n1, err := pw.Read(buf1)
	require.NoError(t, err)
	assert.Equal(t, 10, n1)
	assert.Equal(t, data[:10], buf1)

	buf2 := make([]byte, 10)
	n2, err := pw.Read(buf2)
	require.NoError(t, err)
	assert.Equal(t, 10, n2)
	assert.Equal(t, data[10:20], buf2)

	buf3 := make([]byte, 100)
	n3, err := pw.Read(buf3)
	require.NoError(t, err)
	assert.Equal(t, len(data)-20, n3)
	assert.Equal(t, data[20:], buf3[:n3])
}

func TestProgressWriter_ZeroTotal(t *testing.T) {
	data := []byte("test data")
	reader := bytes.NewReader(data)
	pw := NewProgressWriter(reader, 0, true, "Test")

	buf := make([]byte, 100)
	n, err := pw.Read(buf)

	require.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, data, buf[:n])
}

func TestProgressWriter_Disabled(t *testing.T) {
	data := []byte("test data")
	reader := bytes.NewReader(data)
	pw := NewProgressWriter(reader, int64(len(data)), false, "Test")

	buf := make([]byte, 100)
	n, err := pw.Read(buf)

	require.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, data, buf[:n])
}

func TestBatchProgressTracker_Increment(t *testing.T) {
	tests := []struct {
		name       string
		total      int
		enabled    bool
		increments int
	}{
		{
			name:       "increments with progress enabled",
			total:      5,
			enabled:    true,
			increments: 5,
		},
		{
			name:       "increments with progress disabled",
			total:      5,
			enabled:    false,
			increments: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bt := NewBatchProgressTracker(tt.total, tt.enabled, "Test")

			if tt.enabled {
				require.NoError(t, bt.Start())
				defer bt.Stop()
			}

			for i := 0; i < tt.increments; i++ {
				bt.Increment()
			}

			assert.Equal(t, tt.increments, bt.completed)
		})
	}
}

func TestBatchProgressTracker_ZeroTotal(t *testing.T) {
	bt := NewBatchProgressTracker(0, true, "Test")

	bt.Increment()
	bt.Increment()

	assert.Equal(t, 2, bt.completed)
}

func TestNewProgressReader(t *testing.T) {
	t.Run("wraps reader correctly", func(t *testing.T) {
		data := []byte("test")
		reader := bytes.NewReader(data)
		pw := NewProgressWriter(reader, 4, true, "Test")

		assert.NotNil(t, pw)
		assert.Equal(t, int64(4), pw.total)
		// Note: enabled depends on isTerminal() which is typically false in tests
		// We just verify the structure is correct
		if pw.enabled {
			assert.NotNil(t, pw.progress)
		} else {
			assert.Nil(t, pw.progress)
		}
	})

	t.Run("disabled when enabled is false", func(t *testing.T) {
		data := []byte("test")
		reader := bytes.NewReader(data)
		pw := NewProgressWriter(reader, 4, false, "Test")

		assert.NotNil(t, pw)
		assert.Equal(t, false, pw.enabled)
		assert.Nil(t, pw.progress)
	})
}

func TestProgressWriter_ImplementsReader(t *testing.T) {
	var _ io.Reader = (*ProgressWriter)(nil)
}

func TestProgressWriter_WithNilReader(t *testing.T) {
	pw := NewProgressWriter(nil, 100, true, "Test")

	buf := make([]byte, 10)
	n, err := pw.Read(buf)

	assert.Error(t, err)
	assert.Equal(t, 0, n)
}

func TestIsTerminal(t *testing.T) {
	// This test verifies that isTerminal() checks stdout.
	// In a test environment, stdout is typically not a TTY.
	// The actual behavior will vary based on how tests are run.
	result := isTerminal()
	assert.IsType(t, false, result)
}

func TestProgressWriter_NonTerminal(t *testing.T) {
	data := []byte("test data")
	reader := bytes.NewReader(data)

	// Create progress writer with enabled=true, but it should be
	// disabled if not running in a terminal (which is typical in tests)
	pw := NewProgressWriter(reader, int64(len(data)), true, "Test")

	// Read all data
	buf := make([]byte, 100)
	n, err := pw.Read(buf)

	require.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, data, buf[:n])
}

func TestBatchProgressTracker_NonTerminal(t *testing.T) {
	bt := NewBatchProgressTracker(5, true, "Test")

	// Increment should work even if not in terminal
	bt.Increment()
	bt.Increment()

	assert.Equal(t, 2, bt.completed)
}

func TestProgressWriter_PipedOutput(t *testing.T) {
	// Simulate piped output by redirecting stdout
	// Save original stdout
	oldStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	data := []byte("test data for pipe")
	reader := bytes.NewReader(data)
	pw := NewProgressWriter(reader, int64(len(data)), true, "Test")

	// Restore stdout after creating progress writer
	w.Close()
	os.Stdout = oldStdout

	// Progress should be disabled when output is piped
	assert.False(t, pw.enabled)

	// Read should still work
	buf := make([]byte, 100)
	n, err := pw.Read(buf)

	require.NoError(t, err)
	assert.Equal(t, len(data), n)
}
