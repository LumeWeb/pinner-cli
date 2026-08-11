package main

import (
	"context"
	"fmt"
	"os"

	"go.lumeweb.com/pinner-cli/internal/cli"
)

func main() {
	if err := cli.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
