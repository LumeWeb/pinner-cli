package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/pterm/pterm"
)

// progressWriter wraps an io.Writer and updates a pterm progress bar with
// throughput (bytes/sec) display. Used for downloads (write side) and uploads
// (read side via progressReader).
type progressWriter struct {
	writer  io.Writer
	bar     *pterm.ProgressbarPrinter
	start   time.Time
	lastUpd time.Time
	total   int64
	written int64
}

// newProgressWriter creates a progress-tracking writer.
// total is the expected byte count (0 for unknown).
// label is the prefix shown on the progress bar (e.g. "Downloading").
func newProgressWriter(w io.Writer, total int64, label string) *progressWriter {
	bar, _ := pterm.DefaultProgressbar.
		WithTotal(int(total)).
		WithTitle(label).
		WithRemoveWhenDone(true).
		Start()
	return &progressWriter{
		writer:  w,
		bar:     bar,
		start:   time.Now(),
		total:   total,
		lastUpd: time.Now(),
	}
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.writer.Write(p)
	if n > 0 {
		pw.written += int64(n)
		pw.bar.Add(n)
		pw.updateTitle()
	}
	return n, err
}

func (pw *progressWriter) updateTitle() {
	now := time.Now()
	if now.Sub(pw.lastUpd) < 100*time.Millisecond {
		return
	}
	pw.lastUpd = now

	elapsed := now.Sub(pw.start).Seconds()
	throughput := fmtBytesPerSec(pw.written, elapsed)

	if pw.total > 0 {
		pct := float64(pw.written) / float64(pw.total) * 100
		pw.bar.UpdateTitle(fmt.Sprintf("%.0f%% %s (%s / %s)", pct, throughput, fmtBytes(pw.written), fmtBytes(pw.total)))
	} else {
		pw.bar.UpdateTitle(fmt.Sprintf("%s (%s)", fmtBytes(pw.written), throughput))
	}
}

func (pw *progressWriter) Close() {
	if pw.bar != nil {
		_, _ = pw.bar.Stop()
	}
}

// progressReader wraps an io.Reader for upload progress.
type progressReader struct {
	reader   io.Reader
	bar      *pterm.ProgressbarPrinter
	start    time.Time
	lastUpd  time.Time
	total    int64
	read     int64
}

func newProgressReader(r io.Reader, total int64, label string) *progressReader {
	bar, _ := pterm.DefaultProgressbar.
		WithTotal(int(total)).
		WithTitle(label).
		WithRemoveWhenDone(true).
		Start()
	return &progressReader{
		reader:  r,
		bar:     bar,
		start:   time.Now(),
		total:   total,
		lastUpd: time.Now(),
	}
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		pr.read += int64(n)
		pr.bar.Add(n)
		pr.updateTitle()
	}
	return n, err
}

func (pr *progressReader) updateTitle() {
	now := time.Now()
	if now.Sub(pr.lastUpd) < 100*time.Millisecond {
		return
	}
	pr.lastUpd = now

	elapsed := now.Sub(pr.start).Seconds()
	throughput := fmtBytesPerSec(pr.read, elapsed)

	if pr.total > 0 {
		pct := float64(pr.read) / float64(pr.total) * 100
		pr.bar.UpdateTitle(fmt.Sprintf("%.0f%% %s (%s / %s)", pct, throughput, fmtBytes(pr.read), fmtBytes(pr.total)))
	} else {
		pr.bar.UpdateTitle(fmt.Sprintf("%s (%s)", fmtBytes(pr.read), throughput))
	}
}

func (pr *progressReader) Close() {
	if pr.bar != nil {
		_, _ = pr.bar.Stop()
	}
}

// fmtBytes formats a byte count human-readably.
func fmtBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// fmtBytesPerSec formats throughput as bytes/sec.
func fmtBytesPerSec(bytes int64, elapsedSec float64) string {
	if elapsedSec <= 0 {
		return "—"
	}
	bps := float64(bytes) / elapsedSec
	const unit = 1000
	if bps < unit {
		return fmt.Sprintf("%.0f B/s", bps)
	}
	div, exp := float64(unit), 0
	for n := bps / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB/s", bps/div, "KMGTPE"[exp])
}
