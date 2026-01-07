package cli

import (
	"bufio"
	"os"
	"strings"

	"github.com/ipfs/boxo/pinning/remote/client"
	"github.com/pterm/pterm"
)

func readCIDsFromFile(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cids []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			cids = append(cids, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return cids, nil
}

func isStdinPipe() bool {
	stat, _ := os.Stdin.Stat()
	return (stat.Mode() & os.ModeCharDevice) == 0
}

// formatStatusWithColor adds color coding to the status text.
func formatStatusWithColor(status string) string {
	switch go_pinning_service_http_client.Status(status) {
	case go_pinning_service_http_client.StatusPinned:
		return pterm.FgGreen.Sprint(status)
	case go_pinning_service_http_client.StatusQueued, go_pinning_service_http_client.StatusPinning:
		return pterm.FgYellow.Sprint(status)
	case go_pinning_service_http_client.StatusFailed:
		return pterm.FgRed.Sprint(status)
	default:
		return status
	}
}

func readLinesFromStdin() ([]string, error) {
	var lines []string
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

// Dry run option key constants
const (
	DryRunOptionName            = "Name"
	DryRunOptionWait            = "Wait for completion"
	DryRunOptionParallel        = "Parallel operations"
	DryRunOptionContinueOnError = "Continue on error"
	DryRunOptionConfirm         = "Confirmation required"
	DryRunOptionInputType       = "Input type"
	DryRunOptionPath            = "Path"
	DryRunOptionMemoryLimit     = "Memory limit"
	DryRunOptionCID             = "CID"
	DryRunOptionAction          = "Action"
	DryRunOptionKey             = "Key"
	DryRunOptionCurrentValue    = "Current value"
	DryRunOptionNewValue        = "New value"
	DryRunOptionDescription     = "Description"
)

// DryRunPreview represents a dry-run preview configuration.
type DryRunPreview struct {
	Operation string
	Endpoint  string
	Items     []string
	ItemLabel string
	MaxItems  int
	Options   map[string]string
}

// DryRunOption creates an option entry map for dry-run previews.
func DryRunOption(key, value string) map[string]string {
	return map[string]string{key: value}
}

// RenderDryRun renders a dry-run preview in a consistent format.
func RenderDryRun(output Output, preview DryRunPreview) {
	output.Printfln("[DRY RUN] Preview of %s:", preview.Operation)
	if preview.Endpoint != "" {
		output.Printfln("  Endpoint: %s", preview.Endpoint)
	}
	if preview.Items != nil && len(preview.Items) > 0 {
		output.Printfln("  %s: %d", preview.ItemLabel, len(preview.Items))
		limit := preview.MaxItems
		if limit <= 0 {
			limit = 10
		}
		for i, item := range preview.Items {
			if i >= limit {
				output.Printfln("    ... and %d more", len(preview.Items)-limit)
				break
			}
			output.Printfln("    - %s", item)
		}
	}
	for key, value := range preview.Options {
		output.Printfln("  %s: %s", key, value)
	}
	output.Printfln("")
	output.Printfln("[DRY RUN] No changes were made. Remove --dry-run to execute.")
}
