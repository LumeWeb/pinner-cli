package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// TestAccountCommandTree_CompiledLeaves pins the account command-tree shape
// (internal/clicatalog/shapes_account.go): the account parent must merge the
// hand-written otp/api-keys parents with the catalog-compiled leaves in
// deterministic order, and must NOT expose a flat `otp-disable` leaf (the
// disable op nests under the hand-written `otp` parent).
func TestAccountCommandTree_CompiledLeaves(t *testing.T) {
	account := newAccountCommand()
	names := commandNames(account.Commands)
	// Hand-written parents come first, then the catalog-compiled leaves in
	// AccountOperations declaration order (info, update-email, update-password,
	// subscription, quota) with the excluded disable op absent.
	want := []string{
		"otp", "api-keys",
		"info", "update-email", "update-password", "subscription", "quota",
	}
	require.Equal(t, want, names, "account subcommand order/names")

	// No flat otp-disable leaf anywhere (the op is ExcludeFromFlatMount).
	require.Nil(t, findCommand(account.Commands, "otp-disable"))
}

// TestAccountCommandTree_OpenFlagInjection pins the CLI-only --open convenience
// injection: it must be present on the web-URL read commands (subscription and
// quota) and absent elsewhere. This is mount-owned flag injection in
// buildAccountLeaf, not part of the shared leaf builder.
func TestAccountCommandTree_OpenFlagInjection(t *testing.T) {
	account := newAccountCommand()
	for _, leafName := range []string{"subscription", "quota"} {
		leaf := findCommand(account.Commands, leafName)
		require.NotNil(t, leaf, "account %s leaf should exist", leafName)
		require.Contains(t, getFlagNames(leaf), "open", "account %s should carry --open", leafName)
	}
	// A non-URL leaf must not carry --open.
	info := findCommand(account.Commands, "info")
	require.NotNil(t, info)
	require.NotContains(t, getFlagNames(info), "open", "account info should not have --open")
}

// TestAccountCommandTree_UpdateEmailFlags preserves the sensitive --password
// flag on the update-email leaf (catalog-compiled). The new email is accepted
// both via the compiled --email flag and positionally (the account config's
// ResolvePositional maps a positional <email> when the arg is empty), so the
// required marker is relaxed to let the positional path reach the handler.
func TestAccountCommandTree_UpdateEmailFlags(t *testing.T) {
	account := newAccountCommand()
	leaf := findCommand(account.Commands, "update-email")
	require.NotNil(t, leaf, "account update-email leaf should exist")
	flags := getFlagNames(leaf)
	require.Contains(t, flags, "password", "update-email should carry --password")
	require.Contains(t, flags, "email", "update-email should carry the compiled --email flag")
	// The email/password flags must not be urfave-required so the positional
	// <email> path (ResolvePositional) can run before the handler.
	requireFlagNotRequired(t, leaf, "email")
	requireFlagNotRequired(t, leaf, "password")
}

// TestAccountCommandTree_OTPDisableStillNested pin that the catalog-driven otp
// disable flow remains under the hand-written `otp` parent (not a flat leaf).
func TestAccountCommandTree_OTPDisableStillNested(t *testing.T) {
	account := newAccountCommand()
	otp := findCommand(account.Commands, "otp")
	require.NotNil(t, otp, "account otp parent should exist")
	disable := findCommand(otp.Commands, "disable")
	require.NotNil(t, disable, "account otp disable should exist under the otp parent")
	// The disable command is catalog-wired: it carries an Action.
	require.NotNil(t, disable.Action, "account otp disable should have a catalog-wired Action")
	flags := getFlagNames(disable)
	require.Contains(t, flags, "password", "otp disable should carry --password")
}

// requireFlagNotRequired asserts the named flag exists and is not marked
// urfave-required (relaxFlagRequired clears it so positional/flag-absent paths
// run before the handler).
func requireFlagNotRequired(t *testing.T, cmd *cli.Command, name string) {
	t.Helper()
	for _, f := range cmd.Flags {
		if f.Names()[0] != name {
			continue
		}
		require.False(t, f.(*cli.StringFlag).Required, "--%s should not be urfave-required", name)
		return
	}
	t.Fatalf("flag --%s not found on %s", name, cmd.Name)
}
