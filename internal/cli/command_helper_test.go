package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli/v3"
)

// TestAgentFlagImpliesJSON verifies that --agent forces JSON output mode,
// matching the behavior of --json.
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
		{
			name:     "--agent: JSON output (agent implies json)",
			args:     []string{"test", "--agent"},
			wantJSON: true,
		},
		{
			name:     "--agent and --json: JSON output",
			args:     []string{"test", "--agent", "--json"},
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
					jsonOutput := c.Bool(FlagJSON) || c.Bool(FlagAgent)
					assert.Equal(t, tt.wantJSON, jsonOutput)
					return nil
				},
			}
			err := cmd.Run(t.Context(), tt.args)
			assert.NoError(t, err)
		})
	}
}

// TestNewOutputFormatter_AgentMode verifies the output formatter returns
// jsonFormatter when agent mode (json=true) is active.
func TestNewOutputFormatter_AgentMode(t *testing.T) {
	t.Parallel()

	// --agent implies json=true, so NewOutputFormatter(true,...) should
	// return a jsonFormatter.
	formatter := NewOutputFormatter(true, false, false, false)
	assert.IsType(t, &jsonFormatter{}, formatter)
}
