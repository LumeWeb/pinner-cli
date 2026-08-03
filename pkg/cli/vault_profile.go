package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
)

func newVaultProfileCommand() *cli.Command {
	return &cli.Command{
		Name:     "profile",
		Usage:    "Manage vault profiles",
		Category: "Vault",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "List all vault profiles",
				Action: func(ctx context.Context, c *cli.Command) error {
					output := setupOutput(c)
					reg, err := vault.LoadRegistry()
					if err != nil {
						return err
					}
					if len(reg.Profiles) == 0 {
						output.Printfln("No vault profiles found. Run 'pinner vault create' to create one.")
						return nil
					}

					if c.Bool(FlagJSON) || c.Bool(FlagAgent) {
						profiles := make([]vaultProfileEntry, 0, len(reg.Profiles))
						for name, p := range reg.Profiles {
							profiles = append(profiles, vaultProfileEntry{
								Name:      name,
								VaultID:   p.VaultID,
								Device:    p.DeviceName,
								IsDefault: name == reg.Default,
								Cache:     p.CachePath,
							})
						}
						output.PrintJSON(vaultProfileListResponse{Profiles: profiles})
					} else {
						for name, p := range reg.Profiles {
							marker := "  "
							if name == reg.Default {
								marker = "* "
							}
							output.Printfln("%s%s  vault:%s  device:%s", marker, name, p.VaultID, p.DeviceName)
						}
					}
					return nil
				},
			},
			{
				Name:      "use",
				Usage:     "Set the default vault profile",
				ArgsUsage: "<profile-name>",
				Action: func(ctx context.Context, c *cli.Command) error {
					output := setupOutput(c)
					if c.Args().Len() < 1 {
						return fmt.Errorf("usage: pinner vault profile use <name>")
					}
					name := c.Args().Get(0)

					reg, err := vault.LoadRegistry()
					if err != nil {
						return err
					}
					if _, exists := reg.Profiles[name]; !exists {
						return fmt.Errorf("profile %q not found", name)
					}
					reg.Default = name
					if err := vault.SaveRegistry(reg); err != nil {
						return err
					}
					output.Printfln("Default profile set to %q.", name)
					return nil
				},
			},
			{
				Name:      "rename",
				Usage:     "Rename a vault profile",
				ArgsUsage: "<old-name> <new-name>",
				Action: func(ctx context.Context, c *cli.Command) error {
					output := setupOutput(c)
					if c.Args().Len() < 2 {
						return fmt.Errorf("usage: pinner vault profile rename <old> <new>")
					}
					oldName := c.Args().Get(0)
					newName := c.Args().Get(1)

					if err := vault.ValidateProfileName(oldName); err != nil {
						return fmt.Errorf("invalid old profile name: %w", err)
					}
					if err := vault.ValidateProfileName(newName); err != nil {
						return fmt.Errorf("invalid new profile name: %w", err)
					}

					reg, err := vault.LoadRegistry()
					if err != nil {
						return err
					}
					profile, exists := reg.Profiles[oldName]
					if !exists {
						return fmt.Errorf("profile %q not found", oldName)
					}
					if _, exists := reg.Profiles[newName]; exists {
						return fmt.Errorf("profile %q already exists", newName)
					}

					// Rename the profile directory on disk
					oldDir := vault.ProfileDir(oldName)
					newDir := vault.ProfileDir(newName)
					if err := os.MkdirAll(filepath.Dir(newDir), 0700); err != nil {
						return fmt.Errorf("failed to create new profile directory: %w", err)
					}
					// Guard against a stale, unregistered directory at the
					// target path: os.Rename would silently merge or overwrite
					// it, pointing the profile at colliding content.
					if _, err := os.Stat(newDir); err == nil {
						return fmt.Errorf("target profile directory already exists: %s", newDir)
					}
					if err := os.Rename(oldDir, newDir); err != nil {
						return fmt.Errorf("failed to rename profile directory: %w", err)
					}

					// Update registry. If the registry write fails, roll the
					// directory back so we never leave the profile pointing
					// at a directory that has already moved.
					profile.CachePath = vault.ProfileDBPath(newName)
					profile.AppKeyRef = vault.ProfileStatePath(newName)
					delete(reg.Profiles, oldName)
					reg.Profiles[newName] = profile
					if reg.Default == oldName {
						reg.Default = newName
					}
					if err := vault.SaveRegistry(reg); err != nil {
						if rbErr := os.Rename(newDir, oldDir); rbErr != nil {
							return fmt.Errorf("failed to save registry: %w (and rollback of directory rename failed: %v)", err, rbErr)
						}
						return fmt.Errorf("failed to save registry (directory rename rolled back): %w", err)
					}
					output.Printfln("Profile renamed: %s -> %s", oldName, newName)
					return nil
				},
			},
		},
	}
}
