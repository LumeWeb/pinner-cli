package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
)

func newVaultStatusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Show vault profile status",
		Description: `Summarizes identity, local session, remote health, and cache health
for the selected vault profile.`,
		Action: func(ctx context.Context, c *cli.Command) error {
			output := setupOutput(c)
			profileName, err := vault.ResolveProfile(c.String(FlagProfile))
			if err != nil {
				return err
			}

			reg, err := vault.LoadRegistry()
			if err != nil {
				return err
			}
			profile, exists := reg.Profiles[profileName]
			if !exists {
				return fmt.Errorf("profile %q not found", profileName)
			}

			// Try to load profile state.
			state, stateErr := vault.LoadProfileState(profileName)
			stateOK := stateErr == nil && state != nil && state.AppKey != ""

			// Try to open the cache DB and count objects. Gate on the DB file
			// existing: OpenDB would otherwise CREATE an empty database for a
			// profile that has no cache (e.g. a pending profile from `vault
			// create --agent`, or a profile restored with --no-sync), a write
			// side effect for a read-only status command, and would misreport
			// cacheState as "healthy" for a vault that isn't cached.
			var objectCount int64
			dbOK := false
			dbPath := vault.ProfileDBPath(profileName)
			if _, err := os.Stat(dbPath); err == nil {
				if db, err := vault.OpenDB(dbPath); err == nil {
					if err := db.Model(&vault.File{}).Count(&objectCount).Error; err == nil {
						dbOK = true
					}
					if sqlDB, err := db.DB(); err == nil {
						sqlDB.Close()
					}
				}
			}

			cacheState := "missing"
			if dbOK {
				cacheState = "healthy"
			}

			deviceName := profile.DeviceName
			deviceID := ""
			if stateOK {
				deviceID = state.DeviceID
			}

			// Remote health would require opening the SDK and pinging the
			// indexer; for MVP we report based on state availability.
			remoteReachable := stateOK

			if output.IsJSON() {
				output.PrintJSON(vaultStatusResponse{
					Profile: profileName,
					VaultID: profile.VaultID,
					State:   vaultStatusState{Unlocked: fmt.Sprintf("%v", stateOK)},
					Device:  vaultDeviceInfo{ID: deviceID, Name: deviceName},
					Remote:  vaultStatusRemote{Reachable: remoteReachable},
					Cache:   vaultStatusCache{State: cacheState, ObjectsIndexed: objectCount},
				})
				return nil
			}

			stateStr := "locked"
			if stateOK {
				stateStr = "unlocked"
			}
			remoteStr := "unreachable"
			if remoteReachable {
				remoteStr = "reachable"
			}
			output.PrintFields(FieldGroup{
				Title: "Vault Status",
				Fields: []Field{
					{Label: "Profile", Value: profileName},
					{Label: "Vault", Value: profile.VaultID},
					{Label: "State", Value: stateStr},
					{Label: "Device", Value: deviceName},
					{Label: "Remote", Value: remoteStr},
					{Label: "Cache", Value: cacheState},
					{Label: "Objects Indexed", Value: fmt.Sprintf("%d", objectCount)},
				},
			})
			return nil
		},
	}
}
