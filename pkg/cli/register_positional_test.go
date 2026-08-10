package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// testArgs is a test cli.Args implementation that returns preset values.
type testArgs struct {
	values []string
}

func (a testArgs) Get(n int) string {
	if n >= 0 && n < len(a.values) {
		return a.values[n]
	}
	return ""
}
func (a testArgs) First() string {
	if len(a.values) > 0 {
		return a.values[0]
	}
	return ""
}
func (a testArgs) Tail() []string {
	if len(a.values) > 1 {
		return a.values[1:]
	}
	return nil
}
func (a testArgs) Len() int      { return len(a.values) }
func (a testArgs) Present() bool { return len(a.values) > 0 }
func (a testArgs) Slice() []string {
	out := make([]string, len(a.values))
	copy(out, a.values)
	return out
}

// testCmdWithArgs wraps a cli.Command with injected positional args.
type testCmdWithArgs struct {
	*cli.Command
	args cli.Args
}

func (w *testCmdWithArgs) Args() cli.Args { return w.args }

func newTestCmdWithArgs(c *cli.Command, args ...string) *testCmdWithArgs {
	return &testCmdWithArgs{Command: c, args: testArgs{values: args}}
}

func TestRegisterPositionalArgFallback(t *testing.T) {
	authService := NewMockAuthService(t)
	output := newTestOutput()

	// Positional email is used when --email flag is empty
	authService.EXPECT().Register(mock.Anything, "positional@example.com", "John", "Doe", "secret123").Return(nil)

	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: FlagEmail},
			&cli.StringFlag{Name: FlagFirstName, Value: "John"},
			&cli.StringFlag{Name: FlagLastName, Value: "Doe"},
			&cli.StringFlag{Name: FlagPassword, Value: "secret123"},
		},
	}

	wrapper := newTestCmdWithArgs(cmd, "positional@example.com")

	cfgMgrFactory := func() (config.Manager, error) { return newTestConfigMgr(t), nil }
	authServiceFactory := func(cfgMgr config.Manager, output Output, apiEndpoint string) AuthService {
		return authService
	}

	err := register(context.Background(), wrapper, output, cfgMgrFactory, authServiceFactory)
	require.NoError(t, err)
}

func TestRegisterEmailFlagTakesPrecedenceOverPositional(t *testing.T) {
	authService := NewMockAuthService(t)
	output := newTestOutput()

	// --email flag value should be used, not the positional arg
	authService.EXPECT().Register(mock.Anything, "flag@example.com", "John", "Doe", "secret123").Return(nil)

	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: FlagEmail, Value: "flag@example.com"},
			&cli.StringFlag{Name: FlagFirstName, Value: "John"},
			&cli.StringFlag{Name: FlagLastName, Value: "Doe"},
			&cli.StringFlag{Name: FlagPassword, Value: "secret123"},
		},
	}

	wrapper := newTestCmdWithArgs(cmd, "positional@example.com")

	cfgMgrFactory := func() (config.Manager, error) { return newTestConfigMgr(t), nil }
	authServiceFactory := func(cfgMgr config.Manager, output Output, apiEndpoint string) AuthService {
		return authService
	}

	err := register(context.Background(), wrapper, output, cfgMgrFactory, authServiceFactory)
	require.NoError(t, err)
}
