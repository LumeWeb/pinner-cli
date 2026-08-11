package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli/v3"
)

func TestCLICommandWrapperString(t *testing.T) {
	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "test", Value: "hello"},
		},
	}
	w := newCLICommandWrapper(cmd)
	assert.Equal(t, "hello", w.String("test"))
}

func TestCLICommandWrapperInt(t *testing.T) {
	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "count", Value: 42},
		},
	}
	w := newCLICommandWrapper(cmd)
	assert.Equal(t, 42, w.Int("count"))
}

func TestCLICommandWrapperBool(t *testing.T) {
	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "flag", Value: true},
		},
	}
	w := newCLICommandWrapper(cmd)
	assert.True(t, w.Bool("flag"))
}

func TestCLICommandWrapperUint64(t *testing.T) {
	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.Uint64Flag{Name: "size", Value: 1024},
		},
	}
	w := newCLICommandWrapper(cmd)
	assert.Equal(t, uint64(1024), w.Uint64("size"))
}

func TestCLICommandWrapperStringSlice(t *testing.T) {
	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: "tags", Value: []string{"a", "b"}},
		},
	}
	w := newCLICommandWrapper(cmd)
	assert.Equal(t, []string{"a", "b"}, w.StringSlice("tags"))
}

func TestCLICommandWrapperGetCID(t *testing.T) {
	cmd := &cli.Command{
		Action: func(ctx context.Context, cmd *cli.Command) error { return nil },
	}
	_ = cmd.Run(context.Background(), []string{"test", "bafybeig123"})
	w := newCLICommandWrapper(cmd)
	assert.Equal(t, "bafybeig123", w.GetCID())
}

func TestCLICommandWrapperArgs(t *testing.T) {
	cmd := &cli.Command{
		Action: func(ctx context.Context, cmd *cli.Command) error { return nil },
	}
	_ = cmd.Run(context.Background(), []string{"test", "arg1", "arg2"})
	w := newCLICommandWrapper(cmd)
	args := w.Args()
	assert.Equal(t, 2, args.Len())
	assert.Equal(t, "arg1", args.Get(0))
	assert.Equal(t, "arg2", args.Get(1))
}

func TestCLICommandWrapperImplementsInterfaces(t *testing.T) {
	cmd := &cli.Command{}
	w := newCLICommandWrapper(cmd)

	var _ flagGetter = w
	var _ flagGetterWithInt = w
	var _ flagGetterWithIsSet = w
	var _ argsGetter = w
	var _ cidGetter = w
	var _ argsFlagGetter = w
	var _ cidFlagGetter = w
}
