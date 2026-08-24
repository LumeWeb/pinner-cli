package catalogops

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/catalog"
)

// adminWebsitesBlock is the `admin websites block` operation. Returns
// *admin.Website.
func adminWebsitesBlock(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_websites_block",
		Title:       "Block a website",
		Summary:     "Block a website by ID",
		Description: "Block an IPFS website by its ID. Requires admin privileges. Returns the updated website.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<website-id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Website ID to block", PositionalOnly: true},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.websites(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("admin_websites_block: website ID is required")
			}
			return svc.BlockWebsite(ctx, id)
		}),
	})
}

// adminWebsitesUnblock is the `admin websites unblock` operation. Returns
// *admin.Website.
func adminWebsitesUnblock(d AdminDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:        "admin_websites_unblock",
		Title:       "Unblock a website",
		Summary:     "Unblock a website by ID",
		Description: "Unblock a previously blocked IPFS website by its ID. Requires admin privileges. Returns the updated website.",
		Category:    "admin",
		Safety:      catalog.SafetyMutate,
		Interaction: catalog.InteractionAgentSafe,
		Visibility:  catalog.VisibilityBoth,
		Positional:  "<website-id>",
		Args: []catalog.OperationArg{
			{Name: "id", Type: catalog.ArgTypeString, Required: true, Help: "Website ID to unblock", PositionalOnly: true},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			svc, err := d.websites(input)
			if err != nil {
				return nil, err
			}
			if err := svc.RequireAuthenticated(); err != nil {
				return nil, err
			}
			id := catalog.StrArg(input, "id", "")
			if id == "" {
				return nil, fmt.Errorf("admin_websites_unblock: website ID is required")
			}
			return svc.UnblockWebsite(ctx, id)
		}),
	})
}
