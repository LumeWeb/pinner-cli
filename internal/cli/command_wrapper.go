package cli

import (
	"time"

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

func (w *cliCommandWrapper) Uint(name string) uint {
	return w.Command.Uint(name)
}

func (w *cliCommandWrapper) Duration(name string) time.Duration {
	return w.Command.Duration(name)
}

// emptyArgs is a nil-safe cli.Args implementation with no arguments.
type emptyArgs struct{}

func (emptyArgs) Get(int) string  { return "" }
func (emptyArgs) First() string   { return "" }
func (emptyArgs) Tail() []string  { return nil }
func (emptyArgs) Len() int        { return 0 }
func (emptyArgs) Present() bool   { return false }
func (emptyArgs) Slice() []string { return nil }

func (w *cliCommandWrapper) Args() cli.Args {
	args := w.Command.Args()
	if args == nil {
		return emptyArgs{}
	}
	return args
}

// newCLICommandWrapper creates a new cliCommandWrapper from a *cli.Command.
func newCLICommandWrapper(c *cli.Command) *cliCommandWrapper {
	return &cliCommandWrapper{c}
}

// Compile-time interface satisfaction check.
var _ commandGetter = (*cliCommandWrapper)(nil)
