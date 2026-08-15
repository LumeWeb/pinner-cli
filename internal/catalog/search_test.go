package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSearchArg reads the standard search input key with the usual default.
func TestSearchArg(t *testing.T) {
	assert.Equal(t, "", SearchArg(map[string]any{}))
	assert.Equal(t, "", SearchArg(map[string]any{"search": ""}))
	assert.Equal(t, "docs", SearchArg(map[string]any{"search": "docs"}))
}
