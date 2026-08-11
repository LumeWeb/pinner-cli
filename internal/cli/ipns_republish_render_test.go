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

// TestRenderIPNSRepublishResult verifies renderIPNSResult handles the
// IPNSRepublishResponse type returned by the ipns.republish handler. Without
// a case it fell through to the default "unroutable result type" error,
// making every `pinner ipns republish <key>` fail at render time.
func TestRenderIPNSRepublishResult(t *testing.T) {
	var buf bytes.Buffer
	op := catalog.NewOperation(catalog.OperationSpec{Name: "ipns.republish"})
	resp := &ipfs.IPNSRepublishResponse{Count: 3, Message: "record refreshed"}

	cmd := &cli.Command{
		Name:       "republish",
		ArgsUsage:  "<key-name-or-id>",
		Flags:      []cli.Flag{&cli.StringFlag{Name: "key-name"}},
		Writer:     &buf,
		Action: func(ctx context.Context, c *cli.Command) error {
			return renderIPNSResult(ctx, c, op, resp)
		},
	}

	// Positional <key> form.
	if err := cmd.Run(t.Context(), []string{"republish", "my-key"}); err != nil {
		t.Fatalf("run (positional): unexpected error: %v", err)
	}
	got := buf.String()
	want := "Republished IPNS key my-key: record refreshed (3 record(s))"
	if !strings.Contains(got, want) {
		t.Fatalf("positional output %q does not contain %q", got, want)
	}

	// Flag-only --key-name form (ipns.republish declares it as a flag arg).
	buf.Reset()
	if err := cmd.Run(t.Context(), []string{"republish", "--key-name", "flag-key"}); err != nil {
		t.Fatalf("run (flag): unexpected error: %v", err)
	}
	got = buf.String()
	want = "Republished IPNS key flag-key: record refreshed (3 record(s))"
	if !strings.Contains(got, want) {
		t.Fatalf("flag output %q does not contain %q", got, want)
	}
}
