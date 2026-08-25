package cli

import "fmt"

// formatBytes renders a byte count, or "unlimited" for a negative value. Used
// by the vault and admin renderers.
func formatBytes(bytes int) string {
	if bytes < 0 {
		return "unlimited"
	}
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := int64(bytes) / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
