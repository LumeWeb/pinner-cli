package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServiceEnvValue(t *testing.T) {
	require.Equal(t, "override", serviceEnvValue(ServiceEnvironment{"KEY": "override"}, "KEY", "current"))
	require.Equal(t, "current", serviceEnvValue(ServiceEnvironment{}, "KEY", "current"))
}
