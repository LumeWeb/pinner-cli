package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStdinSourceString(t *testing.T) {
	s := &StdinSource{}
	assert.Equal(t, "stdin", s.String())
}

func TestStdinSourceGoString(t *testing.T) {
	s := &StdinSource{}
	assert.Equal(t, "cli.StdinSource", s.GoString())
}

func TestNewStdinSource(t *testing.T) {
	s := NewStdinSource()
	assert.NotNil(t, s)
}

func TestStdin(t *testing.T) {
	s := Stdin()
	assert.NotNil(t, s)
	assert.IsType(t, &StdinSource{}, s)
}
