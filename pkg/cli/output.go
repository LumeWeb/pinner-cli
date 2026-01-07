package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ggwhite/go-masker"
	go_pinning_service_http_client "github.com/ipfs/boxo/pinning/remote/client"
	"github.com/pterm/pterm"
	"github.com/samber/lo"
)

// sensitiveKeywords contains keywords that indicate sensitive data should be masked.
var sensitiveKeywords = []string{"token", "auth", "password", "secret", "key"}

// Output defines the interface for CLI output formatting.
type Output interface {
	// Print prints a message to the output.
	Print(message string)

	// Printf formats and prints a message to the output.
	Printf(format string, args ...any)

	// Printfln formats and prints a message to the output with a newline.
	Printfln(format string, args ...any)

	// PrintJSON prints data as JSON to the output.
	PrintJSON(data any) error

	// PrintVerbose prints a message only if verbose mode is enabled.
	PrintVerbose(message string)

	// PrintVerbosef formats and prints a message only if verbose mode is enabled.
	PrintVerbosef(format string, args ...any)

	// PrintError prints an error message to stderr.
	PrintError(err error)

	// SetWriter sets the output writer.
	SetWriter(w io.Writer)

	// IsJSON returns true if JSON output is enabled.
	IsJSON() bool

	// IsVerbose returns true if verbose mode is enabled.
	IsVerbose() bool

	// IsQuiet returns true if quiet mode is enabled.
	IsQuiet() bool

	// IsUnmask returns true if unmask mode is enabled.
	IsUnmask() bool

	// PrintTable prints data as a formatted table.
	PrintTable(headers []string, rows [][]string)

	// PrintList prints items as a bulleted list.
	PrintList(items []string)

	// MaskSensitive masks sensitive data like tokens, passwords, etc.
	MaskSensitive(value, key string) string

	// Watch continuously monitors and displays updates for the provided data fetcher.
	// Returns when all items reach terminal status or context is cancelled.
	Watch(ctx context.Context, fetcher func(context.Context) (any, error), formatter func(any) (string, []string, [][]string)) error
}

// outputConfig holds configuration for output formatters.
type outputConfig struct {
	json    bool
	verbose bool
	quiet   bool
	unmask  bool
	writer  io.Writer
}

// baseFormatter provides common functionality for output formatters.
type baseFormatter struct {
	config outputConfig
}

// SetWriter sets the output writer.
func (b *baseFormatter) SetWriter(w io.Writer) {
	b.config.writer = w
}

// IsJSON returns true if JSON output is enabled.
func (b *baseFormatter) IsJSON() bool {
	return b.config.json
}

// IsVerbose returns true if verbose mode is enabled.
func (b *baseFormatter) IsVerbose() bool {
	return b.config.verbose
}

// IsQuiet returns true if quiet mode is enabled.
func (b *baseFormatter) IsQuiet() bool {
	return b.config.quiet
}

// IsUnmask returns true if unmask mode is enabled.
func (b *baseFormatter) IsUnmask() bool {
	return b.config.unmask
}

// PrintError prints an error message to stderr.
func (b *baseFormatter) PrintError(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
}

// PrintVerbose prints a message only if verbose mode is enabled and not in JSON mode.
func (b *baseFormatter) PrintVerbose(message string) {
	if b.config.verbose && !b.config.quiet && !b.config.json {
		fmt.Fprintf(b.config.writer, "[verbose] %s\n", message)
	}
}

// PrintVerbosef formats and prints a message only if verbose mode is enabled and not in JSON mode.
func (b *baseFormatter) PrintVerbosef(format string, args ...any) {
	if b.config.verbose && !b.config.quiet && !b.config.json {
		fmt.Fprintf(b.config.writer, "[verbose] "+format+"\n", args...)
	}
}

// humanFormatter provides human-readable output formatting.
type humanFormatter struct {
	baseFormatter
}

// Print prints a message to the output.
func (h *humanFormatter) Print(message string) {
	if !h.config.quiet {
		fmt.Fprintln(h.config.writer, message)
	}
}

// Printf formats and prints a message to the output.
func (h *humanFormatter) Printf(format string, args ...any) {
	if !h.config.quiet {
		fmt.Fprintf(h.config.writer, format, args...)
	}
}

// Printfln formats and prints a message to the output with a newline.
func (h *humanFormatter) Printfln(format string, args ...any) {
	if !h.config.quiet {
		fmt.Fprintf(h.config.writer, format+"\n", args...)
	}
}

// PrintJSON prints data as JSON to the output.
func (h *humanFormatter) PrintJSON(data any) error {
	encoder := json.NewEncoder(h.config.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// PrintTable prints data as a formatted table.
func (h *humanFormatter) PrintTable(headers []string, rows [][]string) {
	if h.config.quiet {
		return
	}

	tableData := pterm.TableData{headers}
	for _, row := range rows {
		tableData = append(tableData, row)
	}

	pterm.DefaultTable.
		WithHasHeader().
		WithBoxed().
		WithWriter(h.config.writer).
		WithData(tableData).
		Render()
}

// PrintList prints items as a bulleted list.
func (h *humanFormatter) PrintList(items []string) {
	if h.config.quiet {
		return
	}

	bulletItems := lo.Map(items, func(item string, _ int) pterm.BulletListItem {
		return pterm.BulletListItem{
			Level: 0,
			Text:  item,
		}
	})

	pterm.DefaultBulletList.WithItems(bulletItems).WithWriter(h.config.writer).Render()
}

// MaskSensitive masks sensitive data based on the key name.
func (h *humanFormatter) MaskSensitive(value, key string) string {
	if h.config.unmask {
		return value
	}
	return maskSensitiveValue(value, key)
}

// Watch continuously monitors and displays updates for the provided data fetcher.
func (h *humanFormatter) Watch(ctx context.Context, fetcher func(context.Context) (any, error), formatter func(any) (string, []string, [][]string)) error {
	return watchLoop(ctx, 2*time.Second, h, true, fetcher, formatter)
}

// jsonFormatter provides JSON-only output formatting.
type jsonFormatter struct {
	baseFormatter
}

// Print prints a message as JSON to the output.
func (j *jsonFormatter) Print(message string) {
	if !j.config.quiet {
		data := map[string]string{"message": message}
		j.PrintJSON(data)
	}
}

// Printf formats and prints a message as JSON to the output.
func (j *jsonFormatter) Printf(format string, args ...any) {
	if !j.config.quiet {
		message := fmt.Sprintf(format, args...)
		data := map[string]string{"message": message}
		j.PrintJSON(data)
	}
}

// Printfln formats and prints a message as JSON to the output (same as Printf for JSON).
func (j *jsonFormatter) Printfln(format string, args ...any) {
	if !j.config.quiet {
		message := fmt.Sprintf(format, args...)
		data := map[string]string{"message": message}
		j.PrintJSON(data)
	}
}

// PrintJSON prints data as JSON to the output.
func (j *jsonFormatter) PrintJSON(data any) error {
	encoder := json.NewEncoder(j.config.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// PrintTable prints data as JSON (table format).
func (j *jsonFormatter) PrintTable(headers []string, rows [][]string) {
	if j.config.quiet {
		return
	}

	data := map[string]any{
		"type":    "table",
		"headers": headers,
		"rows":    rows,
	}
	j.PrintJSON(data)
}

// PrintList prints items as JSON (list format).
func (j *jsonFormatter) PrintList(items []string) {
	if j.config.quiet {
		return
	}

	data := map[string]any{
		"type":  "list",
		"items": items,
	}
	j.PrintJSON(data)
}

// MaskSensitive masks sensitive data based on the key name.
func (j *jsonFormatter) MaskSensitive(value, key string) string {
	if j.config.unmask {
		return value
	}
	return maskSensitiveValue(value, key)
}

// Watch continuously monitors and displays updates for the provided data fetcher.
func (j *jsonFormatter) Watch(ctx context.Context, fetcher func(context.Context) (any, error), formatter func(any) (string, []string, [][]string)) error {
	return watchLoop(ctx, 2*time.Second, j, false, fetcher, formatter)
}

// allTerminal checks if all rows indicate terminal status (not queued or pinning).
func allTerminal(rows [][]string) bool {
	for _, row := range rows {
		if len(row) < 3 {
			continue
		}
		status := go_pinning_service_http_client.Status(row[2])
		if status == go_pinning_service_http_client.StatusQueued || status == go_pinning_service_http_client.StatusPinning {
			return false
		}
	}
	return true
}

// watchLoop is the shared watch implementation for both human and JSON formatters.
func watchLoop(ctx context.Context, interval time.Duration, output Output, humanFormat bool, fetcher func(context.Context) (any, error), formatter func(any) (string, []string, [][]string)) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	output.Printf("Watching (Press Ctrl+C to stop)...\n")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			data, err := fetcher(ctx)
			if err != nil {
				return err
			}

			if humanFormat {
				pterm.Printo("\r")
			}

			title, headers, rows := formatter(data)

			if humanFormat && title != "" {
				output.Printf("%s\n", title)
			}

			if !humanFormat {
				watchData := map[string]any{
					"type":    "watch",
					"title":   title,
					"headers": headers,
					"rows":    rows,
					"time":    time.Now().Format(time.RFC3339),
				}
				if err := output.PrintJSON(watchData); err != nil {
					return err
				}
			} else {
				output.PrintTable(headers, rows)
			}

			if len(rows) == 0 {
				output.Printf("No items found\n")
				return nil
			}

			if allTerminal(rows) {
				output.Printf("All items have reached terminal status\n")
				return nil
			}
		}
	}
}

// maskSensitiveValue masks sensitive data based on the key name.
// It uses go-masker to mask tokens, passwords, and other sensitive data.
func maskSensitiveValue(value, key string) string {
	if value == "" {
		return ""
	}

	keyLower := strings.ToLower(key)

	// Check if the key indicates sensitive data
	if lo.SomeBy(sensitiveKeywords, func(keyword string) bool {
		return strings.Contains(keyLower, keyword)
	}) {
		return masker.ID(value)
	}

	// Check for email
	if strings.Contains(keyLower, "email") {
		return masker.Email(value)
	}

	// Return unmasked value for non-sensitive keys
	return value
}

// NewOutputFormatter creates a new output formatter based on configuration.
func NewOutputFormatter(json, verbose, quiet, unmask bool) Output {
	config := outputConfig{
		json:    json,
		verbose: verbose,
		quiet:   quiet,
		unmask:  unmask,
		writer:  os.Stdout,
	}

	base := baseFormatter{config: config}

	if json {
		return &jsonFormatter{baseFormatter: base}
	}
	return &humanFormatter{baseFormatter: base}
}
