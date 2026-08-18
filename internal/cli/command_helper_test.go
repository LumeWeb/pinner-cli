package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli/v3"
)

// TestAgentFlagImpliesJSON verifies that --json forces JSON output mode.
func TestAgentFlagImpliesJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantJSON bool
	}{
		{
			name:     "no flags: human output",
			args:     []string{"test"},
			wantJSON: false,
		},
		{
			name:     "--json: JSON output",
			args:     []string{"test", "--json"},
			wantJSON: true,
		},
		}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := &cli.Command{
				Name:  "test",
				Flags: GlobalFlags(),
				Action: func(ctx context.Context, c *cli.Command) error {
					jsonOutput := c.Bool(FlagJSON)
					assert.Equal(t, tt.wantJSON, jsonOutput)
					return nil
				},
			}
			err := cmd.Run(t.Context(), tt.args)
			assert.NoError(t, err)
		})
	}
}

// TestNewOutputFormatter_JSON verifies the output formatter returns
// jsonFormatter when JSON output (json=true) is active.
func TestNewOutputFormatter_JSON(t *testing.T) {
	t.Parallel()

	// json=true, so NewOutputFormatter(true,...) should return a jsonFormatter.
	formatter := NewOutputFormatter(true, false, false, false)
	assert.IsType(t, &jsonFormatter{}, formatter)
}
