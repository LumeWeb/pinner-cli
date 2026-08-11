package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockArgsPresent(t *testing.T) {
	empty := &mockArgs{args: []string{}}
	assert.False(t, empty.Present())

	nonEmpty := &mockArgs{args: []string{"a"}}
	assert.True(t, nonEmpty.Present())
}

func TestMockArgsTail(t *testing.T) {
	empty := &mockArgs{args: []string{}}
	assert.Empty(t, empty.Tail())

	single := &mockArgs{args: []string{"a"}}
	assert.Empty(t, single.Tail())

	multi := &mockArgs{args: []string{"a", "b", "c"}}
	assert.Equal(t, []string{"b", "c"}, multi.Tail())
}

func TestMockCommandWithInt64(t *testing.T) {
	cmd := newMockCommand().withInt64("size", 1024)
	assert.Equal(t, int64(1024), cmd.Int64("size"))
}
