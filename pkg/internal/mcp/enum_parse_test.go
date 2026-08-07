package mcp_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	mcpadapter "go.lumeweb.com/pinner-cli/pkg/internal/mcp"
)

func TestEnumStringFlagRealParse(t *testing.T) {
	var got string
	cmd := &cli.Command{
		Name: "cancel",
		Flags: []cli.Flag{
			mcpadapter.EnumStringFlag("mode", "cancel mode", false, "end_of_billing_period", "immediate", "end_of_billing_period"),
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			got = c.String("mode")
			return nil
		},
	}
	err := cmd.Run(t.Context(), []string{"cancel", "--mode", "immediate"})
	require.NoError(t, err)
	require.Equal(t, "immediate", got)

	// Default value applies when the flag is omitted.
	err = cmd.Run(t.Context(), []string{"cancel"})
	require.NoError(t, err)
	require.Equal(t, "end_of_billing_period", got)
}
