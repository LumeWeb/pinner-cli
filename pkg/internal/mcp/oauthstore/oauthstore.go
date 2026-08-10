// Package oauthstore provides a tiny GORM-backed SQLite store for the MCP
// OAuth authorization server's durable state: registered clients and issued
// refresh tokens. The store exists so a long-running OAuth flow's refresh
// tokens survive and, crucially, tolerate reuse/rotation without being
// invalidated the instant they are presented (which was breaking Anthropic's
// Claude connector with "invalid_grant").
//
// Auth codes stay in-memory in the server (10-minute TTL, single-use); only
// clients and refresh tokens need durability here.
package oauthstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Client is a dynamically registered OAuth client. RedirectURIs is stored as
// a JSON-encoded []string.
type Client struct {
	ID           string    `gorm:"primaryKey;column:id"`
	Name         string    `gorm:"column:name"`
	RedirectURIs string    `gorm:"column:redirect_uris"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

// TableName matches the existing in-memory naming for readability.
func (Client) TableName() string { return "oauth_clients" }

// RefreshToken is an issued refresh token. Per OAuth BCP (RFC 9700) refresh
// tokens are rotated on every use: the presenting token is marked used and a
// successor is issued. A token that is re-presented is a reuse signal — within
// a short detection window this is treated as a benign race (the client had
// not persisted the successor yet) and is tolerated; beyond the window the
// whole token chain is revoked and the use rejected, matching refresh-token
// replay protection. Token, store the chain root so a chain can be revoked.
type RefreshToken struct {
	Token     string     `gorm:"primaryKey;column:token"`
	ClientID  string     `gorm:"column:client_id"`
	Resource  string     `gorm:"column:resource"`
	ChainRoot string     `gorm:"column:chain_root;index"`
	IssuedAt  time.Time  `gorm:"column:issued_at"`
	UsedAt    *time.Time `gorm:"column:used_at"` // set when rotated; nil = current
	ExpiresAt time.Time  `gorm:"column:expires_at"`
	Revoked   bool       `gorm:"column:revoked"`
}

// TableName returns the refresh-token table name.
func (RefreshToken) TableName() string { return "oauth_refresh_tokens" }

// Store wraps the SQLite database and exposes the operations the OAuth server
// needs. It is goroutine-safe through GORM's connection pool.
type Store struct {
	db         *gorm.DB
	refreshTTL time.Duration
	// reuseWindow is the grace period during which a freshly-rotated refresh
	// token that is re-presented is treated as a benign race (the client had
	// not yet persisted the successor) and is accepted, rather than treating it
	// as a replay that revokes the grant. This prevents breaking well-behaved
	// connectors that legitimately re-present a token during reconnect.
	reuseWindow time.Duration
}

// Open opens (or creates) the SQLite store at path, applying schema migration
// via AutoMigrate. The DB file is restricted to 0600 like other sensitive
// pinner state. refreshTTL sets the lifetime of newly issued refresh tokens.
func Open(path string, refreshTTL time.Duration) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("oauthstore: db path must not be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("oauthstore: create dir: %w", err)
	}
	db, err := gorm.Open(sqlite.Open(path+"?_foreign_keys=on&_busy_timeout=5000"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("oauthstore: open db: %w", err)
	}
	if err := db.AutoMigrate(&Client{}, &RefreshToken{}); err != nil {
		return nil, fmt.Errorf("oauthstore: migrate: %w", err)
	}
	if err := restrictFile(path); err != nil {
		return nil, fmt.Errorf("oauthstore: restrict permissions: %w", err)
	}
	if refreshTTL <= 0 {
		refreshTTL = 30 * 24 * time.Hour
	}
	return &Store{db: db, refreshTTL: refreshTTL, reuseWindow: 30 * time.Second}, nil
}

// Close closes the underlying database connection pool.
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// ---- clients ----

// SaveClient persists a registered client.
func (s *Store) SaveClient(id, name string, redirectURIs []string) error {
	enc, err := json.Marshal(redirectURIs)
	if err != nil {
		return err
	}
	c := Client{ID: id, Name: name, RedirectURIs: string(enc), CreatedAt: time.Now()}
	return s.db.Save(&c).Error
}

// ClientRedirectURIs returns the redirect URIs for a client id.
func (s *Store) ClientRedirectURIs(id string) ([]string, error) {
	var c Client
	if err := s.db.Where("id = ?", id).First(&c).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	var uris []string
	if err := json.Unmarshal([]byte(c.RedirectURIs), &uris); err != nil {
		return nil, err
	}
	return uris, nil
}

// ---- refresh tokens ----

// IssueRefreshToken stores the initial refresh token of a new chain (the root)
// issued from an authorization-code exchange.
func (s *Store) IssueRefreshToken(token, clientID, resource string) error {
	return s.issueInChain(token, clientID, resource, token)
}

// issueInChain stores a refresh token whose chain root is chainRoot. Successor
// tokens from rotation inherit the chain root of the token they rotate from so
// a whole grant chain can be revoked together.
func (s *Store) issueInChain(token, clientID, resource, chainRoot string) error {
	return s.db.Create(&RefreshToken{
		Token:     token,
		ClientID:  clientID,
		Resource:  resource,
		ChainRoot: chainRoot,
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(s.refreshTTL),
	}).Error
}

// RotateStatus describes the outcome of a refresh-token presentation.
type RotateStatus int

const (
	// RotateOK: valid, unexpired, never-used token — rotate and accept.
	RotateOK RotateStatus = iota
	// RotateOKReused: valid token already rotated, but re-presented within the
	// reuse-detection window (benign race: client hadn't persisted the
	// successor). Accept a fresh pair without revoking.
	RotateOKReused
	// RotateReplay: a previously-rotated token re-presented after the detection
	// window, or a revoked/expired token. Revoke the chain and reject.
	RotateReplay
	// RotateUnknown: no refresh token with this value.
	RotateUnknown
)

// RotateRefreshToken validates a refresh-token presentation per OAuth BCP
// (RFC 9700) refresh-token rotation + reuse detection. On a valid use it marks
// the token used, issues a successor in the same chain, and returns
// RotateOK. If the token was already used but is re-presented within the reuse
// window the same client, it returns RotateOKReused (accept + re-rotate).
// Re-presentation beyond the window, a revoked token, or bad binding revokes
// the whole chain and returns RotateReplay, which the caller surfaces as
// invalid_grant.
func (s *Store) RotateRefreshToken(token, clientID, resource, successor string) (string, RotateStatus, error) {
	var rt RefreshToken
	err := s.db.Where("token = ?", token).First(&rt).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", RotateUnknown, nil
		}
		return "", RotateUnknown, err
	}
	if rt.Revoked || time.Now().After(rt.ExpiresAt) {
		return "", RotateReplay, nil
	}
	// Binding: the presenting client must match, and the resource must match
	// when one was bound.
	if clientID != "" && rt.ClientID != clientID {
		return "", RotateReplay, nil
	}
	if resource != "" && rt.Resource != "" && resource != rt.Resource {
		return "", RotateReplay, nil
	}
	now := time.Now()
	if rt.UsedAt != nil {
		// Already rotated. Within the reuse window → benign race, tolerate.
		if now.Sub(*rt.UsedAt) <= s.reuseWindow {
			if err := s.issueInChain(successor, rt.ClientID, rt.Resource, rt.ChainRoot); err != nil {
				return "", RotateUnknown, err
			}
			return rt.ClientID, RotateOKReused, nil
		}
		// Replay: revoke the whole chain and reject.
		if err := s.revokeChain(rt.ChainRoot); err != nil {
			return "", RotateUnknown, err
		}
		return "", RotateReplay, nil
	}
	// First use: mark used, issue successor in the same chain.
	if err := s.db.Model(&rt).Updates(map[string]any{"used_at": now}).Error; err != nil {
		return "", RotateUnknown, err
	}
	if err := s.issueInChain(successor, rt.ClientID, rt.Resource, rt.ChainRoot); err != nil {
		return "", RotateUnknown, err
	}
	return rt.ClientID, RotateOK, nil
}

// revokeChain marks every token in a chain as revoked.
func (s *Store) revokeChain(root string) error {
	return s.db.Model(&RefreshToken{}).Where("chain_root = ?", root).Update("revoked", true).Error
}

// Reap deletes expired refresh tokens and stale clients. Called periodically
// by the server's reaper.
func (s *Store) Reap() error {
	now := time.Now()
	err := s.db.Where("expires_at < ?", now).Delete(&RefreshToken{}).Error
	if err != nil {
		return err
	}
	return s.db.Where("issued_at < ?", now.Add(-s.refreshTTL)).Delete(&Client{}).Error
}

func restrictFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0o600 {
		return os.Chmod(path, 0o600)
	}
	return nil
}
