package cli

import (
	"context"
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/core/config"
)

func pinsAdd(ctx context.Context, cmd interface {
	cidGetter
	flagGetterWithIsSet
	StringSlice(name string) []string
}, output Output, cfgMgr config.Manager, authToken string, secure bool, pinningServiceFactory PinningServiceFactory) error {
	cids, err := pin(ctx, cmd, output, cfgMgr, authToken, secure, pinningServiceFactory)
	if err != nil {
		return err
	}

	if cmd.Bool(FlagDryRun) {
		return nil
	}

	metaPairs := cmd.StringSlice(FlagMeta)
	if len(metaPairs) == 0 {
		return nil
	}

	if len(cids) == 0 {
		return nil
	}

	meta, err := parseMetaPairs(metaPairs)
	if err != nil {
		return err
	}

	var pinningService PinningService
	if authToken != "" {
		pinningService = NewPinningService(cfgMgr, output, cfgMgr.Config().GetIPFSEndpointWithSecure(secure), WithAuthToken(authToken))
	} else {
		pinningService = pinningServiceFactory(cfgMgr, secure)
	}

	if err := pinningService.RequireAuthenticated(); err != nil {
		return err
	}

	slice := metaMapToSlice(meta)
	var lastErr error
	for _, cid := range cids {
		if err := pinningService.UpdateMetadata(ctx, cid, slice, false); err != nil {
			lastErr = fmt.Errorf("pin succeeded but metadata update failed for CID %s: %w", cid, err)
		}
	}
	return lastErr
}
