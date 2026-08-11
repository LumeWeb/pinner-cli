// Package catalogops: faithful IPNS operations driving the core IPNS service.
//
// The IPNS domain (pns keys list/create/get/delete, publish, republish,
// resolve) is migrated to the operation catalog: each Operation.Handler calls
// internal/core/ipns.Service directly and returns typed DATA (never renders).
// Rendering happens in the pkg/cli wiring layer (ipns_wiring.go).
//
// Import rule (architectural invariant): this package imports internal/catalog
// and internal/core/* but NEVER pkg/cli.
package catalogops

import (
	"context"
	"fmt"

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
// override from the input map (flag → config precedence), falling back to
// GetAuthToken(). Returns a clean error when no config manager is available,
// mirroring PinsDeps.service.
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
		Name: "ipns.keys.list", Title: "List IPNS keys", Summary: "List all IPNS keys",
		Description: "List all IPNS keys for the authenticated account.",
		Category:    "ipns", Safety: catalog.SafetyRead, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "", Args: nil,
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.service(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			return svc.ListKeys(ctx)
		}),
	})
}

func ipnsKeysCreate(d IPNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "ipns.keys.create", Title: "Create an IPNS key", Summary: "Create a new IPNS key",
		Description: "Create a new IPNS key, optionally importing an existing private key via --key.",
		Category:    "ipns", Safety: catalog.SafetyMutate, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<name>",
		Args: []catalog.OperationArg{
			{Name: "name", Type: catalog.ArgTypeString, Required: true, Help: "Key name (positional)"},
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
				return nil, fmt.Errorf("ipns.keys.create: key name is required")
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
		Name: "ipns.keys.get", Title: "Get an IPNS key", Summary: "Get details of a specific IPNS key",
		Description: "Get the full details (name, ID, sequence) of a single IPNS key.",
		Category:    "ipns", Safety: catalog.SafetyRead, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Key ID (positional)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.service(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("ipns.keys.get: key ID is required")
			}
			return svc.GetKey(ctx, id)
		}),
	})
}

func ipnsKeysDelete(d IPNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "ipns.keys.delete", Title: "Delete an IPNS key", Summary: "Delete an IPNS key",
		Description: "Delete an IPNS key by ID. Destructive: requires --force.",
		Category:    "ipns", Safety: catalog.SafetyDestructive, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Key ID (positional)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.service(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("ipns.keys.delete: key ID is required")
			}
			return nil, svc.DeleteKey(ctx, id)
		}),
	})
}

func ipnsPublish(d IPNSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "ipns.publish", Title: "Publish a CID to IPNS", Summary: "Publish a CID under an IPNS key",
		Description: "Publish a CID to an IPNS key, optionally with a TTL.",
		Category:    "ipns", Safety: catalog.SafetyMutate, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<cid>",
		Args: []catalog.OperationArg{
			{Name: "cid", Type: catalog.ArgTypeString, Required: true, Help: "Content identifier (positional)"},
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
				return nil, fmt.Errorf("ipns.publish: CID is required")
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
		Name: "ipns.republish", Title: "Republish an IPNS record", Summary: "Republish an IPNS record for a key",
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
		Name: "ipns.resolve", Title: "Resolve an IPNS name", Summary: "Resolve an IPNS name to a CID",
		Description: "Resolve an IPNS name to the CID it points to.",
		Category:    "ipns", Safety: catalog.SafetyRead, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<name>",
		Args: []catalog.OperationArg{
			{Name: "name", Type: catalog.ArgTypeString, Required: true, Help: "IPNS name to resolve (positional)"},
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
				return nil, fmt.Errorf("ipns.resolve: name is required")
			}
			return svc.Resolve(ctx, name)
		}),
	})
}
