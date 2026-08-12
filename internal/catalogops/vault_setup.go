package catalogops

import (
	"context"
	"fmt"
	"os"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// This file holds the vault provisioning setup operations (vault.create and
// vault.restore) as catalog operations that drive the core vault.Provisioner
// directly. They return typed handoff data, never CLI strings, so the MCP
// layer can mint a one-time out-of-band URL and a resume handle without
// parsing CLI stdout.
//
// These two operations are deliberately not part of VaultOperations (the
// CLI-facing list): the CLI keeps its hand-written interactive `vault create`
// and `vault restore` commands. They are exposed separately so the MCP layer
// can route pinner_vault_create / pinner_vault_restore through them. They are
// InteractionAgentSafe (not InteractionNeedsHandoff) because they must be
// invocable by a model actor through the catalog Invoke gate; the OOB hand-off
// they describe is returned by the MCP wrapper as a needs_human result, not by
// the catalog gate.

// VaultCreateHandoff is the typed result of vault.create: a pending vault has
// been provisioned by the core Provisioner (fresh 0600 recovery-seed file +
// pending registry entry). Seed holds the plaintext recovery mnemonic for the
// MCP layer to hand to the human over a one-time seed_url. It is host-side
// only and is excluded from JSON serialization (json:"-") so it can never leak
// through a machine channel. Consumers must never place Seed on the MCP
// Text/StructuredContent, CLI stdout, or logs.
type VaultCreateHandoff struct {
	// Profile is the provisioned pending profile name.
	Profile string `json:"profile"`
	// SeedPath is the durable 0600 recovery-seed file path.
	SeedPath string `json:"seed_path,omitempty"`
	// Seed is the plaintext recovery mnemonic, host-side presentation only.
	// REDACTED: never log, never place on the MCP Text/StructuredContent or CLI
	// stdout, never serialize. Excluded from JSON via json:"-". Transported only
	// through the OOB one-time loopback seed_url.
	Seed string `json:"-"`
}

// VaultRestoreHandoff is the typed result of vault.restore: the profile
// targeted by an out-of-band browser restore has been resolved. The MCP layer
// mints a one-time restore_url against the OOB restore coordinator for this
// profile. The actual completion (Provisioner.Restore) runs when the human
// submits the seed in the browser, never on the agent channel.
type VaultRestoreHandoff struct {
	Profile string `json:"profile"`
}

// VaultSetupOperations returns the vault provisioning setup operations that
// the MCP layer consumes to drive pinner_vault_create / pinner_vault_restore
// as clean, CLI-free OOB hand-offs. They are separate from VaultOperations so
// the CLI's hand-written interactive create/restore commands are unaffected.
func VaultSetupOperations(d VaultDeps) []catalog.Operation {
	return []catalog.Operation{
		vaultCreate(d),
		vaultRestore(d),
	}
}

// ---------------------------------------------------------------------------
// vault create
// ---------------------------------------------------------------------------

func vaultCreate(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:             "vault.create",
		Title:            "Create a new vault",
		Summary:          "Provision a new pending vault profile with a fresh recovery seed",
		Description:      "Provision a new vault identity under the given profile name: generate a fresh recovery seed, persist it to a 0600 host file, and register a pending profile awaiting restore. The vault is completed by an out-of-band browser restore. Returns the profile and the host seed path.",
		AgentDescription: "Provision a new vault under a profile and hand the host off to a human. Generates a fresh recovery seed, persists it to a 0600 host file, and registers a pending profile. An out-of-band seed_url is returned for the human to retrieve the recovery seed in a browser; poll the returned pinner_vault_create_resume handle until the seed has been retrieved. The plaintext mnemonic never appears on this channel.",
		Category:         "vault",
		Safety:           catalog.SafetyMutate,
		Interaction:      catalog.InteractionAgentSafe,
		Visibility:       catalog.VisibilityModel,
		Positional:       "",
		Args: []catalog.OperationArg{
			{Name: "profile", Type: catalog.ArgTypeString, Required: true, Help: "Vault profile name to provision (a fresh vault cannot auto-resolve a default)"},
			{Name: "device-name", Type: catalog.ArgTypeString, Help: "Name for this device (defaults to hostname)"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			profileName := catalog.StrArg(input, "profile", "")
			if profileName == "" {
				return nil, fmt.Errorf("vault.create: --profile <name> is required to provision a new vault")
			}
			if d.Provisioner == nil {
				return nil, fmt.Errorf("vault.create: no provisioning service wired")
			}
			prov := d.Provisioner()
			if prov == nil {
				return nil, fmt.Errorf("vault.create: no provisioning service wired")
			}
			pend, err := prov.CreatePending(vault.CreateRequest{
				Profile:    profileName,
				DeviceName: catalog.StrArg(input, "device-name", ""),
			})
			if err != nil {
				return nil, err
			}
			return &VaultCreateHandoff{
				Profile:  pend.Profile,
				SeedPath: pend.SeedPath,
				Seed:     pend.Seed,
			}, nil
		}),
	})
}

// ---------------------------------------------------------------------------
// vault restore
// ---------------------------------------------------------------------------

// resolveRestoreProfile resolves the profile an OOB restore targets, matching
// the CLI restore action's fresh-device allow: an explicit --profile, else
// PINNER_PROFILE, else the registry default, else "default". This keeps the
// OOB coordinator minting the restore_url for the same profile the browser
// form restores into.
func resolveRestoreProfile(flagValue string) (string, error) {
	profileName := flagValue
	if profileName == "" {
		profileName = os.Getenv("PINNER_PROFILE")
	}
	if profileName == "" {
		reg, err := vault.LoadRegistry()
		if err == nil && reg.Default != "" {
			profileName = reg.Default
		}
	}
	if profileName == "" {
		profileName = "default"
	}
	if err := vault.ValidateProfileName(profileName); err != nil {
		return "", err
	}
	// Mirror the CLI restore action's active-profile guard: restoring over an
	// already-active vault would silently overwrite its VaultID and device
	// credentials and remove the recovery seed. A pending profile (empty
	// VaultID) is the one case restore is meant to complete, so it is allowed.
	reg, err := vault.LoadRegistry()
	if err != nil {
		return "", err
	}
	if p, exists := reg.Profiles[profileName]; exists && p.VaultID != "" {
		return "", fmt.Errorf("profile %q already exists as an active vault; restore cannot overwrite it", profileName)
	}
	return profileName, nil
}

func vaultRestore(d VaultDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:             "vault.restore",
		Title:            "Restore a vault",
		Summary:          "Start an out-of-band restore for a vault profile",
		Description:      "Start restoring an existing vault on this device from a recovery seed supplied out-of-band by a human in a browser. Resolves the target profile and returns it so an out-of-band restore_url can be minted; the restore itself completes when the human enters the seed on that page. The seed never crosses the agent channel.",
		AgentDescription: "Start an out-of-band vault restore for a profile. An out-of-band restore_url is returned for the human to open in a browser and enter the recovery seed to complete the restore; poll the returned pinner_vault_restore_resume handle until done. The seed never appears on this channel.",
		Category:         "vault",
		Safety:           catalog.SafetyMutate,
		Interaction:      catalog.InteractionAgentSafe,
		Visibility:       catalog.VisibilityModel,
		Positional:       "",
		Args: []catalog.OperationArg{
			{Name: "profile", Type: catalog.ArgTypeString, Help: "Vault profile to restore (defaults to the default/only profile, else 'default')"},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			profileName, err := resolveRestoreProfile(catalog.StrArg(input, "profile", ""))
			if err != nil {
				return nil, err
			}
			return &VaultRestoreHandoff{Profile: profileName}, nil
		}),
	})
}
