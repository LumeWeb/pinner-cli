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

// catalog_vault_wiring.go adapts the vault domain operations in
// internal/catalogops to the urfave CLI: it compiles catalog operations into
// commands under the "vault" parent, renders each handler's result through the
// Output formatter, and maps positional args, profile selection, and the
// destructive --force gate onto operation inputs. IO and CLI concerns
// (positional path/name mapping, profile resolution, force gate, result
// rendering) live here, not in catalogops.
//
// Name mapping: canonical catalog names use dots ("vault.status"). The "vault."
// group prefix is stripped and leaves are mounted under a "vault" parent;
// two-level ops ("vault.profile.use", "vault.cache.rebuild", "vault.cache.clear")
// nest under their "profile"/"cache" parent commands.
//
// The commands that are fundamentally interactive/IO (create, restore, cp, cat)
// are not compiled from the catalog (see catalogops.VaultOperations for why)
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
	// A flat leaf whose name is later used as a two-level parent (e.g. the
	// one-level `vault_share` issuing command and the two-level
	// `vault_share_accept`) is folded INTO that parent: the parent keeps its
	// top-level Action (so `vault share <path>` still issues) while the accept
	// leaf nests under it (`vault share accept ...`). urfave/cli runs a
	// command's Action when no matching subcommand name is supplied, so the two
	// coexist as one `share` entry.
	parents := map[string]*cli.Command{}
	var out []*cli.Command
	flatLeaf := map[string]*cli.Command{} // mounted flat leaf by display name
	for _, c := range compiled {
		canonical := c.Name // e.g. "vault_cache_rebuild", BEFORE mount mutates it
		mounted := mountVaultCatalogCommand(c)
		rest := strings.TrimPrefix(canonical, "vault_")
		if idx := strings.Index(rest, "_"); idx > 0 {
			// Two-level: parent_child (profile_use, cache_rebuild, cache_clear)
			parentName := rest[:idx]
			parent, ok := parents[parentName]
			if !ok {
				parent = &cli.Command{Name: parentName, Category: "Vault", Usage: vaultParentUsage(parentName), Commands: []*cli.Command{}}
				if flat, exists := flatLeaf[parentName]; exists {
					// Fold the flat leaf into the parent as its top-level Action.
					parent.Action = flat.Action
					parent.Flags = append(parent.Flags, flat.Flags...)
					parent.Usage = flat.Usage
					parent.ArgsUsage = flat.ArgsUsage
					out = removeCommand(out, flat)
				}
				parents[parentName] = parent
				out = append(out, parent)
			}
			parent.Commands = append(parent.Commands, mounted)
			continue
		}
		flatLeaf[rest] = mounted
		out = append(out, mounted)
	}
	return out
}

// removeCommand returns out with cmd removed (matched by pointer identity).
func removeCommand(out []*cli.Command, cmd *cli.Command) []*cli.Command {
	for i, c := range out {
		if c == cmd {
			return append(out[:i], out[i+1:]...)
		}
	}
	return out
}

// mountVaultCatalogCommand adapts a single catalog-compiled command (dotted
// name like "vault_status") into a live vault subcommand: it strips the
// "vault_" group prefix, sets the vault category, and wraps the Action with the
// CLI-input adapter (positional → operation input, profile, destructive gate)
// and the vault result renderer.
func mountVaultCatalogCommand(cmd *cli.Command) *cli.Command {
	canonical := cmd.Name
	display := strings.TrimPrefix(canonical, "vault_")
	// Two-level ops (profile_use, cache_rebuild, cache_clear) keep their leaf
	// name; the parent is handled by newVaultCatalogCommands.
	if idx := strings.Index(display, "_"); idx > 0 {
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
	// `vault flush` MUST block. The catalog vault_flush op is non-blocking: it
	// launches the durability work on a detached background goroutine and
	// returns "accepted" immediately. That is the right shape for the long-lived
	// MCP server, but for a one-shot CLI command the background goroutine dies
	// when the process exits — leaving every pending file forever pending.
	// Override the mounted action so the CLI waits on the flush synchronously
	// and reports the real flushed count.
	if canonical == "vault_flush" {
		cmd.Action = vaultFlushSyncAction()
		// The sync action reads the positional path itself; keep the catalog
		// flags (--profile) as mounted.
	}
	return cmd
}

// vaultFlushSyncAction returns a blocking Action for `pinner vault flush`: it
// runs svc.Flush/FlushPath synchronously (the same primitive `cp --flush` uses)
// and reports the number of files made durable before the process exits. It is
// the CLI counterpart to the catalog's non-blocking MCP vault_flush.
func vaultFlushSyncAction() cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		output := setupOutput(c)

		profileName, err := vault.ResolveProfile(c.String(FlagProfile))
		if err != nil {
			return err
		}
		svc, err := vaultCatalogDepsVar.ServiceForProfile(profileName)
		if err != nil {
			return err
		}
		defer svc.Close()

		// Flush is a heavyweight host-set upload; allow it up to the configured
		// upload timeout (not the short per-call default) so a real backlog is
		// not cut off mid-pin.
		dctx := ctx
		if cfgMgr, cerr := configManagerFactory(); cerr == nil && cfgMgr != nil {
			if to := cfgMgr.Config().GetUploadTimeout(); to > 0 {
				var cancel context.CancelFunc
				dctx, cancel = context.WithTimeout(ctx, to)
				defer cancel()
			}
		}

		// Positional <path> restricts the flush to a single file; otherwise flush
		// every staged (pending) file.
		path := c.Args().First()
		if path != "" {
			// FlushPath is a no-op for an already-durable/lost file.
			if err := svc.FlushPath(dctx, path); err != nil {
				return err
			}
			if output.IsJSON() {
				return output.PrintJSON(map[string]any{"status": "ok", "flushed": 1, "path": path})
			}
			output.Printfln("Flushed %s", path)
			return nil
		}
		n, err := svc.Flush(dctx)
		if err != nil {
			return err
		}
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"status": "ok", "flushed": n})
		}
		output.Printfln("Flushed %d pending file(s)", n)
		return nil
	}
}

// vaultActionAdapter returns the per-invocation ActionFunc for a vault catalog
// operation. It builds the operation input map from flags plus the resolved
// positional path/name and profile, applies the destructive --force gate, and
// renders the handler's result through renderVaultResult.
func vaultActionAdapter(op catalog.Operation) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		// Build the input map from the compiler-declared flags.
		input := catalog.FlagsToInput(c, op)

		// Map the positional argument into the operation's path/name input.
		// The catalog CLI compiler reads flags only, so the adapter resolves
		// the positional <path>/<name> into the declared string arg. The
		// mapping rule (right-aligned, surplus rejection, name resolution from
		// the Positional declaration) lives in the catalog framework and is
		// shared with every frontend.
		if err := catalog.MapPositionalArgs(op.Args(), op.Positional(), c.Args().Slice(), input); err != nil {
			return err
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
				return fmt.Errorf("vault %s: pass --force to confirm this destructive operation", strings.TrimPrefix(op.Name(), "vault_"))
			}
		}

		// Apply the legacy per-call deadline (shared with every catalog domain).
		dctx, cancel := applyDefaultTimeout(ctx)
		defer cancel()

		result, err := op.Handler().Execute(dctx, input)
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
// typed result through the CLI Output formatter. It is the single rendering
// home for catalog-driven vault commands.
func renderVaultResult(_ context.Context, c *cli.Command, op catalog.Operation, result any) error {
	output := setupOutput(c)

	switch r := result.(type) {
	case *vault.StatusResult:
		if output.IsJSON() {
			// Raw StatusResult at top level.
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
		switch {
		case r.Pending:
			status = "PENDING"
		case r.DigestVerified == "verified" && r.ObjectExists:
			status = "OK"
		case r.DigestVerified == "unverified" && r.ObjectExists:
			status = "UNVERIFIED"
		}
		output.PrintFields(FieldGroup{
			Title: "Verification Result",
			Fields: []Field{
				{"Path", r.Path}, {"Status", status}, {"Content Digest", r.ContentDigest},
				{"Digest Verified", r.DigestVerified},
				{"Digest Match", vaultDigestMatchDisplay(r.DigestMatch)},
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
			return output.PrintJSON(map[string]any{"share_url": r.ShareURL, "expires": r.Expires, "status": r.Status, "message": r.Message})
		}
		if r.Status != "ok" {
			// pending/uploaded/lost all carry a message and no share URL; never
			// fall through to printing an empty link.
			output.Printfln("File is not shareable (%s): %s", r.Status, r.Message)
			return nil
		}
		output.Print(r.ShareURL)
		output.Printfln("")
		output.Printfln("Share link expires: %s", r.Expires)
		return nil

	case *catalogops.VaultFlushResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"status": r.Status, "message": r.Message, "flushed": r.Flushed})
		}
		if r.Flushed > 0 {
			output.Printfln("%s: %s (%d file(s))", r.Status, r.Message, r.Flushed)
			return nil
		}
		output.Printfln("%s: %s", r.Status, r.Message)
		return nil

	case *catalogops.VaultShareAcceptResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"path": r.Path, "object_key": r.ObjectKey, "size": r.Size})
		}
		output.Printfln("Accepted copy pinned at %s (%d bytes, object %s)", r.Path, r.Size, r.ObjectKey)
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

	case catalogops.ListResult:
		return renderListResult(output, r)

	case *catalogops.VaultVersionGetResult:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		cur := "no"
		if r.IsCurrent {
			cur = "yes"
		}
		output.PrintFields(FieldGroup{
			Title: "Vault File Version",
			Fields: []Field{
				{"Path", r.Path}, {"Version ID", r.VersionID}, {"Seq", fmt.Sprintf("%d", r.Seq)},
				{"Current", cur}, {"Size", fmt.Sprintf("%d bytes", r.Size)},
				{"Media Type", r.MediaType}, {"Content Digest", r.ContentDigest},
				{"Object ID", r.ObjectKey}, {"Created", r.CreatedAt}, {"Updated", r.UpdatedAt},
			},
		})
		return nil

	case *catalogops.VaultVersionRestoreResult:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.Printfln("Restored %s to version %s (%d bytes) as the new current version.", r.Path, r.RestoredTo, r.Size)
		return nil

	case *catalogops.VaultTagResult:
		if output.IsJSON() {
			return output.PrintJSON(r)
		}
		output.Printfln("Tagged %s: %v", r.Path, r.Tags)
		return nil

	case *catalogops.VaultSearchResult:
		if output.IsJSON() {
			return output.PrintJSON(map[string]any{"count": r.Count, "results": r.Results})
		}
		if r.Count == 0 {
			output.Printfln("No matching files.")
			return nil
		}
		rows := make([][]string, 0, r.Count)
		for _, p := range r.Results {
			item := r.Detail[p]
			rows = append(rows, []string{p, formatBytes(int(item.Size)), strings.Join(item.Tags, ","), item.Source, item.Host, item.Agent})
		}
		output.PrintTable([]string{"Path", "Size", "Tags", "Source", "Host", "Agent"}, rows)
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

// vaultDigestMatchDisplay renders the tri-state DigestMatch for human output:
// "n/a" when there is no verdict (nil), otherwise the boolean as a string.
func vaultDigestMatchDisplay(dm *bool) string {
	if dm == nil {
		return "n/a"
	}
	return fmt.Sprintf("%v", *dm)
}
