package cli

import (
	"context"
	"os"

	"go.lumeweb.com/pinner-cli/pkg/cli/vault"
	"go.lumeweb.com/pinner-cli/pkg/config"
	"gorm.io/gorm"
)

// closeDB safely closes the underlying sql.DB handle of a gorm.DB.
func closeDB(db *gorm.DB) {
	if db == nil {
		return
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

// vaultStatusAdapter implements mcpadapter.VaultStatusProvider by reading
// from the vault profile registry and the local vault database.
type vaultStatusAdapter struct {
	cfgMgr config.Manager
}

func (a *vaultStatusAdapter) IsInitialized() bool {
	reg, err := vault.LoadRegistry()
	if err != nil || len(reg.Profiles) == 0 {
		return false
	}
	// Report on the ACTIVE profile (env/default/single), not whichever
	// profile the map iteration happens to hit first. A multi-profile setup
	// must not claim "initialized" based on a profile the CLI doesn't operate
	// on.
	name, err := vault.ResolveProfile("")
	if err != nil || name == "" {
		return false
	}
	if _, err := os.Stat(vault.ProfileDBPath(name)); err == nil {
		return true
	}
	return false
}

func (a *vaultStatusAdapter) IsSiaConfigured() bool {
	reg, err := vault.LoadRegistry()
	if err != nil || len(reg.Profiles) == 0 {
		return false
	}
	name, err := vault.ResolveProfile("")
	if err != nil || name == "" {
		return false
	}
	state, err := vault.LoadProfileState(name)
	if err == nil && state.AppKey != "" {
		return true
	}
	return false
}

func (a *vaultStatusAdapter) IndexerURL() string {
	if a.cfgMgr == nil {
		return ""
	}
	return a.cfgMgr.Config().GetSiaIndexerURL()
}

func (a *vaultStatusAdapter) FileCount(ctx context.Context) (int64, error) {
	// Count only the ACTIVE profile's DB, matching IsInitialized/IsSiaConfigured
	// which also resolve the active profile. Summing across every profile while
	// the status resource is gated on the active one produced misleading
	// cross-profile counts in multi-profile setups.
	reg, err := vault.LoadRegistry()
	if err != nil {
		return 0, err
	}
	if len(reg.Profiles) == 0 {
		return 0, nil
	}
	name, err := vault.ResolveProfile("")
	if err != nil || name == "" {
		return 0, nil
	}

	db, err := vault.OpenDB(vault.ProfileDBPath(name))
	if err != nil {
		return 0, err
	}
	defer closeDB(db)

	var count int64
	if err := db.Model(&vault.File{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (a *vaultStatusAdapter) AccountBalance(ctx context.Context) (float64, error) {
	return 0, nil
}
