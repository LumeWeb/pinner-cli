package mcp

import (
	"os"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"
)

// mcpString resolves an explicitly supplied CLI flag before its service env key.
func mcpString(cmd *cli.Command, flag, envKey string) string {
	if cmd.IsSet(flag) {
		return cmd.String(flag)
	}
	if value := strings.TrimSpace(getenv(envKey)); value != "" {
		return value
	}
	return cmd.String(flag)
}

// mcpBool resolves an explicitly supplied CLI bool before its service env key.
func mcpBool(cmd *cli.Command, flag, envKey string) bool {
	if cmd.IsSet(flag) {
		return cmd.Bool(flag)
	}
	value := strings.TrimSpace(getenv(envKey))
	parsed, err := strconv.ParseBool(value)
	if err == nil {
		return parsed
	}
	return cmd.Bool(flag)
}

var getenv = os.Getenv
