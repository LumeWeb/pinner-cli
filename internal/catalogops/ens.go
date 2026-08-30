// Package catalogops implements ENS (Ethereum Name Service) / onchain domain
// operations for the operation catalog. ENS names do not use the website
// system (websites_create / DNS _dnslink); they resolve via an IPNS-based
// contenthash set onchain in the ENS resolver. Each operation drives the core
// IPNS service directly and returns typed data (including wallet guidance);
// rendering happens in the frontend wiring layer.
package catalogops

import (
	"context"
	"fmt"
	"strings"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/ipns"
)

// Contenthash/verify identity constants shared by the ENS operations and their
// frontend wiring. An ENS contenthash is "ipns://<ipns name>"; the verify URL
// prefers the eth.limo gateway for .eth names and the IPNS inbrowser gateway
// otherwise.
const (
	ENSSchemePrefix = "ipns://"
	ensSuffix       = ".eth"
	ensLimoFmt      = "https://%s.eth.limo"
	ensGatewayFmt   = "https://%s.ipns.inbrowser.link"
)

// ENSDeps are the dependencies the ENS operations need. ENS reuses the IPNS
// service (an ENS pointing is an IPNS key + publish), so the deps embed the
// IPNS deps and forward service() to them. All getters are lazy (resolved per
// invocation, never at package init).
type ENSDeps struct {
	IPNS IPNSDeps
}

// service builds the IPNS service the ENS operations drive, honoring the
// per-invocation auth-token override exactly as the IPNS domain does.
func (d ENSDeps) service(input map[string]any) (ipns.Service, error) {
	return d.IPNS.service(input)
}

// ENSPointResult is the data returned by ens_point: the published IPNS
// identity, the contenthash the user must set in the ENS resolver, a verify
// URL, and ordered wallet guidance. The next_steps are machine- and
// human-readable "what the user does next, onchain" instructions.
type ENSPointResult struct {
	Name        string   `json:"name"`
	CID         string   `json:"cid"`
	IPNSName    string   `json:"ipns_name"`
	Contenthash string   `json:"contenthash"`
	VerifyURL   string   `json:"verify_url,omitempty"`
	Created     bool     `json:"created"`
	NextSteps   []string `json:"next_steps"`
}

// ENSUnpointResult is the data returned by ens_unpoint: the removed IPNS
// identity plus guidance to clear the onchain contenthash if the name should
// stop resolving.
type ENSUnpointResult struct {
	Name      string   `json:"name"`
	IPNSName  string   `json:"ipns_name"`
	Deleted   bool     `json:"deleted"`
	NextSteps []string `json:"next_steps"`
}

// ENSOperations returns the catalog operations for the ENS domain. They are
// single-level (ens_point, ens_unpoint) and stay behind progressive disclosure
// on the MCP surface — they are never promoted to the curated tools/list, so
// adding ENS support does not bloat the default tool call list.
func ENSOperations(d ENSDeps) []catalog.Operation {
	return []catalog.Operation{
		ensPoint(d),
		ensUnpoint(d),
	}
}

func ensPoint(d ENSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "ens_point", Title: "Point an onchain/ENS domain at IPFS content", Summary: "Publish a CID to IPNS and return the ENS contenthash to set onchain",
		Description: "Point an onchain/decentralized domain (e.g. vitalik.eth) at IPFS content via IPNS. Idempotent: if an IPNS key for this domain already exists it is reused and the new CID republished. Returns the contenthash string (ipns://...) the user must set in the ENS resolver, plus a verify URL and the wallet-next-step guidance. The onchain contenthash set is NOT done by Pinner — the user signs it from their own wallet/ENS manager.",
		Category:    "ens", Safety: catalog.SafetyMutate, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<name>",
		Args: []catalog.OperationArg{
			{Name: "name", Type: catalog.ArgTypeString, Required: true, Help: "Onchain/ENS domain to point (e.g. vitalik.eth)", AgentHelp: "The onchain/ENS domain to point, e.g. vitalik.eth. Do not invent one; use the name the user provided."},
			{Name: "cid", Type: catalog.ArgTypeString, Required: true, Help: "CID to point the domain at"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.service(input)
			if err != nil {
				return nil, err
			}
			name := catalog.StrArg(input, "name", "")
			cid := catalog.StrArg(input, "cid", "")
			return PointENS(ctx, svc, name, cid)
		}),
	})
}

func ensUnpoint(d ENSDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name: "ens_unpoint", Title: "Remove an onchain/ENS domain pointing", Summary: "Delete the IPNS key for an onchain/ENS domain",
		Description: "Remove the IPNS key for an onchain/decentralized domain so its IPNS name is no longer managed by Pinner. DESTRUCTIVE and irreversible: the key is permanently removed and any content pointed at by the returned contenthash stops being served. This does NOT clear the onchain contenthash record — guide the user to remove/clear it in their ENS resolver if the name should stop resolving. Requires confirm=true.",
		Category:    "ens", Safety: catalog.SafetyDestructive, Interaction: catalog.InteractionAgentSafe, Visibility: catalog.VisibilityBoth,
		Positional: "<name>",
		Args: []catalog.OperationArg{
			{Name: "name", Type: catalog.ArgTypeString, Required: true, Help: "Onchain/ENS domain to unpoint (e.g. vitalik.eth)"},
			{Name: "confirm", Type: catalog.ArgTypeBool, AgentRequired: true, Default: "true", Help: "Confirm the destructive delete", AgentHelp: "Must be true to delete the key; this is destructive and cannot be undone. Only a human sets this on confirmation; a model alone cannot confirm a destructive delete."},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			// Enforce confirm here, not just on the MCP schema: a human or app
			// actor outside the model ActorModel gate could otherwise pass
			// confirm:false and still delete. Mirrors ipns_keys_delete.
			if !catalog.BoolArg(input, "confirm", false) {
				return nil, fmt.Errorf("ens_unpoint: confirmation is required to delete the key")
			}
			svc, err := d.service(input)
			if err != nil {
				return nil, err
			}
			name := catalog.StrArg(input, "name", "")
			return UnpointENS(ctx, svc, name)
		}),
	})
}

// PointENS implements the shared point logic used by both the ens_point
// catalog operation (MCP surface) and the CLI point command: create-or-reuse
// the IPNS key keyed by the domain name, publish the CID to it, and return the
// contenthash + verification + wallet guidance. It is presentation-free (core
// data only) so both frontends render the same underlying result. The service
// must not be nil.
func PointENS(ctx context.Context, svc ipns.Service, name, cid string) (*ENSPointResult, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required (e.g., vitalik.eth)")
	}
	if cid == "" {
		return nil, fmt.Errorf("cid is required")
	}
	if err := svc.RequireAuthenticated(); err != nil {
		return nil, err
	}

	key, isNew, err := resolveOrCreateIPNSKey(ctx, svc, name)
	if err != nil {
		return nil, err
	}

	if _, err := svc.Publish(ctx, cid, key.Name, nil); err != nil {
		return nil, fmt.Errorf("failed to publish to IPNS: %w", err)
	}

	contenthash := ENSSchemePrefix + key.IpnsName
	res := &ENSPointResult{
		Name:        name,
		CID:         cid,
		IPNSName:    key.IpnsName,
		Contenthash: contenthash,
		Created:     isNew,
		VerifyURL:   ResolveVerifyURL(name, key.IpnsName),
	}
	res.NextSteps = pointNextSteps(res)
	return res, nil
}

// UnpointENS implements the shared unpoint logic used by both the ens_unpoint
// catalog operation and the CLI unpoint command: find the IPNS key keyed by
// the domain name and delete it, returning the removed identity plus guidance
// to clear the onchain contenthash. The service must not be nil.
func UnpointENS(ctx context.Context, svc ipns.Service, name string) (*ENSUnpointResult, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required (e.g., vitalik.eth)")
	}
	if err := svc.RequireAuthenticated(); err != nil {
		return nil, err
	}

	keys, err := svc.ListKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list IPNS keys: %w", err)
	}

	var found *ipfs.IPNSKeyResponse
	for _, k := range keys {
		if k.Name == name {
			found = &k
			break
		}
	}
	if found == nil {
		return nil, fmt.Errorf("no IPNS key found for %q", name)
	}

	if err := svc.DeleteKey(ctx, fmt.Sprintf("%d", found.Id)); err != nil {
		return nil, fmt.Errorf("failed to delete IPNS key: %w", err)
	}

	res := &ENSUnpointResult{Name: name, IPNSName: found.IpnsName, Deleted: true}
	res.NextSteps = []string{
		fmt.Sprintf("The IPNS key for %s was removed; it will no longer serve the contenthash %s.", name, ENSSchemePrefix+found.IpnsName),
		"If you want the ENS name to stop resolving entirely, also clear or update the contenthash record in your ENS resolver (an onchain transaction from your wallet/ENS manager).",
	}
	return res, nil
}

// ResolveVerifyURL returns the gateway URL a user can open to confirm an
// onchain/ENS name resolves after pointing. .eth names use the eth.limo
// gateway (no wallet required); other names use the generic IPNS inbrowser
// gateway.
func ResolveVerifyURL(name, ipnsName string) string {
	if strings.HasSuffix(strings.ToLower(name), ensSuffix) {
		return fmt.Sprintf(ensLimoFmt, name[:len(name)-len(ensSuffix)])
	}
	return fmt.Sprintf(ensGatewayFmt, ipnsName)
}

// pointNextSteps composes the ordered wallet guidance returned in an
// ENSPointResult. It deliberately does not assume a specific wallet or setup:
// it names the common paths as options and surfaces the exact contenthash
// value plus a verify URL, leaving the agent to present them naturally.
func pointNextSteps(res *ENSPointResult) []string {
	steps := []string{
		fmt.Sprintf("Set the ENS resolver's contenthash record to: %s", res.Contenthash),
		"This is an onchain transaction on the Ethereum network — Pinner cannot set it for you.",
		"Set it from a wallet or manager that can sign an ENS set-contenthash transaction, for example the ENS manager (app.ens.domains), the ENS SDK via ethers.js, or a wallet with ENS support.",
	}
	if res.VerifyURL != "" {
		steps = append(steps, fmt.Sprintf("After the onchain transaction confirms, verify at: %s", res.VerifyURL))
	}
	return steps
}

// resolveOrCreateIPNSKey returns the IPNS key for an ENS/onchain domain name,
// creating it if it does not already exist. It mirrors the idempotent point
// contract: creation succeeds (returning isNew=true), or an existing key with
// that name is reused (isNew=false).
func resolveOrCreateIPNSKey(ctx context.Context, svc ipns.Service, name string) (*ipfs.IPNSKeyResponse, bool, error) {
	created, createErr := svc.CreateKey(ctx, name, nil)
	if createErr == nil {
		return created, true, nil
	}

	keys, listErr := svc.ListKeys(ctx)
	if listErr != nil {
		return nil, false, fmt.Errorf("failed to list IPNS keys: %w", listErr)
	}

	for _, k := range keys {
		if k.Name == name {
			return &k, false, nil
		}
	}

	return nil, false, fmt.Errorf("IPNS key %q not found and creation failed: %w", name, createErr)
}
