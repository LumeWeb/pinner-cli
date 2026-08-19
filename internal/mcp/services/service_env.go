package services

import (
	"os"

	"go.lumeweb.com/pinner-cli/internal/service"
)

const defaultMCPEnvFileName = "mcp.env"

// ServiceEnvironment is the managed MCP service's env representation. It is an
// alias for the service package's Environment type, which owns parsing,
// loading, and writing of the KEY=VALUE env file shared by all backends.
type ServiceEnvironment = service.Environment

// getenv is the process env lookup seam, overridable in tests to control the
// "current" value serviceEnvValue falls back to when the service env file omits
// a key.
var getenv = os.Getenv

func serviceEnvValue(env ServiceEnvironment, key string, current string) string {
	if value, ok := env[key]; ok && value != "" {
		return value
	}
	return current
}
