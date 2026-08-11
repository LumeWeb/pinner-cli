package cli

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/build"
)

func TestVersionFlagConfiguration(t *testing.T) {
	assert.NotNil(t, cli.VersionFlag)
	assert.Equal(t, "version", cli.VersionFlag.Names()[0])

	names := cli.VersionFlag.Names()
	assert.Contains(t, names, "V", "version flag should have -V alias")
}

func TestVersionPrinterSet(t *testing.T) {
	assert.NotNil(t, cli.VersionPrinter, "VersionPrinter should be set in init()")
}

func TestPrintVersionHuman(t *testing.T) {
	old := build.Default
	defer func() { build.Default = old }()

	build.Default = build.New("1.2.3", "abc12345", "main", "", "go1.26", "linux", "amd64")

	cmd := &cli.Command{}

	var buf bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cli.VersionPrinter(cmd)

	_ = w.Close()
	os.Stdout = oldStdout
	_, _ = buf.ReadFrom(r)

	output := buf.String()
	require.Contains(t, output, "1.2.3")
}

func TestPrintVersionJSON(t *testing.T) {
	old := build.Default
	defer func() { build.Default = old }()

	build.Default = build.New("2.0.0", "deadbeef", "main", "", "go1.26", "linux", "amd64")

	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: FlagJSON, Value: true},
		},
	}

	var buf bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cli.VersionPrinter(cmd)

	_ = w.Close()
	os.Stdout = oldStdout
	_, _ = buf.ReadFrom(r)

	output := buf.String()
	require.Contains(t, output, "2.0.0")
	require.Contains(t, output, "deadbeef")
}

func TestNewVersionCommand(t *testing.T) {
	root := NewRootCommand()
	assert.NotEmpty(t, root.Version, "root command should have a version set")
}
