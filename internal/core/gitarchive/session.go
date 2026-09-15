package gitarchive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.sia.tech/core/types"
	"go.sia.tech/siastorage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"go.lumeweb.com/pinner/core/vault"
)

// ErrNoProfile is surfaced whenever git archive cannot resolve a usable vault
// profile. It is the canonical missing-profile error seen by the CLI and the
// helper (exact message: "no vault profile; run pinner vault create or pinner
// vault restore").
var ErrNoProfile = errors.New("no vault profile; run pinner vault create or pinner vault restore")

// Session carries the resolved, profile-scoped context for git archive
// operations: the profile's app key (which authenticates the Sia account), the
// Sia indexer URL, the open SQLite cache DB (with the git archive tables
// migrated), and the mockable Sia storage SDK.
type Session struct {
	// Profile is the resolved vault profile name.
	Profile string
	// AppKey is the profile's app key (hex-decoded from state.json).
	AppKey types.PrivateKey
	// IndexerURL is the Sia indexer origin for this profile.
	IndexerURL string
	// DB is the profile SQLite cache with the gitarchive tables present.
	DB *gorm.DB

	// SDK is the Sia storage client. nil until first use; callers may inject a
	// fake for tests (see ensureSDK).
	SDK SDKClient
}

// NewSession resolves the active profile (flag → PINNER_PROFILE → default →
// single-profile, via the vault registry), loads and decodes its app key,
// opens its SQLite cache DB (running the git archive AutoMigrate), and returns
// a Session with the SDK still unbuilt. The SDK is built lazily on first use
// because constructing it hits the network (CheckAppAuth + refreshHosts).
func NewSession(profileValue, indexerURL string) (*Session, error) {
	name, err := vault.ResolveProfile(profileValue)
	if err != nil {
		return nil, ErrNoProfile
	}
	if err := vault.ValidateProfileName(name); err != nil {
		return nil, ErrNoProfile
	}
	state, err := vault.LoadProfileState(name)
	if err != nil {
		return nil, fmt.Errorf("gitarchive: load profile state: %w", err)
	}
	if state.AppKey == "" {
		return nil, ErrNoProfile
	}
	appKey, err := vault.DecodeAppKey(state.AppKey)
	if err != nil {
		return nil, fmt.Errorf("gitarchive: decode app key: %w", err)
	}

	db, err := openProfileDB(name)
	if err != nil {
		return nil, err
	}
	return &Session{
		Profile:    name,
		AppKey:     appKey,
		IndexerURL: indexerURL,
		DB:         db,
	}, nil
}

// openProfileDB opens the profile's SQLite cache and runs the git archive
// schema migration. Unlike the vault library it uses Gorm sqlite directly and
// only the additive gitarchive AutoMigrate; it never touches the vault File
// schema.
func openProfileDB(profile string) (*gorm.DB, error) {
	path := vault.ProfileDBPath(profile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("gitarchive: create profile dir: %w", err)
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("gitarchive: open profile cache: %w", err)
	}
	if err := AutoMigrate(db); err != nil {
		return nil, fmt.Errorf("gitarchive: migrate profile cache: %w", err)
	}
	return db, nil
}

// ensureSDK returns the Sia SDK, building the real one lazily on first use
// (mirroring the vault service). When a fake SDK has been injected into the
// Session (e.g. by tests) it is returned unchanged and no network is touched.
func (s *Session) ensureSDK() (SDKClient, error) {
	if s.SDK != nil {
		return s.SDK, nil
	}
	if s.IndexerURL == "" {
		return nil, fmt.Errorf("gitarchive: no Sia indexer URL for profile %q", s.Profile)
	}
	metadata := siastorage.AppMetadata{
		ID:          vault.AppID(),
		Name:        "Pinner CLI Git Archive",
		Description: "Git archive hosting via Sia",
		ServiceURL:  s.IndexerURL,
	}
	builder := siastorage.NewBuilder(s.IndexerURL, metadata)
	real, err := builder.SDK(s.AppKey)
	if err != nil {
		return nil, fmt.Errorf("gitarchive: build SDK: %w", err)
	}
	s.SDK = &realSDK{SDK: real}
	return s.SDK, nil
}

// Close releases SDK-held resources (host connections, thread group).
func (s *Session) Close() error {
	if s.SDK != nil {
		return s.SDK.Close()
	}
	return nil
}
