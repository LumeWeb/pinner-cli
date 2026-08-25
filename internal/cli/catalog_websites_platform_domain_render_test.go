package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"

	"go.lumeweb.com/pinner-cli/internal/catalog"
)

// TestRenderWebsitesPlatformDomainAvailabilityResult verifies renderWebsitesResult
// handles the *ipfs.PlatformAvailabilityResponse type returned by the
// websites_platform_domain_availability handler. Without a case it fell through
// to the default "unroutable result type" error, breaking every human-readable
// `pinner websites platform-domain availability` invocation (JSON/MCP worked
// because it JSON-encodes the result directly).
func TestRenderWebsitesPlatformDomainAvailabilityResult(t *testing.T) {
	t.Run("renders results as a table", func(t *testing.T) {
		var buf bytes.Buffer
		op := catalog.NewOperation(catalog.OperationSpec{Name: "websites_platform_domain_availability"})
		resp := &ipfs.PlatformAvailabilityResponse{
			Label: "my-app",
			Results: []ipfs.PlatformAvailabilityResult{
				{Available: true, Namespace: "pinner", PlatformDomain: "my-app.pinner.xyz"},
				{Available: false, Namespace: "other", PlatformDomain: "my-app.other.xyz"},
			},
		}

		cmd := &cli.Command{
			Name:   "availability",
			Writer: &buf,
			Action: func(ctx context.Context, c *cli.Command) error {
				return renderWebsitesResult(ctx, c, op, resp)
			},
		}

		if err := cmd.Run(t.Context(), []string{"availability"}); err != nil {
			t.Fatalf("run: unexpected error: %v", err)
		}
		got := buf.String()
		for _, want := range []string{
			"Platform Domain Availability",
			"Label: my-app",
			"my-app.pinner.xyz",
			"true",
			"my-app.other.xyz",
			"false",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("output %q does not contain %q", got, want)
			}
		}
	})

	t.Run("empty results renders empty state", func(t *testing.T) {
		var buf bytes.Buffer
		op := catalog.NewOperation(catalog.OperationSpec{Name: "websites_platform_domain_availability"})
		resp := &ipfs.PlatformAvailabilityResponse{Label: "", Results: nil}

		cmd := &cli.Command{
			Name:   "availability",
			Writer: &buf,
			Action: func(ctx context.Context, c *cli.Command) error {
				return renderWebsitesResult(ctx, c, op, resp)
			},
		}

		if err := cmd.Run(t.Context(), []string{"availability"}); err != nil {
			t.Fatalf("run: unexpected error: %v", err)
		}
		got := buf.String()
		if !strings.Contains(got, "No platform domains found") {
			t.Fatalf("output %q does not contain empty-state message", got)
		}
	})
}
