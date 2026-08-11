package cli

import (
	"io"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/pterm/pterm"
)

// isTerminal checks if the output is a terminal (TTY).
// This prevents progress bars from displaying in non-interactive contexts
// like pipes, redirects, or CI environments.
func isTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd())
}

// ProgressWriter wraps an io.Reader to track read progress and display a pterm progress bar.
type ProgressWriter struct {
	reader   io.Reader
	total    int64
	read     int64
	progress *pterm.ProgressbarPrinter
	enabled  bool
}

// NewProgressWriter creates a new progress writer.
// If enabled is false, total is 0, or not in a terminal, no progress bar will be displayed.
func NewProgressWriter(reader io.Reader, total int64, enabled bool, title string) *ProgressWriter {
	pw := &ProgressWriter{
		reader:  reader,
		total:   total,
		enabled: enabled && total > 0 && isTerminal(),
	}

	if pw.enabled {
		pw.progress = pterm.DefaultProgressbar.
			WithTotal(int(total)).
			WithTitle(title).
			WithRemoveWhenDone(true).
			WithShowCount(false).
			WithShowPercentage(false)
	}

	return pw
}

// Read implements io.Reader, tracking bytes read and updating progress.
func (pw *ProgressWriter) Read(p []byte) (int, error) {
	if pw.reader == nil {
		return 0, io.EOF
	}
	n, err := pw.reader.Read(p)
	if n > 0 {
		pw.read += int64(n)
		if pw.enabled {
			pw.progress.Add(int(n))
		}
	}
	return n, err
}

// Start begins displaying the progress bar.
func (pw *ProgressWriter) Start() error {
	if pw.enabled {
		_, err := pw.progress.Start()
		return err
	}
	return nil
}

// Stop ensures the progress bar is stopped.
func (pw *ProgressWriter) Stop() error {
	if pw.enabled {
		_, err := pw.progress.Stop()
		return err
	}
	return nil
}

// BatchProgressTracker tracks progress for batch operations.
type BatchProgressTracker struct {
	total     int
	completed int
	progress  *pterm.ProgressbarPrinter
	enabled   bool
}

// NewBatchProgressTracker creates a new batch progress tracker.
func NewBatchProgressTracker(total int, enabled bool, title string) *BatchProgressTracker {
	bt := &BatchProgressTracker{
		total:   total,
		enabled: enabled && total > 0 && isTerminal(),
	}

	if bt.enabled {
		bt.progress = pterm.DefaultProgressbar.
			WithTotal(total).
			WithTitle(title).
			WithRemoveWhenDone(true).
			WithShowCount(true).
			WithShowPercentage(false)
	}

	return bt
}

// Increment increments the completed count and updates progress.
func (bt *BatchProgressTracker) Increment() {
	bt.completed++
	if bt.enabled {
		bt.progress.Increment()
	}
}

// Start begins displaying the progress bar.
func (bt *BatchProgressTracker) Start() error {
	if bt.enabled {
		_, err := bt.progress.Start()
		return err
	}
	return nil
}

// Stop ensures the progress bar is stopped.
func (bt *BatchProgressTracker) Stop() error {
	if bt.enabled {
		_, err := bt.progress.Stop()
		return err
	}
	return nil
}
