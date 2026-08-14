package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// fakeAccountDeps returns a hermetic AccountDeps whose subscription operation
// derives a canned web URL with no real config or network, backed by a mocked
// auth service that returns a known subscription status.
func fakeAccountDeps(t *testing.T) catalogops.AccountDeps {
	cfgMgr := newTestConfigMgr(t)
	authService := NewMockAuthService(t)
	authService.EXPECT().GetSubscriptionStatus(mock.Anything).
		Return(nil, nil)
	return catalogops.AccountDeps{
		CfgMgr: func() config.Manager { return cfgMgr },
		AuthService: func(cfgMgr config.Manager, token string) auth.AuthService {
			return authService
		},
		PortalURL: func(cfgMgr config.Manager) string {
			return "https://account.pinner.xyz/account/subscription"
		},
	}
}

// buildSubscriptionCmd assembles the account_subscription catalog operation
// wrapped by the same accountActionAdapter used in production, with the CLI-only
// --open flag appended (as accountWiringParent does).
func buildSubscriptionCmd(t *testing.T) *cli.Command {
	t.Helper()
	var op catalog.Operation
	for _, o := range catalogops.AccountOperations(fakeAccountDeps(t)) {
		if o.Name() == "account_subscription" {
			op = o
			break
		}
	}
	if op == nil {
		t.Fatal("account_subscription operation not found")
	}
	cmd := &cli.Command{Name: "account_subscription"}
	cmd.Action = accountActionAdapter(op)
	cmd.Flags = append(cmd.Flags, &cli.BoolFlag{Name: "open"})
	return cmd
}

// runSubscription runs account_subscription under a minimal root command,
// capturing stdout into buf. The extra args (e.g. --json, --open) are appended.
func runSubscription(t *testing.T, extra ...string) ([]byte, error) {
	t.Helper()
	// Ensure setupCommandContext gets a valid config manager so the JSON
	// formatter path is selected based on the --json flag.
	cfgMgr := newTestConfigMgr(t)
	orig := configManagerFactory
	configManagerFactory = func() (config.Manager, error) { return cfgMgr, nil }
	t.Cleanup(func() { configManagerFactory = orig })

	sub := buildSubscriptionCmd(t)
	root := &cli.Command{
		Name: "pinner",
		// --json is a global flag (flags.go) that setupCommandContext reads to
		// select the JSON formatter; define it on the root so the leaf command
		// inherits it during Root().Run lookup.
		Flags:    []cli.Flag{&cli.BoolFlag{Name: FlagJSON}},
		Commands: []*cli.Command{sub},
	}
	var buf bytes.Buffer
	root.Writer = &buf

	args := append([]string{"pinner", "account_subscription"}, extra...)
	err := root.Run(context.Background(), args)
	return buf.Bytes(), err
}

// TestAccountSubscriptionOpenJSONSuppressesMessages pins the wiring contract:
// with --json and --open, human-readable browser chatter must NOT appear on
// stdout; the output must be a single valid JSON object carrying the web_url
// deep-link (the account info arrives as data, and the human opens the link).
func TestAccountSubscriptionOpenJSONSuppressesMessages(t *testing.T) {
	out, err := runSubscription(t, "--json", "--open")
	require.NoError(t, err, "account_subscription --json --open")
	var v map[string]any
	require.NoError(t, json.Unmarshal(out, &v), "output is not valid JSON: %s", string(out))
	require.Equal(t, "https://account.pinner.xyz/account/subscription", v["web_url"])
	require.False(t, v["is_subscribed"].(bool))
}

// TestAccountSubscriptionOpenWithoutJSON still completes in human mode (browser
// open is best-effort and non-fatal when no opener is available).
func TestAccountSubscriptionOpenWithoutJSON(t *testing.T) {
	out, err := runSubscription(t, "--open")
	require.NoError(t, err, "account_subscription --open")
	// Human mode should still print the URL/management message.
	require.NotEmpty(t, bytes.TrimSpace(out), "expected human-readable output in non-JSON mode")
}
