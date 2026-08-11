package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

// parseVaultExpiry parses a duration string like "7d", "30d", "1h", "0" (never).
func parseVaultExpiry(s string) (time.Time, error) {
	if s == "0" || s == "never" {
		return time.Now().AddDate(100, 0, 0), nil
	}
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid expiry: %s", s)
		}
		if days <= 0 {
			return time.Time{}, fmt.Errorf("expiry days must be positive: %s", s)
		}
		return time.Now().AddDate(0, 0, days), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid expiry format: %w (use e.g. 7d, 30d, 1h, 0 for never)", err)
	}
	if d <= 0 {
		return time.Time{}, fmt.Errorf("expiry must be in the future: %s", s)
	}
	return time.Now().Add(d), nil
}

func newVaultShareCommand() *cli.Command {
	return &cli.Command{
		Name:      "share",
		Usage:     "Generate a shareable link for a vault file",
		ArgsUsage: vaultArgsUsage,
		Description: `Generate a shareable download link for a vault file. Returns the share URL and its expiry time.

Control the expiry with --expiry (e.g. 7d, 30d, 1h, or 0 for never). Does NOT upload or modify the file itself.`,
		Flags: []cli.Flag{VaultExpiryFlag()},
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			vaultPath := c.Args().First()
			if vaultPath == "" {
				return fmt.Errorf("vault path required")
			}

			svc, _, err := vaultServiceForCommand(c)
			if err != nil {
				return err
			}
			defer svc.Close()

			validUntil, err := parseVaultExpiry(c.String(FlagExpiry))
			if err != nil {
				return err
			}

			shareURL, err := svc.Share(ctx, vaultPath, validUntil)
			if err != nil {
				return err
			}

			if output.IsJSON() {
				output.PrintJSON(vaultShareResponse{
					ShareURL: shareURL,
					Expires:  validUntil.Format(time.RFC3339),
				})
			} else {
				// stdout: only the share URL for piping/clipboard. The expiry
				// status goes to stderr (honoring --quiet) so
				// `vault share f | pbcopy` stays clean and quiet-mode output
				// is fully suppressed.
				fmt.Println(shareURL)
				if !c.Bool(FlagQuiet) {
					fmt.Fprintf(os.Stderr, "Share link expires: %s\n", validUntil.Format(time.RFC3339))
				}
			}
			return nil
		},
	}
}
