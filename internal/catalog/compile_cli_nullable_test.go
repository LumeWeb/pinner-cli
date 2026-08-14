package catalog

import (
	"context"
	"testing"

	"github.com/urfave/cli/v3"
)

// runBoolCommand runs a single-flag command through urfave's parser with the
// given argv so flag presence (IsSet) reflects what was actually provided.
func runBoolCommand(t *testing.T, argv []string) *cli.Command {
	t.Helper()
	cmd := &cli.Command{
		Name:  "test",
		Flags: []cli.Flag{&cli.BoolFlag{Name: "dns-hosting"}},
		Action: func(ctx context.Context, c *cli.Command) error {
			return nil
		},
	}
	if err := cmd.Run(context.Background(), argv); err != nil {
		t.Fatalf("Run(%v): %v", argv, err)
	}
	return cmd
}

// TestCLIArgValueNullableBool pins the shared ArgType->CLI mapping for a
// nullable bool. Both the compiled command path (actionFor) and every wiring
// adapter (FlagValue) delegate to cliArgValue, so this single test covers the
// tri-state for the whole CLI surface: absent -> nil, --flag=true -> &true,
// --flag=false -> &false.
func TestCLIArgValueNullableBool(t *testing.T) {
	arg := OperationArg{Name: "dns-hosting", Type: ArgTypeNullableBool}

	// Absent: an unparsed command has IsSet=false for every flag, exercising
	// the tri-state "omitted" branch deterministically.
	cmd := &cli.Command{Name: "test", Flags: []cli.Flag{&cli.BoolFlag{Name: "dns-hosting"}}}
	if v, set, empty := cliArgValue(cmd, arg); set || empty || v != nil {
		t.Fatalf("absent: got (v=%#v set=%v empty=%v), want (nil,false,false)", v, set, empty)
	}

	// Present (explicit true, explicit false, and the bare form) must all
	// surface as *bool so the Handler can distinguish true from false.
	for _, tc := range []struct {
		flag string
		want bool
	}{
		{"--dns-hosting=true", true},
		{"--dns-hosting=false", false},
		{"--dns-hosting", true}, // bare form sets true
	} {
		cmd := runBoolCommand(t, []string{"test", tc.flag})
		v, set, empty := cliArgValue(cmd, arg)
		if !set || empty {
			t.Fatalf("%s: got (set=%v empty=%v), want (true,false)", tc.flag, set, empty)
		}
		p, ok := v.(*bool)
		if !ok {
			t.Fatalf("%s: got %T, want *bool", tc.flag, v)
		}
		if *p != tc.want {
			t.Fatalf("%s: got %v, want %v", tc.flag, *p, tc.want)
		}
	}
}
