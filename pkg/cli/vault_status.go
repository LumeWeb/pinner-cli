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

			// Try to load profile state
			state, stateErr := vault.LoadProfileState(profileName)
			stateOK := stateErr == nil && state != nil && state.AppKey != ""

			// Try to open DB and count objects. Gate on the DB file existing:
			// OpenDB would otherwise CREATE an empty database for a profile
			// that has no cache (e.g. a pending profile from `vault create
			// --agent`, or a profile restored with --no-sync), a write side
			// effect for a read-only status command, and would misreport
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

			// Determine cache health
			cacheState := "missing"
			if dbOK {
				cacheState = "healthy"
			}

			// Determine device name
			deviceName := profile.DeviceName
			deviceID := ""
			if stateOK {
				deviceID = state.DeviceID
			}

			// For remote health, we'd need to open the SDK and ping.
			// For MVP, just report based on state availability.
			remoteReachable := stateOK

			if c.Bool(FlagJSON) || c.Bool(FlagAgent) {
				output.PrintJSON(vaultStatusResponse{
					Profile: profileName,
					VaultID: profile.VaultID,
					State:   vaultStatusState{Unlocked: fmt.Sprintf("%v", stateOK)},
					Device:  vaultDeviceInfo{ID: deviceID, Name: deviceName},
					Remote:  vaultStatusRemote{Reachable: remoteReachable},
					Cache:   vaultStatusCache{State: cacheState, ObjectsIndexed: objectCount},
				})
			} else {
				output.Printfln("Profile:         %s", profileName)
				output.Printfln("Vault:           %s", profile.VaultID)
				if stateOK {
					output.Printfln("State:           unlocked")
				} else {
					output.Printfln("State:           locked")
				}
				output.Printfln("Device:          %s", deviceName)
				if remoteReachable {
					output.Printfln("Remote:          reachable")
				} else {
					output.Printfln("Remote:          unreachable")
				}
				output.Printfln("Cache:           %s", cacheState)
				output.Printfln("Objects indexed: %d", objectCount)
			}
			return nil
		},
	}
}
