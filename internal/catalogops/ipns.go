// Package catalogops implements IPNS domain operations for the operation
// catalog. Each operation drives the core IPNS service directly and returns
// typed data; rendering happens in the CLI wiring layer.
package catalogops

import (
	"context"
	"fmt"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/ipns"
)

// IPNSDeps are the dependencies the IPNS operations need at construction time.
// getters are lazy (resolved per invocation, never at package init).
type IPNSDeps struct {
	CfgMgr func() config.Manager
	Secure func() bool
	// ServiceFactory builds a service; NewAuthenticated builds one pinned to an
	// explicit auth token. GetAuthToken supplies the token override.
	ServiceFactory   ipns.ServiceFactoryFunc
	NewAuthenticated func(cfgMgr config.Manager, token string, secure bool) (ipns.Service, error)
	GetAuthToken     func() string
}

// service builds the IPNS Service honoring the per-invocation auth-token
// override from the input map (flag over config), falling back to
// GetAuthToken(). Returns a clean error when no config manager is available.
func (d IPNSDeps) service(input map[string]any) (ipns.Service, error) {
	cfgMgr := d.config()
	if cfgMgr == nil {
		return nil, fmt.Errorf("catalogops: no config manager available")
	}
	secure := false
	if d.Secure != nil {
		secure = d.Secure()
	}
	if d.NewAuthenticated != nil {
		// Per-invocation --auth-token flag override takes precedence, then the
		// GetAuthToken config fallback.
		if t := authTokenFromInput(input); t != "" {
			return d.NewAuthenticated(cfgMgr, t, secure)
		}
		if g := d.GetAuthToken; g != nil {
			if t := g(); t != "" {
				return d.NewAuthenticated(cfgMgr, t, secure)
			}
		}
	}
	return d.ServiceFactory(cfgMgr, secure), nil
}

func (d IPNSDeps) config() config.Manager {
	if d.CfgMgr != nil {
		return d.CfgMgr()
	}
	return nil
}

// IPNSOperations returns the catalog operations for the IPNS domain. They are
// two-level (ipns.keys.list, ipns.keys.create, ... ipns.publish, ipns.resolve);
// the wiring nests ipns.keys.* under a "keys" parent.
func IPNSOperations(d IPNSDeps) []catalog.Operation {
	return []catalog.Operation{
		ipnsKeysList(d),
		ipnsKeysCreate(d),
		ipnsKeysGet(d),
		ipnsKeysDelete(d),
		ipnsPublish(d),
		ipnsRepublish(d),
		ipnsResolve(d),
	}
}

func ipnsKeysList(d IPNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "ipns_keys_list", Title: "List IPNS keys", Summary: "List all IPNS keys",
		Description: "List all IPNS keys for the authenticated account, optionally narrowing by a server-side name substring search.",
		Category:    "ipns", Safety: catalog.SafetyRead, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "",
		Args: append(catalog.ListArgs(),
			catalog.OperationArg{Name: "search", Type: catalog.ArgTypeString, Help: "Full-text search evaluated server-side against key name"},
		),
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.service(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			var keys []ipfs.IPNSKeyResponse
			if search := catalog.SearchArg(input); search != "" {
				keys, err = svc.ListKeys(ctx, ipfs.ListKeyOption{}.WithFilterName(search))
			} else {
				keys, err = svc.ListKeys(ctx)
			}
			if err != nil {
				return nil, err
			}
			page := catalog.ParseList(input)
			items := slicePage(keys, page.Start, page.Limit)
			headers := []string{"ID", "NAME", "IPNS NAME", "PEER ID", "CREATED"}
			rows := make([][]string, 0, len(items))
			for _, k := range items {
				rows = append(rows, []string{
					fmt.Sprintf("%d", k.Id), k.Name, k.IpnsName, k.PeerId,
					k.Created.Format("2006-01-02 15:04:05"),
				})
			}
			return NewListResult(items, ListResultMeta{
				Noun: "IPNS key(s)", Headers: headers, Rows: rows,
			}), nil
		}),
	})
}

func ipnsKeysCreate(d IPNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "ipns_keys_create", Title: "Create an IPNS key", Summary: "Create a new IPNS key",
		Description: "Create a new IPNS key, optionally importing an existing private key via the key field.",
		Category:    "ipns", Safety: catalog.SafetyMutate, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<name>",
		Args: []catalog.OperationArg{
			{Name: "name", Type: catalog.ArgTypeString, Required: true, Help: "Key name"},
			{Name: "key", Type: catalog.ArgTypeString, Sensitive: true, Help: "Private key to import (optional)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.service(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			name := catalog.StrArg(input, "name", "")
			if name == "" {
				return nil, fmt.Errorf("ipns_keys_create: key name is required")
			}
			var key *string
			if k := catalog.StrArg(input, "key", ""); k != "" {
				key = &k
			}
			return svc.CreateKey(ctx, name, key)
		}),
	})
}

func ipnsKeysGet(d IPNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "ipns_keys_get", Title: "Get an IPNS key", Summary: "Get details of a specific IPNS key",
		Description: "Get the full details (name, ID, sequence) of a single IPNS key.",
		Category:    "ipns", Safety: catalog.SafetyRead, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeFlexibleID, Required: true, Help: "Key ID"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.service(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrFlexibleArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("ipns_keys_get: key ID is required")
			}
			return svc.GetKey(ctx, id)
		}),
	})
}

func ipnsKeysDelete(d IPNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "ipns_keys_delete", Title: "Delete an IPNS key", Summary: "Delete an IPNS key",
		Description: "Delete an IPNS key by ID. DESTRUCTIVE and irreversible: this permanently removes the key and breaks any website publishing under it until republished. Requires confirm=true.",
		Category:    "ipns", Safety: catalog.SafetyDestructive, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeFlexibleID, Required: true, Help: "Key ID"},
			// Confirm is declared so the destructive confirm hand-off on the MCP
			// surface has a field to set on resume. It is AgentRequired (MCP-only)
			// with Default set so the CLI adapter injects a real value: the CLI's
			// --confirm flag defaults to true via this Default (see
			// ipns_wiring.go's ipnsActionAdapter, which populates every arg), so a
			// CLI delete passes the handler gate and the documented
			// delete-without-force contract is preserved. On the MCP surface the
			// model surface is still protected regardless, because the central
			// SafetyDestructive gate refuses destructive ops for a model actor
			// until a human confirms.
			{Name: "confirm", Type: catalog.ArgTypeBool, AgentRequired: true, Default: "true", Help: "Confirm the destructive delete", AgentHelp: "Must be true to delete the key; this is destructive and cannot be undone. Only a human sets this on confirmation; a model alone cannot confirm a destructive delete."},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			// Enforce confirm here, not just on the MCP schema: a human or app
			// actor outside the model ActorModel gate could otherwise pass
			// confirm:false and still delete. Mirrors websites_delete and
			// dns_records_delete. The CLI never sets confirm (see ipns_wiring),
			// so this does not affect the CLI path.
			if !catalog.BoolArg(input, "confirm", false) {
				return nil, fmt.Errorf("ipns_keys_delete: confirmation is required to delete the key")
			}
			svc, err := d.service(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrFlexibleArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("ipns_keys_delete: key ID is required")
			}
			return nil, svc.DeleteKey(ctx, id)
		}),
	})
}

func ipnsPublish(d IPNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "ipns_publish", Title: "Publish a CID to IPNS", Summary: "Publish a CID under an IPNS key",
		Description: "Publish a CID to an IPNS key, optionally with a TTL.",
		Category:    "ipns", Safety: catalog.SafetyMutate, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<cid>",
		Args: []catalog.OperationArg{
			{Name: "cid", Type: catalog.ArgTypeString, Required: true, Help: "Content identifier"},
			{Name: "key-name", Type: catalog.ArgTypeString, Help: "IPNS key to publish under (defaults to default key)"},
			{Name: "ttl", Type: catalog.ArgTypeString, Help: "TTL for the published record"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.service(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			cid := catalog.StrArg(input, "cid", "")
			if cid == "" {
				return nil, fmt.Errorf("ipns_publish: CID is required")
			}
			var ttl *string
			if t := catalog.StrArg(input, "ttl", ""); t != "" {
				ttl = &t
			}
			return svc.Publish(ctx, cid, catalog.StrArg(input, "key-name", ""), ttl)
		}),
	})
}

func ipnsRepublish(d IPNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "ipns_republish", Title: "Republish an IPNS record", Summary: "Republish an IPNS record for a key",
		Description: "Republish an existing IPNS record for a key.",
		Category:    "ipns", Safety: catalog.SafetyMutate, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "",
		Args: []catalog.OperationArg{
			{Name: "key-name", Type: catalog.ArgTypeString, Required: true, Help: "IPNS key to republish"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.service(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			return svc.Republish(ctx, catalog.StrArg(input, "key-name", ""))
		}),
	})
}

func ipnsResolve(d IPNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "ipns_resolve", Title: "Resolve an IPNS name", Summary: "Resolve an IPNS name to a CID",
		Description: "Resolve an IPNS name to the CID it points to.",
		Category:    "ipns", Safety: catalog.SafetyRead, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<name>",
		Args: []catalog.OperationArg{
			{Name: "name", Type: catalog.ArgTypeString, Required: true, Help: "IPNS name to resolve"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.service(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			name := catalog.StrArg(input, "name", "")
			if name == "" {
				return nil, fmt.Errorf("ipns_resolve: name is required")
			}
			return svc.Resolve(ctx, name)
		}),
	})
}
