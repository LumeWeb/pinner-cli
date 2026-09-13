//go:build !no_tunnel

package main

import (
	"context"
	"fmt"
	"os"

	"go.lumeweb.com/pinner-cli/internal/cli"
	"go.lumeweb.com/pinner-cli/internal/cli/gitremote"
)

func main() {
	os.Exit(dispatch(context.Background(), os.Args, os.Stdin, os.Stdout, os.Stderr))
}

// dispatch routes the single `pinner` binary based on the basename it was
// invoked through (argv-0 identity dispatch — see gitremote.IsRemoteHelperInvocation).
//
//   - invoked as `git-remote-pinner` (the symlink to this binary): run the Git
//     remote-helper protocol, bypassing urfave CLI parsing entirely;
//   - invoked as `pinner` (or anything else): run the normal CLI command tree.
//
// Both paths share the gitarchive Store/session packages, but protocol dispatch
// is isolated here at the entry point. The normal CLI path never reads Git helper
// stdin and the helper path never enters the command tree.
func dispatch(ctx context.Context, args []string, in, out, errw *os.File) int {
	if gitremote.IsRemoteHelperInvocation(args[0]) {
		// argv[1] is the remote URL (e.g. `pinner::<locker>` for the account
		// archive, or `pinner::share/<URL>` for a profile-less shared clone).
		// NewStoreFromRemote routes between the profile-backed SessionStore and
		// the profile-less ShareStore accordingly.
		remoteURL := ""
		if len(args) >= 2 {
			remoteURL = args[1]
		}
		store, err := gitremote.NewStoreFromRemote(ctx, remoteURL)
		if err != nil {
			fmt.Fprintf(errw, "git-remote-pinner: %v\n", err)
			return 1
		}
		defer store.Close()
		if err := gitremote.Run(ctx, store, in, out, errw); err != nil {
			fmt.Fprintf(errw, "git-remote-pinner: %v\n", err)
			return 1
		}
		return 0
	}

	if err := cli.Run(ctx, args); err != nil {
		fmt.Fprintf(errw, "Error: %v\n", err)
		return 1
	}
	return 0
}
