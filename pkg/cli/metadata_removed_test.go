package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMetadataRemovedCommand(t *testing.T) {
	cmd := newMetadataRemovedCommand()

	assert.Equal(t, "metadata", cmd.Name)
	assert.Equal(t, "Pinning", cmd.Category)
	assert.Contains(t, cmd.Usage, "REMOVED")
	assert.True(t, cmd.Hidden, "metadata command should be hidden")
	assert.NotNil(t, cmd.Action)
}

func TestMetadataRemovedAction(t *testing.T) {
	cmd := newMetadataRemovedCommand()

	err := cmd.Action(context.Background(), cmd)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "metadata")
	assert.Contains(t, err.Error(), "pins update")
}
