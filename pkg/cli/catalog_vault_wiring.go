package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// catalog_vault_wiring.go is the pkg/cli frontend adapter for the vault domain
// operations in internal/catalogops. It compiles the catalog's vault
// operations into real urfave/cli/v3 commands nested under the "vault" parent
// command, renders every handler's typed DATA result through the CLI's Output
// formatter, and maps positional args / profile selection / the destructive
// force-gate onto the operation inputs.
//
// Architectural split (mirrors the pins pilot in catalog_wiring.go):
//   - internal/catalogops exposes VaultDeps + VaultOperations; it never renders
//     and never imports pkg/cli.
//   - This file is where IO/CLI concerns live: positional path/name mapping,
//     profile resolution, the destructive --force gate, and all result
//     rendering.
//
// Name mapping: canonical catalog names use dots ("vault.status"). We strip the
// "vault." group prefix and mount leaves under a "vault" parent; two-level ops
// ("vault.profile.use", "vault.cache.rebuild", "vault.cache.clear") are nested
// under their "profile"/"cache" parent commands.
//
// The commands that are fundamentally interactive/IO (create, restore, cp, cat)
// are NOT compiled from the catalog — see catalogops.VaultOperations for why —
// and are appended to the vault parent as hand-written commands.

// vaultCatalogDeps lazily builds the catalogops.VaultDeps from the live CLI
// wiring. The service getter reads the (test-overridable) vaultServiceFactory
// var per invocation so tests that swap it keep working; the indexer URL is
// resolved from config per invocation (never at package init).
func vaultCatalogDeps() catalogops.VaultDeps {
	return catalogops.VaultDeps{
		Service: func(profileName, indexerURL string) (vault.VaultService, error) {
			return vaultServiceFactory(profileName, indexerURL)
		},
		ResolveIndexerURL: func() string {
			cfgMgr, err := configManagerFactory()
			if err != nil {
				return ""
			}
			return cfgMgr.Config().GetSiaIndexerURL()
		},
	}
}

// vaultCatalogDepsVar is an indirection so the wiring and the renderer can both
// reach the canonical operation list without rebuilding it repeatedly.
var vaultCatalogDepsVar = catalogops.VaultDeps(vaultCatalogDeps())

// vaultParentUsage returns the CLI Usage line for a two-level vault parent
// command (e.g. "profile", "cache"), kept non-empty to satisfy registration
// assertions and give users a one-line summary.
func vaultParentUsage(parent string) string {
	switch parent {
	case "profile":
		return "Manage vault profiles"
	case "cache":
		return "Manage the vault SQLite cache"
	default:
		return "Manage vault " + parent
	}
}

// newVaultCatalogCommands compiles the vault catalog operations and returns
// the top-level "vault" subcommands they produce (status, ls, stat, verify,
// rm, sync, share, forget, profile→use, cache→rebuild/clear). The interactive/
// IO hand-written commands (create, restore, cp, cat) are appended by
// newVaultCommand.
func newVaultCatalogCommands() []*cli.Command {
	cat := catalog.NewCatalog()
	ops := catalogops.VaultOperations(vaultCatalogDepsVar)
	for _, op := range ops {
		_ = cat.Add(op)
	}

	compiler := catalog.NewCLICompiler()
	compiled, err := compiler.Compile(cat)
	if err != nil {
		// Compilation of well-formed catalog operations cannot fail; if it
		// does we must not silently skip the vault group.
		panic(fmt.Sprintf("catalog compile vault: %v", err))
	}

	// Leaf commands compiled flat with dotted names; nest the two-level ones.
	parents := map[string]*cli.Command{}
	var out []*cli.Command
	for _, c := range compiled {
		canonical := c.Name // e.g. "vault.cache.rebuild", BEFORE mount mutates it
		mounted := mountVaultCatalogCommand(c)
		rest := strings.TrimPrefix(canonical, "vault.")
		if idx := strings.Index(rest, "."); idx > 0 {
			// Two-level: parent.child (profile.use, cache.rebuild, cache.clear)
			parentName := rest[:idx]
			parent, ok := parents[parentName]
			if !ok {
				parent = &cli.Command{Name: parentName, Category: "Vault", Usage: vaultParentUsage(parentName), Commands: []*cli.Command{}}
				parents[parentName] = parent
				out = append(out, parent)
			}
			parent.Commands = append(parent.Commands, mounted)
			continue
		}
		out = append(out, mounted)
	}
	return out
}

// mountVaultCatalogCommand adapts a single catalog-compiled command (dotted
// name like "vault.status") into a live vault subcommand: it strips the
// "vault." group prefix, sets the vault category, and wraps the Action with the
// CLI-input adapter (positional → operation input, profile, destructive gate)
// and the vault result renderer.
func mountVaultCatalogCommand(cmd *cli.Command) *cli.Command {
	canonical := cmd.Name
	display := strings.TrimPrefix(canonical, "vault.")
	// Two-level ops (profile.use, cache.rebuild, cache.clear) keep their leaf
	// name; the parent is handled by newVaultCatalogCommands.
	if idx := strings.Index(display, "."); idx > 0 {
		display = display[idx+1:]
	}
	cmd.Name = display
	cmd.Category = "Vault"

	// Positional-supplied required args must not be urfave-parse-time required
	// (see relaxFlagRequired); call before wrapping the Action.
	relaxFlagRequired(cmd)

	var op catalog.Operation
	for _, cand := range catalogops.VaultOperations(vaultCatalogDepsVar) {
		if cand.Name() == canonical {
			op = cand
			break
		}
	}
	if op != nil {
		cmd.Action = vaultActionAdapter(op)
	}
	return cmd
}

// vaultActionAdapter returns the per-invocation ActionFunc for a vault catalog
// operation. It builds the operation input map from flags plus the resolved
// positional path/name and profile, applies the destructive --force gate, and
// renders the handler's result through renderVaultResult.
func vaultActionAdapter(op catalog.Operation) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		// Build the input map from the compiler-declared flags.
		input := map[string]any{}
		for _, a := range op.Args() {
			input[a.Name] = flagValue(c, a)
		}

		// Map the positional arg into the operation's path/name input.
		// The catalog CLI compiler reads flags only, so the adapter resolves
		// the positional <path>/<name> into the declared string arg.
		if c.Args().Len() > 0 && (op.Positional() == "<path>" || op.Positional() == "<name>") {
			argName := "path"
			if !hasArg(op, "path") && hasArg(op, "name") {
				argName = "name"
			}
			if stringVal(input[argName]) == "" {
				input[argName] = c.Args().First()
			}
		}

		// Map the --profile flag (used broadly by vault commands) into the
		// operation's "profile" input when declared.
		if hasArg(op, "profile") && c.IsSet(FlagProfile) {
			input["profile"] = c.String(FlagProfile)
		}

		// Destructive gate (vault rm). Enforce --force.
		//
		// Destructive confirmation gate (vault rm / vault forget). The required
		// --profile argument guards against auto-resolving the wrong profile;
		// --force guards the irreversible registry/cache/seed deletion. Both
		// destructive ops enforce confirmation at the handler level too, so the
		// CLI maps --force into input["confirm"] for programmatic parity.
		if op.Safety() == catalog.SafetyDestructive {
			confirm := c.Bool(FlagForce) || c.Bool(FlagConfirm)
			input["confirm"] = confirm
			if !confirm {
				return fmt.Errorf("vault %s: pass --force to confirm this destructive operation", strings.TrimPrefix(op.Name(), "vault."))
			}
		}

		result, err := op.Handler().Execute(ctx, input)
		if err != nil {
			return err
		}
		return renderVaultResult(ctx, c, op, result)
	}
}

// hasArg reports whether the operation declares an arg with the given name.
func hasArg(op catalog.Operation, name string) bool {
	for _, a := range op.Args() {
		if a.Name == name {
			return true
		}
	}
	return false
}

// renderVaultResult is the catalog.RenderFunc that renders a vault handler's
// typed DATA result through the CLI Output formatter. It is the single
// rendering home for catalog-driven vault commands and never touches core
// services. JSON shapes are kept faithful to the legacy hand-written commands.
func renderVaultResult(_ context.Context, c *cli.Command, op catalog.Operation, result any) error {
	output := setupOutput(c)

	switch r := result.(type) {
	case *vault.StatusResult:
		if output.IsJSON() {
			// Faithful: raw StatusResult at top level (as the legacy command).
			return output.PrintJSON(r)
		}
		return renderVaultStatusHuman(output, c, r)

	case []vault.ListItem:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		if len(r) == 0 {
			output.Printfln("Vault is empty.")
			return nil
		}
		rows := make([][]string, len(r))
		for i, item := range r {
			rows[i] = []string{item.Name, item.Type, fmt.Sprintf("%d", item.Size), item.CreatedAt}
		}
		output.PrintTable([]string{"Name", "Type", "Size", "Created"}, rows)
		return nil

	case *vault.StatResult:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.PrintFields(FieldGroup{
			Title: "Vault File Info",
			Fields: []Field{
				{"Type", r.Type}, {"Name", r.Name}, {"Path", r.Path},
				{"Size", fmt.Sprintf("%d bytes", r.Size)}, {"Media Type", r.MediaType},
				{"Content Digest", r.ContentDigest}, {"Object ID", r.ObjectID},
				{"Created", r.CreatedAt}, {"Updated", r.UpdatedAt},
			},
		})
		return nil

	case *vault.VerifyResult:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		status := "FAIL"
		if r.DigestMatch && r.ObjectExists {
			status = "OK"
		}
		output.PrintFields(FieldGroup{
			Title: "Verification Result",
			Fields: []Field{
				{"Path", r.Path}, {"Status", status}, {"Content Digest", r.ContentDigest},
				{"Digest Match", fmt.Sprintf("%v", r.DigestMatch)},
				{"Object Exists", fmt.Sprintf("%v", r.ObjectExists)},
				{"Object ID", r.ObjectID},
			},
		})
		return nil

	case *catalogops.VaultRmResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"deleted": r.Deleted})
		}
		output.Printfln("Deleted %s", r.Deleted)
		return nil

	case *catalogops.VaultSyncResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"events_processed": r.EventsProcessed})
		}
		output.Printfln("Synced %d events", r.EventsProcessed)
		return nil

	case *catalogops.VaultShareResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"share_url": r.ShareURL, "expires": r.Expires})
		}
		output.Print(r.ShareURL)
		output.Printfln("")
		output.Printfln("Share link expires: %s", r.Expires)
		return nil

	case *catalogops.VaultForgetResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"profile": r.Profile, "state": "forgotten"})
		}
		output.Printfln("Vault profile %q forgotten. Local data removed; remote vault data on Sia was left intact.", r.Profile)
		return nil

	case *catalogops.VaultProfileUseResult:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.Printfln("Default vault profile set to %q.", r.Profile)
		return nil

	case *catalogops.VaultCacheResult:
		if output.IsJSON() {
			if r.State == "rebuilt" {
				return output.PrintJSON(map[string]any{"events_processed": r.EventsProcessed})
			}
			return output.PrintJSON(map[string]any{"state": r.State})
		}
		if r.State == "rebuilt" {
			output.Printfln("Cache rebuilt. %d changes synced.", r.EventsProcessed)
			return nil
		}
		if !r.Existed {
			output.Printfln("No cache to clear.")
			return nil
		}
		output.Printfln("Cache cleared.")
		return nil

	default:
		if result == nil {
			return nil
		}
		return fmt.Errorf("catalog command %q returned an unroutable result type %T", op.Name(), result)
	}
}

// renderVaultStatusHuman renders the human-readable status fields, resolving
// the profile name (a CLI-presentation concern) from flags/env/default.
func renderVaultStatusHuman(output Output, c *cli.Command, res *vault.StatusResult) error {
	profileName, err := vault.ResolveProfile(c.String(FlagProfile))
	if err != nil {
		profileName = c.String(FlagProfile)
	}

	remoteStr := "unreachable"
	if res.RemoteReachable {
		remoteStr = "reachable"
		if !res.RemoteReady {
			remoteStr += " (registration propagating)"
		}
	} else if res.RemoteError != "" {
		remoteStr = "unreachable: " + res.RemoteError
	}
	stateStr := "locked"
	if res.Unlocked {
		stateStr = "unlocked"
	}
	lastSync := "never"
	if res.LastSyncTime != "" {
		lastSync = res.LastSyncTime
	}

	fields := []Field{
		{"Profile", profileName},
		{"State", stateStr},
		{"Remote", remoteStr},
		{"Cache", res.CacheState},
		{"Objects Indexed", fmt.Sprintf("%d", res.ObjectsIndexed)},
		{"Indexed Bytes", fmt.Sprintf("%d", res.TotalBytes)},
		{"Last Sync", lastSync},
	}
	if res.RemoteReachable {
		fields = append(fields,
			Field{"Storage Used", fmt.Sprintf("%d", res.StorageUsed)},
			Field{"Storage Limit", fmt.Sprintf("%d", res.StorageLimit)},
			Field{"Storage Remaining", fmt.Sprintf("%d", res.RemainingStorage)},
		)
	}
	output.PrintFields(FieldGroup{Title: "Vault Status", Fields: fields})
	return nil
}
