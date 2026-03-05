package cli

import (
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

// setupCommandContext creates the common configuration and output objects needed by command actions.
// This reduces boilerplate code across command handlers.
func setupCommandContext(cmd *cli.Command) (config.Manager, Output, error) {
	cfgMgr, err := defaultConfigManagerFactory()
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
