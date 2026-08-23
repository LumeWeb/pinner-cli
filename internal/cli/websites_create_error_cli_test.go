package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"

	"go.lumeweb.com/pinner-cli/internal/catalogops"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	"go.lumeweb.com/pinner-cli/internal/core/websites"
)

// cliErrWebsitesService is a websites.Service fake whose CreateWithOptions
// returns the configured error; every other method is a no-op stub satisfied
// by embedding the real interface.
type cliErrWebsitesService struct {
	websites.Service
	err error
}

func (f *cliErrWebsitesService) RequireAuthenticated() error { return nil }
func (f *cliErrWebsitesService) CreateWithOptions(_ context.Context, _ ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error) {
	return nil, f.err
}

// websitesErrDeps returns WebsitesDeps whose ServiceFactory yields the given
// fake, with a hermetic config manager (no real config/network).
func websitesErrDeps(t *testing.T, fake *cliErrWebsitesService) catalogops.WebsitesDeps {
	t.Helper()
	cfgMgr := newTestConfigMgr(t)
	return catalogops.WebsitesDeps{
		CfgMgr: func() config.Manager { return cfgMgr },
		Secure: func() bool { return true },
		ServiceFactory: func(_ config.Manager, _ bool, _ ...websites.Option) websites.Service {
			return fake
		},
		GetAuthToken: func() string { return "" },
	}
}

// TestWebsitesCreateCLISurfacesTranslatedError guards the CLI surface: running
// `pinner websites create` (compiled from the shared catalog op) against a
// service that fails with a CID_NOT_PINNED reason code must surface the
// translated, actionable message. The catalog websitesCreate handler is shared
// with the MCP tool-call path, so this proves the CLI half of the contract.
func TestWebsitesCreateCLISurfacesTranslatedError(t *testing.T) {
	base := errors.New("invalid website data")
	fake := &cliErrWebsitesService{err: &ipfs.APIError{Reason: ipfs.ErrorCodeCIDNotPinned, Err: base}}

	orig := websitesCatalogDepsVar
	websitesCatalogDepsVar = websitesErrDeps(t, fake)
	t.Cleanup(func() { websitesCatalogDepsVar = orig })

	cmds := newWebsitesCatalogCommands()
	var create *cli.Command
	for _, c := range cmds {
		if c.Name == "create" {
			create = c
			break
		}
	}
	require.NotNil(t, create, "websites create command should be compiled")

	root := &cli.Command{
		Name:     "pinner",
		Commands: []*cli.Command{create},
	}
	err := root.Run(context.Background(), []string{
		"pinner", "create",
		"--website", "example.test",
		"--cid", "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not pinned on the gateway")
	require.Contains(t, err.Error(), "pin it first")
	// Original chain preserved for errors.Is.
	require.ErrorIs(t, err, base)
}
