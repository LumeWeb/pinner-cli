package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/cli/wizard"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

// configManagerFactory is the factory function used by setupCommandContext.
// It can be overridden in tests to inject mock config managers.
var configManagerFactory ConfigManagerFactory = defaultConfigManagerFactory

// setupCommandContext creates the common configuration and output objects needed by command actions.
// This reduces boilerplate code across command handlers.
func setupCommandContext(cmd *cli.Command) (config.Manager, Output, error) {
	cfgMgr, err := configManagerFactory()
	if err != nil {
		return nil, nil, err
	}

	// --agent implies --json (structured output, no ANSI colors).
	jsonOutput := cmd.Bool(FlagJSON) || cmd.Bool(FlagAgent)

	// --agent enables non-interactive mode: prompts return errors instead
	// of blocking on stdin. This prevents MCP/CI invocations from hanging
	// when a command requires interactive input that wasn't provided via flags.
	if cmd.Bool(FlagAgent) {
		SetAgentMode(true)
		wizard.NonInteractive = true
	}

	output := NewOutputFormatter(
		jsonOutput,
		cmd.Bool(FlagVerbose),
		cmd.Bool(FlagQuiet),
		cmd.Bool(FlagUnmask),
	)

	return cfgMgr, output, nil
}

// setupOutput creates the output formatter for command actions.
// This reduces boilerplate code across command handlers that only need output.
func setupOutput(cmd *cli.Command) Output {
	_, output, err := setupCommandContext(cmd)
	if err != nil || output == nil {
		fallback := NewOutputFormatter(false, false, false, false)
		fallback.SetWriter(io.Discard)
		return fallback
	}
	output.SetWriter(cmd.Root().Writer)
	return output
}

// flagChecker is satisfied by any command getter that supports IsSet.
type flagChecker interface {
	IsSet(name string) bool
}

// requireUpdateFields checks that at least one of the given flag names is set
// on the command. Returns an error listing the available flags if none are set.
func requireUpdateFields(cmd flagChecker, flags ...string) error {
	for _, f := range flags {
		if cmd.IsSet(f) {
			return nil
		}
	}
	return fmt.Errorf("at least one field must be provided for update (%s)", strings.Join(flags, ", "))
}

// intFlagChecker is satisfied by command getters that support IsSet and Int.
type intFlagChecker interface {
	IsSet(name string) bool
	Int(name string) int
}

// requireSetInt checks that the named int flag is both set and non-zero.
// IntFlag defaults to 0, so IsSet alone doesn't catch --flag 0.
func requireSetInt(cmd intFlagChecker, name string) (int, error) {
	if !cmd.IsSet(name) {
		return 0, fmt.Errorf("--%s is required", name)
	}
	v := cmd.Int(name)
	if v == 0 {
		return 0, fmt.Errorf("--%s must be greater than zero", name)
	}
	return v, nil
}

// commandContext carries all resolved dependencies for a command handler.
// It replaces the repeated setupCommandContext + GetAuthToken + GetSecureSetting + newCLICommandWrapper pattern.
type commandContext struct {
	Cmd       commandGetter
	Output    Output
	CfgMgr    config.Manager
	AuthToken string
	Secure    bool
}

// newCommandContext creates a commandContext from a *cli.Command.
// It resolves output, config, auth token, and secure settings in one call.
func newCommandContext(c *cli.Command) (*commandContext, error) {
	cfgMgr, output, err := setupCommandContext(c)
	if err != nil {
		return nil, err
	}
	authToken := GetAuthToken(c, cfgMgr)
	secure := GetSecureSetting(c, cfgMgr)
	cmd := newCLICommandWrapper(c)
	return &commandContext{
		Cmd:       cmd,
		Output:    output,
		CfgMgr:    cfgMgr,
		AuthToken: authToken,
		Secure:    secure,
	}, nil
}

// withContext wraps a handler that takes *commandContext into a cli.ActionFunc.
// This is the DRY mechanism for Action closures; replaces 5-line boilerplate with 2-3 lines.
func withContext(handler func(ctx context.Context, cc *commandContext) error) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		cc, err := newCommandContext(c)
		if err != nil {
			return err
		}
		return handler(ctx, cc)
	}
}
