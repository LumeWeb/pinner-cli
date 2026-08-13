// Package catalogops builds catalog.Operations whose handlers drive the core
// service domains directly and return typed data. Rendering happens in the
// CLI/MCP frontend.
package catalogops

import (
	"context"
	"fmt"
	"strconv"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/pinning"
)

// PinsDeps are the dependencies the pins operations need at construction time.
type PinsDeps struct {
	// CfgMgr returns a live config manager for the current invocation. It is a
	// getter (not a value) so service construction always uses fresh config,
	// never a package-init snapshot. When nil, service() passes nil to the
	// factories (the operations then fail on auth if unauthenticated).
	CfgMgr func() config.Manager
	// Secure reports whether to use the secure (HTTPS) endpoint. Like CfgMgr it
	// is a getter resolved per invocation; nil means secure=false.
	Secure func() bool
	// ServiceFactory builds an authenticated PinningService. When
	// NewAuthenticated is non-nil and authToken is non-empty it is used;
	// otherwise ServiceFactory is used.
	ServiceFactory pinning.PinningServiceFactory
	// NewAuthenticated builds a service with an explicit auth token; nil means
	// tokens are read from config via ServiceFactory.
	NewAuthenticated func(cfgMgr config.Manager, secure bool, token string) pinning.PinningService
	// GetAuthToken returns an auth token override for the current command
	// context (empty = none).
	GetAuthToken func() string
}

// config returns the live config manager for this invocation, or nil when no
// getter is wired.
func (d PinsDeps) config() config.Manager {
	if d.CfgMgr != nil {
		return d.CfgMgr()
	}
	return nil
}

// service builds the PinningService from the deps. A per-invocation
// auth-token override from input takes precedence over the
// deps.GetAuthToken() config fallback; otherwise ServiceFactory is used.
func (d PinsDeps) service(input map[string]any) (pinning.PinningService, error) {
	cfgMgr := d.config()
	if cfgMgr == nil {
		return nil, fmt.Errorf("catalogops: no config manager available")
	}
	secure := false
	if d.Secure != nil {
		secure = d.Secure()
	}
	if d.NewAuthenticated != nil {
		if t := authTokenFromInput(input); t != "" {
			return d.NewAuthenticated(cfgMgr, secure, t), nil
		}
		if tok := d.GetAuthToken; tok != nil {
			if t := tok(); t != "" {
				return d.NewAuthenticated(cfgMgr, secure, t), nil
			}
		}
	}
	return d.ServiceFactory(cfgMgr, secure), nil
}

// Service returns the PinningService for a given invocation, applying the
// per-invocation --auth-token override from the input map (flag precedes the
// config-read GetAuthToken fallback). It is the exported form of the unexported
// service, exposed for CLI wiring that must construct a service outside a
// handler context (e.g. to enumerate pins for an interactive safety prompt).
func (d PinsDeps) Service(input map[string]any) (pinning.PinningService, error) {
	return d.service(input)
}

// handler adapts a core-service calling function to the catalog Handler
// interface, decoding map[string]any inputs into typed call arguments.
type handler func(ctx context.Context, input map[string]any) (any, error)

func (h handler) Execute(ctx context.Context, input map[string]any) (any, error) {
	return h(ctx, input)
}

// DryRunResult is the data returned by a handler when the caller requests a
// dry-run (--dry-run): the operation never mutates state, and the handler
// instead returns what it WOULD have done. It is pure data. The CLI wiring
// layer renders it as a dry-run preview; the MCP layer renders it as a result.
type DryRunResult struct {
	Operation string
	CIDs      []string
	Options   map[string]string
}

// dryRun builds a DryRunResult describing what a dry-run of the given
// operation would do. The handler returns this without calling the core
// service (no mutation, report the plan).
func dryRun(operation string, cids []string, options map[string]string) *DryRunResult {
	if options == nil {
		options = map[string]string{}
	}
	return &DryRunResult{Operation: operation, CIDs: cids, Options: options}
}

// PinsOperations returns the catalog operations for the pins domain (the
// existing `pins` subcommand group), each driving the core PinningService.
func PinsOperations(d PinsDeps) []catalog.Operation {
	return []catalog.Operation{
		pinsList(d),
		pinsAdd(d),
		pinsRemove(d),
		pinsStatus(d),
		pinsUpdate(d),
	}
}

// pinsList is the `pins ls` operation. Watch polling and stdin name reads
// are CLI presentation concerns handled in the wiring layer; this handler
// only takes the resolved name filter.
func pinsList(d PinsDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "pins_list",
		Title:       "List pins",
		Summary:     "List pinned content",
		Description: "List your pinned content with optional filtering by name, status, and a result limit.",
		Category:    "core",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "",
		Args: []catalog.OperationArg{
			{Name: "name", Type: catalog.ArgTypeString, Help: "Filter pins by name"},
			{Name: "limit", Type: catalog.ArgTypeInt, Default: "0", Help: "Maximum number of pins to return (0 = no limit)"},
			{Name: "status", Type: catalog.ArgTypeString, Help: "Filter pins by status (e.g. pinned, unpinned, failed)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			pins, err := svc.List(ctx, catalog.StrArg(input, "name", ""), catalog.IntArg(input, "limit", 0), catalog.StrArg(input, "status", ""))
			if err != nil {
				return nil, err
			}
			return pins, nil
		}),
	})
}

// pinsAdd is the `pins add` operation (pin existing CIDs). A single CID goes
// through PinningService.Pin; multiple CIDs go through PinBatch. wait defaults
// to true. Metadata pairs are applied after a successful pin. CID input is
// resolved in the CLI wiring layer and injected as the "cids" arg; this
// handler is IO-free. --dry-run returns a DryRunResult instead of mutating
// state.
func pinsAdd(d PinsDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "pins_add",
		Title:       "Add a pin",
		Summary:     "Pin existing content by CID",
		Description: "Pin content that already exists on IPFS by providing its CID(s), optionally with a name, metadata (--meta k=v), parallel batch control (--parallel/--continue), and the choice of waiting for confirmation. CIDs may be given positionally, via --file, or piped from stdin.",
		Category:    "core",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<cid...>",
		Args: []catalog.OperationArg{
			// cids is populated by the CLI wiring layer from positional args,
			// --file, or stdin.
			{Name: "cids", Type: catalog.ArgTypeStringSlice, Help: "Content identifiers to pin (positional, --file, or stdin)"},
			{Name: "name", Type: catalog.ArgTypeString, Help: "Custom name for the pin"},
			{Name: "wait", Type: catalog.ArgTypeBool, Default: "true", Help: "Wait for the pin to be confirmed (default true; use --no-wait to skip)"},
			{Name: "parallel", Type: catalog.ArgTypeInt, Default: "0", Help: "Maximum number of parallel pin operations for a batch"},
			{Name: "continue", Type: catalog.ArgTypeBool, Default: "false", Help: "Continue pinning remaining CIDs when one fails"},
			{Name: "meta", Type: catalog.ArgTypeStringSlice, Help: "Metadata key=value pairs to set on the pin (repeatable)"},
			{Name: "dry-run", Type: catalog.ArgTypeBool, Default: "false", Help: "Show what would be pinned without mutating state"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}

			cids := catalog.StrSliceArg(input, "cids")
			if len(cids) == 0 {
				return nil, fmt.Errorf("pins_add: no CIDs provided (pass <cid...>, --file, or pipe from stdin)")
			}

			name := catalog.StrArg(input, "name", "")
			wait := catalog.BoolArg(input, "wait", true)
			parallel := catalog.IntArg(input, "parallel", 0)
			continueOn := catalog.BoolArg(input, "continue", false)
			metaPairs := catalog.StrSliceArg(input, "meta")

			// Dry-run: report the plan, never mutate.
			if catalog.BoolArg(input, "dry-run", false) {
				options := map[string]string{}
				options["Wait"] = strconv.FormatBool(wait)
				if name != "" {
					options["Name"] = name
				}
				if parallel > 1 && len(cids) > 1 {
					options["Parallel"] = strconv.Itoa(parallel)
				}
				if continueOn {
					options["Continue on error"] = "yes"
				}
				if len(metaPairs) > 0 {
					options["Metadata pairs"] = strconv.Itoa(len(metaPairs) / 2)
				}
				return dryRun("pinning operations", cids, options), nil
			}

			var pinned []string
			var result any
			if len(cids) == 1 {
				r, err := svc.Pin(ctx, cids[0], name, wait)
				if err != nil {
					return nil, err
				}
				result = r
				pinned = []string{cids[0]}
			} else {
				r, err := svc.PinBatch(ctx, cids, name, pinning.BatchOptions{
					Parallel:   parallel,
					ContinueOn: continueOn,
					Wait:       wait,
					Progress:   true,
				})
				if err != nil {
					return nil, err
				}
				result = r
				pinned = cids
			}

			// Apply --meta after the pin succeeds: metadata is set on each
			// CID only after its pin is created.
			if len(metaPairs) > 0 {
				// Parse each --meta k=v pair into an alternating [k,v,k,v]
				// slice for UpdateMetadata, which rejects odd-length slices.
				parsed, perr := splitMetaPairs(metaPairs)
				if perr != nil {
					return nil, perr
				}
				var lastErr error
				for _, cid := range pinned {
					if err := svc.UpdateMetadata(ctx, cid, parsed, false); err != nil {
						lastErr = fmt.Errorf("pin succeeded but metadata update failed for CID %s: %w", cid, err)
					}
				}
				if lastErr != nil {
					return nil, lastErr
				}
			}

			return result, nil
		}),
	})
}

// pinsRemove is the `pins rm` operation (unpin CIDs, or unpin all).
// Confirmation is mandatory: without it a single-CID Unpin silently no-ops,
// and batch/unpin-all fail. The CLI wiring enforces --force (and the hidden
// --confirm alias) via the destructive gate; this handler threads the confirm
// bool through so programmatic callers can pass it explicitly. --all unpins
// every pin (optionally filtered by --status) via UnpinAll; the interactive
// count-typing prompt is enforced in the wiring layer, not here. CID input is
// resolved in the wiring layer and injected as the "cids" arg. --dry-run
// returns a DryRunResult instead of mutating state.
func pinsRemove(d PinsDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "pins_rm",
		Title:       "Remove a pin",
		Summary:     "Unpin existing CIDs (or all pins)",
		Description: "Remove a pin by CID, remove multiple CIDs in a batch, or remove all pins with --all. Destructive: requires confirmation (--force).",
		Category:    "core",
		Safety:      catalog.SafetyDestructive,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<cid...>",
		Args: []catalog.OperationArg{
			{Name: "cids", Type: catalog.ArgTypeStringSlice, Help: "Content identifiers to unpin (positional, --file, or stdin)"},
			{Name: "confirm", Type: catalog.ArgTypeBool, Default: "false", Help: "Confirm the destructive operation (legacy alias for --force)"},
			{Name: "all", Type: catalog.ArgTypeBool, Default: "false", Help: "Remove all pins (requires --force)"},
			{Name: "status", Type: catalog.ArgTypeString, Help: "When --all, only unpin pins with this status (e.g. failed)"},
			{Name: "parallel", Type: catalog.ArgTypeInt, Default: "0", Help: "Maximum number of parallel unpin operations for a batch"},
			{Name: "continue", Type: catalog.ArgTypeBool, Default: "false", Help: "Continue unpinning remaining CIDs when one fails"},
			{Name: "dry-run", Type: catalog.ArgTypeBool, Default: "false", Help: "Show what would be unpinned without mutating state"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}

			confirm := catalog.BoolArg(input, "force", false) || catalog.BoolArg(input, "confirm", false)
			parallel := catalog.IntArg(input, "parallel", 0)
			continueOn := catalog.BoolArg(input, "continue", false)

			// --all unpins every pin (optionally filtered by status).
			if catalog.BoolArg(input, "all", false) {
				statusFilter := catalog.StrArg(input, "status", "")
				if catalog.BoolArg(input, "dry-run", false) {
					// Report the request IDs that would be unpinned without
					// mutating state. Naming them requires a read-only List.
					pins, err := svc.List(ctx, "", 0, statusFilter)
					if err != nil {
						return nil, err
					}
					requestIDs := make([]string, len(pins))
					for i, pin := range pins {
						requestIDs[i] = pin.RequestID
					}
					options := map[string]string{"Confirm": "no (--force required)"}
					if statusFilter != "" {
						options["Status filter"] = statusFilter
					}
					if parallel > 1 {
						options["Parallel"] = strconv.Itoa(parallel)
					}
					if continueOn {
						options["Continue on error"] = "yes"
					}
					return dryRun(fmt.Sprintf("unpin-all (%d pins)", len(pins)), requestIDs, options), nil
				}
				return svc.UnpinAll(ctx, statusFilter, pinning.BatchOptions{
					Parallel:   parallel,
					ContinueOn: continueOn,
					Progress:   true,
				})
			}

			cids := catalog.StrSliceArg(input, "cids")
			if len(cids) == 0 {
				return nil, fmt.Errorf("pins_rm: no CIDs provided (pass <cid...>, --file, pipe from stdin, or --all)")
			}

			if catalog.BoolArg(input, "dry-run", false) {
				options := map[string]string{"Confirm": "yes"}
				if confirm {
					options["Confirm"] = "no (using --force)"
				}
				if parallel > 1 && len(cids) > 1 {
					options["Parallel"] = strconv.Itoa(parallel)
				}
				if continueOn {
					options["Continue on error"] = "yes"
				}
				return dryRun("unpin operations", cids, options), nil
			}

			if len(cids) == 1 {
				return svc.Unpin(ctx, cids[0], confirm)
			}

			return svc.UnpinBatch(ctx, cids, pinning.BatchOptions{
				Parallel:   parallel,
				ContinueOn: continueOn,
			})
		}),
	})
}

// pinsStatus is the `pins status` operation. Status polls until the pin
// settles internally when --watch is set, so watch is threaded through.
func pinsStatus(d PinsDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "pins_status",
		Title:       "Pin status",
		Summary:     "Get the status of a pin",
		Description: "Get the current status of a pinned CID, optionally watching until it settles (--watch).",
		Category:    "core",
		Safety:      catalog.SafetyRead,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<cid>",
		Args: []catalog.OperationArg{
			{Name: "cid", Type: catalog.ArgTypeString, Required: true, Help: "Content identifier to check (positional or --cid)"},
			{Name: "watch", Type: catalog.ArgTypeBool, Default: "false", Help: "Poll until the pin settles"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			cid := catalog.StrArg(input, "cid", "")
			if cid == "" {
				return nil, fmt.Errorf("pins_status: missing required argument cid")
			}
			return svc.Status(ctx, cid, catalog.BoolArg(input, "watch", false))
		}),
	})
}

// pinsUpdate is the `pins update` operation (rename / metadata). At least one
// of --name/--meta/--clear-meta is required (enforced in the wiring layer);
// the handler delegates the update to PinningService.UpdatePin. --dry-run
// returns a DryRunResult instead of mutating state. CID input is a single
// positional (or --cid), resolved in the wiring layer.
func pinsUpdate(d PinsDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "pins_update",
		Title:       "Update a pin",
		Summary:     "Update a pin's name or metadata",
		Description: "Update the name and/or metadata of an existing pin by CID. Metadata is a set of key-value pairs (--meta k=v, repeatable); --clear-meta wipes existing metadata first.",
		Category:    "core",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<cid>",
		Args: []catalog.OperationArg{
			{Name: "cid", Type: catalog.ArgTypeString, Required: true, Help: "Content identifier to update (positional or --cid)"},
			{Name: "name", Type: catalog.ArgTypeString, Help: "New name for the pin"},
			{Name: "meta", Type: catalog.ArgTypeStringSlice, Help: "Metadata key=value pairs to set (repeatable)"},
			{Name: "clear-meta", Type: catalog.ArgTypeBool, Default: "false", Help: "Clear existing metadata before applying the new pairs"},
			{Name: "dry-run", Type: catalog.ArgTypeBool, Default: "false", Help: "Show what would be updated without mutating state"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, svcErr := d.service(input)
			if svcErr != nil {
				return nil, svcErr
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			cid := catalog.StrArg(input, "cid", "")
			if cid == "" {
				return nil, fmt.Errorf("pins_update: missing required argument cid")
			}

			name := catalog.StrArg(input, "name", "")
			metaPairs := catalog.StrSliceArg(input, "meta")
			clearMeta := catalog.BoolArg(input, "clear-meta", false)

			if catalog.BoolArg(input, "dry-run", false) {
				options := map[string]string{"CID": cid}
				if name != "" {
					options["Name"] = name
				}
				if clearMeta {
					options["Clear metadata"] = "true"
				}
				if len(metaPairs) > 0 {
					options["Metadata pairs"] = strconv.Itoa(len(metaPairs) / 2)
				}
				return dryRun("pin update", []string{cid}, options), nil
			}

			// UpdatePin requires an alternating key,value metadata slice.
			// Parse each --meta k=v pair first.
			parsedMeta, perr := splitMetaPairs(metaPairs)
			if perr != nil {
				return nil, perr
			}
			if err := svc.UpdatePin(ctx, cid, name, parsedMeta, clearMeta); err != nil {
				return nil, err
			}
			// Return the updated pin identity as data so the CLI can print a
			// confirmation line and a machine caller gets a structured result.
			return &pinning.PinResult{CID: cid, Status: "updated"}, nil
		}),
	})
}
