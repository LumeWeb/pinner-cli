package cli

import (
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
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

	output := NewOutputFormatter(
		cmd.Bool(FlagJSON),
		cmd.Bool(FlagVerbose),
		cmd.Bool(FlagQuiet),
		cmd.Bool(FlagUnmask),
	)

	return cfgMgr, output, nil
}

// setupOutput creates the output formatter for command actions.
// This reduces boilerplate code across command handlers that only need output.
func setupOutput(cmd *cli.Command) Output {
	_, output, _ := setupCommandContext(cmd)
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
