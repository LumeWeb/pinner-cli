package gitremote

import (
	"context"
	"errors"
	"os"
	"strings"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

// ErrSiaNotWired is returned by the production Store's data operations while the
// Sia-backed archive backend is not compiled in (the default build). The
// remote-helper protocol engine, the go-git pack encode/ingest path, and
// reference handling are always complete; publishing a pack + tip to Sia and
// downloading tip packs back is the dedicated Sia integration enabled by the
// `gitarchive_sia` build tag (see session_store_sia.go). Without that tag the
// data operations report this sentinel; with it, they exercise the real SDK.
var ErrSiaNotWired = errors.New("git remote helper: Sia archive backend not wired yet (build without gitarchive_sia)")

// SessionStore is the production Store backed by a resolved Pinner profile
// session (see internal/core/gitarchive.Session). It owns the session so Close
// releases the SDK/DB.
type SessionStore struct {
	session *gitarchive.Session
	locker  string // account-scoped locker this store reads/writes (pinner::<locker>)
}

var _ Store = (*SessionStore)(nil)

// NewSessionStoreFromEnv resolves the active profile (flag/PINNER_PROFILE/
// default, via the vault registry) and returns a session-backed Store. It also
// surfaces gitarchive.ErrNoProfile intact so the helper prints the exact
// "no vault profile; run pinner vault create or pinner vault restore" message.
func NewSessionStoreFromEnv(ctx context.Context) (*SessionStore, error) {
	return NewSessionStore(os.Getenv("PINNER_PROFILE"), "")
}

// NewSessionStore builds a session-backed Store for the given profile and Sia
// indexer URL. An empty indexerURL is acceptable until real SDK calls are made
// (the session builds its SDK lazily on first use).
func NewSessionStore(profile, indexerURL string) (*SessionStore, error) {
	s, err := gitarchive.NewSession(profile, indexerURL)
	if err != nil {
		return nil, err
	}
	return &SessionStore{session: s}, nil
}

// NewSessionStoreFromSession wraps an existing session so CLI git commands
// (watch/status) can share the session they already opened for DB access. The
// returned store does NOT own the session — the caller manages Close on the
// underlying session.
func NewSessionStoreFromSession(s *gitarchive.Session) *SessionStore {
	return &SessionStore{session: s}
}

// ForLocker scopes this store to the account locker named locker (the locker
// portion of a `pinner::<locker>` remote URL) and returns the store for
// chaining. Data operations against a locker are meaningless without it.
func (s *SessionStore) ForLocker(locker string) *SessionStore {
	s.locker = locker
	return s
}

// Close releases SDK-held resources.
func (s *SessionStore) Close() error {
	if s.session == nil {
		return nil
	}
	return s.session.Close()
}

// ParseLockedRemote reports whether url is an account-scoped `pinner::<locker>`
// remote (as opposed to the profile-less `pinner::share/...` path) and, if so,
// returns the locker name embedded in it.
func ParseLockedRemote(url string) (string, bool) {
	if !strings.HasPrefix(url, "pinner::") {
		return "", false
	}
	locker := strings.TrimPrefix(url, "pinner::")
	if locker == "" || strings.HasPrefix(locker, "share/") {
		return "", false
	}
	return locker, true
}
