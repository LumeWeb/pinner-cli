package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
)

func newAdminPprofCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdPprof,
		Usage: "Runtime profiling operations (admin)",
		Description: `Access Go runtime profiling data via pprof endpoints.

Profiles are binary data meant for consumption by 'go tool pprof'.
Redirect output to a file, then analyze:

  pinner admin pprof heap > heap.prof && go tool pprof heap.prof
  pinner admin pprof cpu > cpu.prof && go tool pprof cpu.prof
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

func newAdminPprofIndexCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdIndex,
		Usage: "Show pprof index page",
		Description: `Show the pprof index page listing available profiles.

Examples:
  pinner admin pprof index`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPprofByteAction(ctx, cmd, output, cfgMgr, defaultProfilingAdminServiceFactory,
				func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) {
					return svc.GetProfileIndex(ctx)
				})
		},
	}
}

func newAdminPprofBlockCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdBlock,
		Usage: "Get block profile data",
		Description: `Get block profile data for analyzing goroutine blocking events.

Examples:
  pinner admin pprof block > block.prof && go tool pprof block.prof`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPprofByteAction(ctx, cmd, output, cfgMgr, defaultProfilingAdminServiceFactory,
				func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) {
					return svc.GetBlockProfile(ctx)
				})
		},
	}
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

func newAdminPprofCmdlineCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdCmdline,
		Usage: "Get command line of the running program",
		Description: `Get the command line of the running program.

Examples:
  pinner admin pprof cmdline`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPprofByteAction(ctx, cmd, output, cfgMgr, defaultProfilingAdminServiceFactory,
				func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) {
					return svc.GetCmdline(ctx)
				})
		},
	}
}

func newAdminPprofGoroutineCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdGoroutine,
		Usage: "Get goroutine profile data",
		Description: `Get stack traces of all current goroutines.

Examples:
  pinner admin pprof goroutine > goroutine.prof && go tool pprof goroutine.prof`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPprofByteAction(ctx, cmd, output, cfgMgr, defaultProfilingAdminServiceFactory,
				func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) {
					return svc.GetGoroutineProfile(ctx)
				})
		},
	}
}

func newAdminPprofHeapCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdHeap,
		Usage: "Get heap profile data",
		Description: `Get a sampling of memory allocations of live objects.

Examples:
  pinner admin pprof heap > heap.prof && go tool pprof heap.prof`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPprofByteAction(ctx, cmd, output, cfgMgr, defaultProfilingAdminServiceFactory,
				func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) {
					return svc.GetHeapProfile(ctx)
				})
		},
	}
}

func newAdminPprofMutexCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdMutex,
		Usage: "Get mutex profile data",
		Description: `Get stack traces of holders of contended mutexes.

Examples:
  pinner admin pprof mutex > mutex.prof && go tool pprof mutex.prof`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPprofByteAction(ctx, cmd, output, cfgMgr, defaultProfilingAdminServiceFactory,
				func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) {
					return svc.GetMutexProfile(ctx)
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

func newAdminPprofCPUCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdCPU,
		Usage: "Get CPU profile data",
		Description: `Get a CPU profile for the default duration.

Examples:
  pinner admin pprof cpu > cpu.prof && go tool pprof cpu.prof`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPprofByteAction(ctx, cmd, output, cfgMgr, defaultProfilingAdminServiceFactory,
				func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) {
					return svc.GetCPUProfile(ctx)
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

func newAdminPprofSymbolCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdSymbol,
		Usage: "Get symbol lookup data",
		Description: `Look up program counters and return function names.

Examples:
  pinner admin pprof symbol`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPprofByteAction(ctx, cmd, output, cfgMgr, defaultProfilingAdminServiceFactory,
				func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) {
					return svc.GetSymbol(ctx)
				})
		},
	}
}

func newAdminPprofThreadcreateCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdThreadcreate,
		Usage: "Get thread creation profile data",
		Description: `Get stack traces that led to the creation of new OS threads.

Examples:
  pinner admin pprof threadcreate > threadcreate.prof && go tool pprof threadcreate.prof`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPprofByteAction(ctx, cmd, output, cfgMgr, defaultProfilingAdminServiceFactory,
				func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) {
					return svc.GetThreadcreate(ctx)
				})
		},
	}
}

func newAdminPprofTraceCommand() *cli.Command {
	return &cli.Command{
		Name:  CmdTrace,
		Usage: "Get execution trace data",
		Description: `Get an execution trace of the running program.

Examples:
  pinner admin pprof trace > trace.out && go tool trace trace.out`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgMgr, output, err := setupCommandContext(cmd)
			if err != nil {
				return err
			}
			return adminPprofByteAction(ctx, cmd, output, cfgMgr, defaultProfilingAdminServiceFactory,
				func(svc ProfilingAdminService, ctx context.Context) ([]byte, error) {
					return svc.GetTrace(ctx)
				})
		},
	}
}

func adminPprofByteAction(ctx context.Context, cmd argsGetter, output Output, cfgMgr config.Manager, serviceFactory ProfilingAdminServiceFactory, fn func(ProfilingAdminService, context.Context) ([]byte, error)) error {
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

	if output.IsJSON() {
		_, err = os.Stdout.Write(data)
		return err
	}

	_, err = os.Stdout.Write(data)
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
