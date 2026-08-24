package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

func newAdminPprofCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdPprof,
		Usage: "Runtime profiling operations (admin)",
		Description: `Access Go runtime profiling data via pprof endpoints.

Profiles are binary data meant for consumption by 'go tool pprof'.
Redirect output to stdout, or save directly to a file with --output:

  pinner admin pprof heap > heap.prof && go tool pprof heap.prof
  pinner admin pprof cpu --output cpu.prof && go tool pprof cpu.prof
  pinner admin pprof trace > trace.out && go tool trace trace.out

Examples:
  pinner admin pprof status
  pinner admin pprof heap
  pinner admin pprof set-block-rate 1
  pinner admin pprof set-mutex-fraction 100`,
		Commands: []*cli.Command{
			newAdminPprofIndexCommand(),
			newAdminPprofBlockCommand(),
			newAdminPprofSetBlockRateCommand(),
			newAdminPprofCmdlineCommand(),
			newAdminPprofGoroutineCommand(),
			newAdminPprofHeapCommand(),
			newAdminPprofMutexCommand(),
			newAdminPprofSetMutexFractionCommand(),
			newAdminPprofCPUCommand(),
			newAdminPprofStatusCommand(),
			newAdminPprofSymbolCommand(),
			newAdminPprofThreadcreateCommand(),
			newAdminPprofTraceCommand(),
		},
	}
}

// pprofOutputFlag routes profile bytes to a file instead of stdout.
func pprofOutputFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    FlagOutput,
		Aliases: []string{"o"},
		Usage:   "Write the profile bytes to this file instead of stdout",
	}
}

// newAdminPprofProfileCommand builds a profile command that fetches binary
// profile data and writes it to --output or streams it to stdout. The profile
// commands share this shape; only the name, help text and fetch function vary.
func newAdminPprofProfileCommand(name, usage, desc string, fn func(ProfilingAdminService, context.Context) ([]byte, error)) *cli.Command {
	return &cli.Command{
		Name:        name,
		Usage:       usage,
		Description: desc,
		Flags:       []cli.Flag{pprofOutputFlag()},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPprofByteAction(ctx, cmd, output, cfgMgr, defaultProfilingAdminServiceFactory, fn)
		},
	}
}

func newAdminPprofIndexCommand() *cli.Command {
	return newAdminPprofProfileCommand(CmdIndex, "Show pprof index page",
		"Show the pprof index page listing available profiles.",
		func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) { return svc.GetProfileIndex(ctx) })
}

func newAdminPprofBlockCommand() *cli.Command {
	return newAdminPprofProfileCommand(CmdBlock, "Get block profile data",
		"Get block profile data for analyzing goroutine blocking events.",
		func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) { return svc.GetBlockProfile(ctx) })
}

func newAdminPprofCmdlineCommand() *cli.Command {
	return newAdminPprofProfileCommand(CmdCmdline, "Get command line of the running program",
		"Get the command line of the running program.",
		func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) { return svc.GetCmdline(ctx) })
}

func newAdminPprofGoroutineCommand() *cli.Command {
	return newAdminPprofProfileCommand(CmdGoroutine, "Get goroutine profile data",
		"Get stack traces of all current goroutines.",
		func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) { return svc.GetGoroutineProfile(ctx) })
}

func newAdminPprofHeapCommand() *cli.Command {
	return newAdminPprofProfileCommand(CmdHeap, "Get heap profile data",
		"Get a sampling of memory allocations of live objects.",
		func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) { return svc.GetHeapProfile(ctx) })
}

func newAdminPprofMutexCommand() *cli.Command {
	return newAdminPprofProfileCommand(CmdMutex, "Get mutex profile data",
		"Get stack traces of holders of contended mutexes.",
		func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) { return svc.GetMutexProfile(ctx) })
}

func newAdminPprofCPUCommand() *cli.Command {
	return newAdminPprofProfileCommand(CmdCPU, "Get CPU profile data",
		"Get a CPU profile for the default duration.",
		func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) { return svc.GetCPUProfile(ctx) })
}

func newAdminPprofSymbolCommand() *cli.Command {
	return newAdminPprofProfileCommand(CmdSymbol, "Get symbol lookup data",
		"Look up program counters and return function names.",
		func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) { return svc.GetSymbol(ctx) })
}

func newAdminPprofThreadcreateCommand() *cli.Command {
	return newAdminPprofProfileCommand(CmdThreadcreate, "Get thread creation profile data",
		"Get stack traces that led to the creation of new OS threads.",
		func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) { return svc.GetThreadcreate(ctx) })
}

func newAdminPprofTraceCommand() *cli.Command {
	return newAdminPprofProfileCommand(CmdTrace, "Get execution trace data",
		"Get an execution trace of the running program.",
		func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) { return svc.GetTrace(ctx) })
}

func newAdminPprofSetBlockRateCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdSetBlockRate,
		Usage: "Set block profiling rate",
		Description: `Set the block profiling rate. 0 disables, 1 captures all events, higher values sample.

Examples:
  pinner admin pprof set-block-rate 1
  pinner admin pprof set-block-rate 0`,
		ArgsUsage: "<rate>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPprofSetRateAction(ctx, cmd, output, cfgMgr, defaultProfilingAdminServiceFactory, "block profile rate",
				func(svc ProfilingAdminService, ctx context.Context, rate int) error {
					return svc.SetBlockProfileRate(ctx, rate)
				})
		},
	}
}

func newAdminPprofSetMutexFractionCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdSetMutexFraction,
		Usage: "Set mutex profiling fraction",
		Description: `Set the mutex profiling fraction. 0 disables, 1 captures all events, 100 samples 1%.

Examples:
  pinner admin pprof set-mutex-fraction 1
  pinner admin pprof set-mutex-fraction 0`,
		ArgsUsage: "<fraction>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPprofSetRateAction(ctx, cmd, output, cfgMgr, defaultProfilingAdminServiceFactory, "mutex profile fraction",
				func(svc ProfilingAdminService, ctx context.Context, rate int) error {
					return svc.SetMutexProfileFraction(ctx, rate)
				})
		},
	}
}

func newAdminPprofStatusCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdStatus,
		Usage: "Get current profiling configuration",
		Description: `Get the current block and mutex profiling rates.

Examples:
  pinner admin pprof status
  pinner admin pprof status --json`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPprofStatusAction(ctx, cmd, output, cfgMgr, defaultProfilingAdminServiceFactory)
		},
	}
}

func adminPprofByteAction(ctx context.Context, cmd flagGetter, output Output, cfgMgr config.Manager, serviceFactory ProfilingAdminServiceFactory, fn func(ProfilingAdminService, context.Context) ([]byte, error)) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetSyncTimeout())
	defer cancel()

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	data, err := fn(service, ctx)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if err := writePprofBytes(cmd.String(FlagOutput), data); err != nil {
		return err
	}
	return nil
}

// writePprofBytes writes profile data to the given path, or streams it to
// stdout when path is empty.
func writePprofBytes(path string, data []byte) error {
	if path != "" {
		return os.WriteFile(path, data, 0o644)
	}
	_, err := os.Stdout.Write(data)
	return err
}

func adminPprofSetRateAction(ctx context.Context, cmd argsGetter, output Output, cfgMgr config.Manager, serviceFactory ProfilingAdminServiceFactory, label string, fn func(ProfilingAdminService, context.Context, int) error) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetSyncTimeout())
	defer cancel()

	if cmd.Args().Len() < 1 {
		return fmt.Errorf("%s value is required", label)
	}

	rate, err := strconv.Atoi(cmd.Args().First())
	if err != nil {
		return fmt.Errorf("invalid %s value: %s", label, cmd.Args().First())
	}

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	if err := fn(service, ctx, rate); err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]bool{"success": true})
	}

	output.Printfln("%s set to %d successfully", label, rate)
	return nil
}

func adminPprofStatusAction(ctx context.Context, cmd argsGetter, output Output, cfgMgr config.Manager, serviceFactory ProfilingAdminServiceFactory) error {
	ctx, cancel := context.WithTimeout(ctx, cfgMgr.Config().GetSyncTimeout())
	defer cancel()

	service := serviceFactory(cfgMgr, output)
	if err := service.RequireAuthenticated(); err != nil {
		return err
	}

	status, err := service.GetStatus(ctx)
	if err != nil {
		output.PrintError(err)
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(status)
	}

	output.PrintFields(FieldGroup{
		Fields: []Field{
			{Label: "Block Profile Rate", Value: fmt.Sprintf("%d", status.BlockProfileRate)},
			{Label: "Mutex Fraction", Value: fmt.Sprintf("%d", status.MutexFraction)},
		},
	})
	return nil
}
