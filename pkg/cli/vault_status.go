package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func newVaultStatusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Show vault profile status",
		Description: `Summarizes identity, local session, remote health, storage usage,
and cache health for the selected vault profile. Remote health is probed
live against the indexer; local cache stats come from the profile's index.`,
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)

			svc, profileName, err := vaultServiceForCommand(c)
			if err != nil {
				return err
			}
			defer svc.Close()

			res, err := svc.Status(ctx)
			if err != nil {
				return err
			}

			if output.IsJSON() {
				output.PrintJSON(res)
				return nil
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
				{Label: "Profile", Value: profileName},
				{Label: "State", Value: stateStr},
				{Label: "Remote", Value: remoteStr},
				{Label: "Cache", Value: res.CacheState},
				{Label: "Objects Indexed", Value: fmt.Sprintf("%d", res.ObjectsIndexed)},
				{Label: "Indexed Bytes", Value: fmt.Sprintf("%d", res.TotalBytes)},
				{Label: "Last Sync", Value: lastSync},
			}
			if res.RemoteReachable {
				fields = append(fields,
					Field{Label: "Storage Used", Value: fmt.Sprintf("%d", res.StorageUsed)},
					Field{Label: "Storage Limit", Value: fmt.Sprintf("%d", res.StorageLimit)},
					Field{Label: "Storage Remaining", Value: fmt.Sprintf("%d", res.RemainingStorage)},
				)
			}
			output.PrintFields(FieldGroup{Title: "Vault Status", Fields: fields})
			return nil
		},
	}
}
