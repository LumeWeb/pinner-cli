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

// Field represents a labeled key-value pair for display.
type Field struct {
	Label string
	Value string
}

// FieldGroup represents a titled collection of fields for structured display.
type FieldGroup struct {
	Title  string
	Fields []Field
	PadTop int
}

// CurrencyCode represents an ISO 4217 currency code.
type CurrencyCode string

// Currency constants for ISO 4217 codes.
const (
	USD CurrencyCode = "USD"
)

// JSON output type constants.
const (
	jsonTypeList        = "list"
	jsonTypeTable       = "table"
	jsonTypeFields      = "fields"
	jsonTypeListGroup   = "list-group"
	jsonTypeMessage     = "message"
	jsonTypeBatchResult = "batch_result"
)

// Currency represents a monetary amount with a currency code.
type Currency struct {
	Amount float64
	Code   CurrencyCode // "USD", "EUR", etc.
}

// String formats the currency as "Amount Code" (e.g., "19.99 USD").
func (c Currency) String() string {
	return fmt.Sprintf("%.2f %s", c.Amount, c.Code)
}

// NewCurrency creates a new Currency with the given amount and code.
func NewCurrency(amount float64, code CurrencyCode) Currency {
	return Currency{Amount: amount, Code: code}
}

// FormatCurrency formats an amount with the given currency code.
func FormatCurrency(amount float64, code CurrencyCode) string {
	return NewCurrency(amount, code).String()
}

// FormatUSD formats a USD amount.
func FormatUSD(amount float32) string {
	return FormatCurrency(float64(amount), USD)
}

// jsonFieldGroup represents a titled FieldGroup for JSON serialization.
type jsonFieldGroup struct {
	Title  string            `json:"title"`
	Type   string            `json:"type"`
	Fields map[string]string `json:"fields"`
}

// jsonListGroup represents a ListGroup for JSON serialization.
type jsonListGroup struct {
	Title     string            `json:"title,omitempty"`
	Type      string            `json:"type"`
	Fields    map[string]string `json:"fields"`
	Items     []string          `json:"items,omitempty"`
	ItemCount int               `json:"item_count,omitempty"`
	Footer    string            `json:"footer,omitempty"`
}

// jsonTable represents a table for JSON serialization.
type jsonTable struct {
	Type    string     `json:"type"`
	Headers []string   `json:"headers"`
	Rows    [][]string `json:"rows"`
}

// jsonList represents a list for JSON serialization.
type jsonList struct {
	Type  string   `json:"type"`
	Items []string `json:"items"`
}

// jsonMessage represents a simple message for JSON serialization.
type jsonMessage struct {
	Message string `json:"message"`
}

// jsonBatchError represents a failed operation in a batch result for JSON serialization.
type jsonBatchError struct {
	CID   string `json:"cid"`
	Error string `json:"error"`
}

// jsonBatchResult represents a batch operation result for JSON serialization.
type jsonBatchResult struct {
	Type      string           `json:"type"`
	Duration  string           `json:"duration"`
	Total     int              `json:"total"`
	Succeeded int              `json:"succeeded"`
	Failed    int              `json:"failed"`
	Skipped   int              `json:"skipped"`
	Errors    []jsonBatchError `json:"errors,omitempty"`
}

// ListGroup represents a titled collection with fields and an optional item list.
type ListGroup struct {
	Title     string
	Fields    []Field
	Items     []string
	ItemLabel string
	MaxItems  int
	Footer    string
	PadTop    int
}

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

	// PrintFields renders a titled group of labeled fields.
	PrintFields(group FieldGroup)

	// PrintListGroup renders a titled group with fields and an optional item list.
	PrintListGroup(group ListGroup)

	// PrintBatchResult renders a batch operation summary with structured output.
	PrintBatchResult(result *BatchResult)

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

	// Word-wrap cell values so the table fits the terminal.
	// Calculate a max column width based on terminal size.
	maxColWidth := 50
	if termWidth := pterm.GetTerminalWidth(); termWidth > 0 {
		// Reserve space for box borders, separators, and padding
		// Box: 2 chars each side + 2 padding = ~6, separators: ~3 per column
		overhead := 6 + (len(headers)-1)*3
		available := termWidth - overhead
		if available > 20 {
			// Give 40% to the widest column, rest distributed evenly
			maxColWidth = available * 2 / (len(headers) + 1)
			if maxColWidth < 20 {
				maxColWidth = 20
			}
		}
	}

	wrappedRows := make([][]string, len(rows))
	for i, row := range rows {
		wrappedRows[i] = make([]string, len(row))
		for j, cell := range row {
			wrappedRows[i][j] = wordWrap(cell, maxColWidth)
		}
	}

	tableData := pterm.TableData{headers}
	for _, row := range wrappedRows {
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

// PrintFields renders a titled group of labeled fields with human-readable formatting.
func (h *humanFormatter) PrintFields(group FieldGroup) {
	if h.config.quiet {
		return
	}

	for i := 0; i < group.PadTop; i++ {
		fmt.Fprintln(h.config.writer)
	}

	if group.Title != "" {
		fmt.Fprintln(h.config.writer, group.Title)
	}

	maxLabel := 0
	for _, field := range group.Fields {
		if len(field.Label) > maxLabel {
			maxLabel = len(field.Label)
		}
	}

	for _, field := range group.Fields {
		fmt.Fprintf(h.config.writer, "  %-*s  %s\n", maxLabel, field.Label, field.Value)
	}
}

// PrintListGroup renders a titled group with fields and an optional item list.
func (h *humanFormatter) PrintListGroup(group ListGroup) {
	if h.config.quiet {
		return
	}

	for i := 0; i < group.PadTop; i++ {
		fmt.Fprintln(h.config.writer)
	}

	if group.Title != "" {
		fmt.Fprintln(h.config.writer, group.Title)
	}

	for _, field := range group.Fields {
		fmt.Fprintf(h.config.writer, "  %s: %s\n", field.Label, field.Value)
	}

	if len(group.Items) > 0 && group.ItemLabel != "" {
		fmt.Fprintf(h.config.writer, "  %s: %d\n", group.ItemLabel, len(group.Items))
	}

	if len(group.Items) > 0 {
		limit := group.MaxItems
		if limit <= 0 {
			limit = 10
		}
		for i, item := range group.Items {
			if i >= limit {
				fmt.Fprintf(h.config.writer, "    ... and %d more\n", len(group.Items)-limit)
				break
			}
			fmt.Fprintf(h.config.writer, "    - %s\n", item)
		}
	}

	if group.Footer != "" {
		fmt.Fprintln(h.config.writer)
		fmt.Fprintln(h.config.writer, group.Footer)
	}
}

// PrintBatchResult renders a batch operation summary with human-readable formatting.
func (h *humanFormatter) PrintBatchResult(result *BatchResult) {
	if h.config.quiet {
		return
	}

	h.PrintFields(FieldGroup{
		Fields: []Field{
			{"Duration", result.Duration.Round(time.Millisecond).String()},
			{"Succeeded", fmt.Sprintf("%d", len(result.Succeeded))},
			{"Failed", fmt.Sprintf("%d", len(result.Failed))},
			{"Skipped", fmt.Sprintf("%d", len(result.Skipped))},
		},
	})

	if len(result.Failed) > 0 {
		fmt.Fprintln(h.config.writer)
		headers := []string{"CID", "ERROR"}
		rows := make([][]string, len(result.Failed))
		for i, fail := range result.Failed {
			rows[i] = []string{fail.CID, fail.Error}
		}
		h.PrintTable(headers, rows)
	}
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
		j.PrintJSON(jsonMessage{Message: message})
	}
}

// Printf formats and prints a message as JSON to the output.
func (j *jsonFormatter) Printf(format string, args ...any) {
	if !j.config.quiet {
		message := fmt.Sprintf(format, args...)
		j.PrintJSON(jsonMessage{Message: message})
	}
}

// Printfln formats and prints a message as JSON to the output (same as Printf for JSON).
func (j *jsonFormatter) Printfln(format string, args ...any) {
	if !j.config.quiet {
		message := fmt.Sprintf(format, args...)
		j.PrintJSON(jsonMessage{Message: message})
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

	j.PrintJSON(jsonTable{
		Type:    jsonTypeTable,
		Headers: headers,
		Rows:    rows,
	})
}

// PrintList prints items as JSON (list format).
func (j *jsonFormatter) PrintList(items []string) {
	if j.config.quiet {
		return
	}

	j.PrintJSON(jsonList{
		Type:  jsonTypeList,
		Items: items,
	})
}

// MaskSensitive masks sensitive data based on the key name.
func (j *jsonFormatter) MaskSensitive(value, key string) string {
	if j.config.unmask {
		return value
	}
	return maskSensitiveValue(value, key)
}

// PrintFields renders a titled group of labeled fields as structured JSON.
func (j *jsonFormatter) PrintFields(group FieldGroup) {
	if j.config.quiet {
		return
	}

	fields := make(map[string]string, len(group.Fields))
	for _, field := range group.Fields {
		fields[field.Label] = field.Value
	}

	if group.Title != "" {
		j.PrintJSON(struct {
			Title  string            `json:"title"`
			Type   string            `json:"type"`
			Fields map[string]string `json:"fields"`
		}{
			Title:  group.Title,
			Type:   jsonTypeFields,
			Fields: fields,
		})
	} else {
		j.PrintJSON(fields)
	}
}

// PrintListGroup renders a titled group with fields and an optional item list as structured JSON.
func (j *jsonFormatter) PrintListGroup(group ListGroup) {
	if j.config.quiet {
		return
	}

	fields := make(map[string]string, len(group.Fields))
	for _, field := range group.Fields {
		fields[field.Label] = field.Value
	}

	result := jsonListGroup{
		Type:   "list-group",
		Fields: fields,
		Items:  group.Items,
	}

	if group.Title != "" {
		result.Title = group.Title
	}
	if group.Footer != "" {
		result.Footer = group.Footer
	}
	if len(group.Items) > 0 && group.ItemLabel != "" {
		result.ItemCount = len(group.Items)
	}

	j.PrintJSON(result)
}

// PrintBatchResult renders a batch operation summary as structured JSON.
func (j *jsonFormatter) PrintBatchResult(result *BatchResult) {
	if j.config.quiet {
		return
	}

	errors := make([]jsonBatchError, len(result.Failed))
	for i, fail := range result.Failed {
		errors[i] = jsonBatchError{CID: fail.CID, Error: fail.Error}
	}

	j.PrintJSON(jsonBatchResult{
		Type:      jsonTypeBatchResult,
		Duration:  result.Duration.Round(time.Millisecond).String(),
		Total:     result.Total,
		Succeeded: len(result.Succeeded),
		Failed:    len(result.Failed),
		Skipped:   len(result.Skipped),
		Errors:    errors,
	})
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

	output.Printfln("Watching (Press Ctrl+C to stop)...")

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
				output.Print(title)
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
				output.Printfln("No items found")
				return nil
			}

			if allTerminal(rows) {
				output.Printfln("All items have reached terminal status")
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

// wordWrap wraps text at the specified width, breaking at word boundaries.
// Long words without break opportunities are hard-wrapped at the width limit.
func wordWrap(text string, width int) string {
	if width <= 0 {
		return text
	}

	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if len(line) <= width {
			lines = append(lines, line)
			continue
		}
		lines = append(lines, wrapLine(line, width)...)
	}
	return strings.Join(lines, "\n")
}

// wrapLine wraps a single line at the given width.
func wrapLine(line string, width int) []string {
	var result []string
	var cur strings.Builder
	curLen := 0

	for _, word := range strings.Fields(line) {
		wordLen := len(word)

		// Hard-wrap a word that exceeds the width by itself
		for wordLen > width {
			space := width - curLen
			if curLen > 0 && space > 1 {
				cur.WriteByte(' ')
				space--
				cur.WriteString(word[:space])
				result = append(result, cur.String())
				cur.Reset()
				curLen = 0
				word = word[space:]
				wordLen = len(word)
		} else {
			if curLen > 0 {
				result = append(result, cur.String())
				cur.Reset()
				curLen = 0
			}
			result = append(result, word[:width])
			word = word[width:]
			wordLen = len(word)
		}
		}

		if curLen+wordLen+1 > width && curLen > 0 {
			result = append(result, cur.String())
			cur.Reset()
			curLen = 0
		}

		if curLen > 0 {
			cur.WriteByte(' ')
			curLen++
		}
		cur.WriteString(word)
		curLen += wordLen
	}

	if cur.Len() > 0 {
		result = append(result, cur.String())
	}

	return result
}
