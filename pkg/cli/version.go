package cli

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/build"
)

func init() {
	// Set custom version printer
	cli.VersionPrinter = printVersion
	
	// Set custom version flag with short alias
	cli.VersionFlag = &cli.BoolFlag{
		Name:    "version",
		Aliases: []string{"V"},
		Usage:   "print the version",
	}
}

func printVersion(cmd *cli.Command) {
	info := build.GetInfo()

	if cmd.Bool(FlagJSON) {
		jsonOutput, err := info.JSON()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error generating JSON: %v\n", err)
			fmt.Fprintln(os.Stdout, info.String())
			return
		}
		fmt.Fprintln(os.Stdout, jsonOutput)
		return
	}

	fmt.Fprintln(os.Stdout, info.String())
}
