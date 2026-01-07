package cli

import (
	"github.com/urfave/cli/v3"
)

// cliCommandWrapper wraps a *cli.Command to implement commandGetter interfaces.
type cliCommandWrapper struct {
	*cli.Command
}

func (w *cliCommandWrapper) GetCID() string {
	return w.Args().First()
}

func (w *cliCommandWrapper) String(name string) string {
	return w.Command.String(name)
}

func (w *cliCommandWrapper) Int(name string) int {
	return w.Command.Int(name)
}

func (w *cliCommandWrapper) Bool(name string) bool {
	return w.Command.Bool(name)
}

func (w *cliCommandWrapper) StringSlice(name string) []string {
	return w.Command.StringSlice(name)
}

func (w *cliCommandWrapper) Uint64(name string) uint64 {
	return w.Command.Uint64(name)
}

func (w *cliCommandWrapper) Args() cli.Args {
	return w.Command.Args()
}

// newCLICommandWrapper creates a new cliCommandWrapper from a *cli.Command.
func newCLICommandWrapper(c *cli.Command) *cliCommandWrapper {
	return &cliCommandWrapper{c}
}
