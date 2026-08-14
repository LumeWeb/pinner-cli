package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// fakeAccountDeps returns a hermetic AccountDeps whose portal operation derives
// a canned web URL with no real config, auth, or network.
func fakeAccountDeps(t *testing.T) catalogops.AccountDeps {
	cfgMgr := newTestConfigMgr(t)
	return catalogops.AccountDeps{
		CfgMgr: func() config.Manager { return cfgMgr },
		PortalURL: func(cfgMgr config.Manager) string {
			return "https://account.pinner.xyz/account/subscription"
		},
	}
}

// buildPortalCmd assembles the account_portal catalog operation wrapped by the
// same accountActionAdapter used in production, with the CLI-only --open flag
// appended (as accountWiringParent does).
func buildPortalCmd(t *testing.T) *cli.Command {
	t.Helper()
	var op catalog.Operation
	for _, o := range catalogops.AccountOperations(fakeAccountDeps(t)) {
		if o.Name() == "account_portal" {
			op = o
			break
		}
	}
	if op == nil {
		t.Fatal("account_portal operation not found")
	}
	cmd := &cli.Command{Name: "account_portal"}
	cmd.Action = accountActionAdapter(op)
	cmd.Flags = append(cmd.Flags, &cli.BoolFlag{Name: "open"})
	return cmd
}

// runPortal runs account_portal under a minimal root command, capturing stdout
// into buf. The extra args (e.g. --json, --open) are appended.
func runPortal(t *testing.T, extra ...string) ([]byte, error) {
	t.Helper()
	// Ensure setupCommandContext gets a valid config manager so the JSON
	// formatter path is selected based on the --json flag.
	cfgMgr := newTestConfigMgr(t)
	orig := configManagerFactory
	configManagerFactory = func() (config.Manager, error) { return cfgMgr, nil }
	t.Cleanup(func() { configManagerFactory = orig })

	portal := buildPortalCmd(t)
	root := &cli.Command{
		Name: "pinner",
		// --json is a global flag (flags.go) that setupCommandContext reads to
		// select the JSON formatter; define it on the root so the leaf command
		// inherits it during Root().Run lookup.
		Flags:    []cli.Flag{&cli.BoolFlag{Name: FlagJSON}},
		Commands: []*cli.Command{portal},
	}
	var buf bytes.Buffer
	root.Writer = &buf

	args := append([]string{"pinner", "account_portal"}, extra...)
	err := root.Run(context.Background(), args)
	return buf.Bytes(), err
}

// TestAccountPortalOpenJSONSuppressesMessages pins the Kody fix: with --json
// and --open, human-readable browser chatter must NOT appear on stdout; the
// output must be a single valid JSON object carrying the url.
func TestAccountPortalOpenJSONSuppressesMessages(t *testing.T) {
	out, err := runPortal(t, "--json", "--open")
	if err != nil {
		t.Fatalf("account_portal --json --open: %v", err)
	}
	var v map[string]any
	if uerr := json.Unmarshal(out, &v); uerr != nil {
		t.Fatalf("output is not valid JSON (%v). Raw output:\n%s", uerr, string(out))
	}
	if v["url"] != "https://account.pinner.xyz/account/subscription" {
		t.Fatalf("unexpected url in JSON result: %#v", v["url"])
	}
}

// TestAccountPortalOpenWithoutJSON still completes in human mode (browser open
// is best-effort and non-fatal when no opener is available).
func TestAccountPortalOpenWithoutJSON(t *testing.T) {
	out, err := runPortal(t, "--open")
	if err != nil {
		t.Fatalf("account_portal --open: %v", err)
	}
	// Human mode should still print the URL/management message.
	if len(bytes.TrimSpace(out)) == 0 {
		t.Fatal("expected human-readable output in non-JSON mode")
	}
}
