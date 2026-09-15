package gitremote

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"go.sia.tech/core/types"
	"go.sia.tech/siastorage"

	"go.lumeweb.com/pinner/core/vault"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

// This file implements the profile-less helper share path. A share URL is a
// self-contained bearer credential (see internal/core/gitarchive.SDKClient):
// `pinner git share` mints pre-signed URLs for the tip + packs and records them
// in a git.share ShareDocument object. Anyone holding the top-level share URL
// can `git clone pinner::share/<URL>` WITHOUT a vault profile/app key — the
// Git remote-helper routes to a ShareStore that reads everything over the
// pre-signed URLs via DownloadSharedObject.

// shareRemotePrefix is the remote URL prefix that selects the profile-less
// share path in the helper (as opposed to the account-scoped `pinner::<locker>`
// path routed to SessionStore).
const shareRemotePrefix = "pinner::share/"

// ErrShareReadonly is returned when a push/delete is attempted against a share
// remote. Share archives are read-only by construction (their URLs carry the
// embedded key but no account write scope).
var ErrShareReadonly = errors.New("gitremote: share archive is read-only")

// ShareDownloader downloads self-contained bearer share objects given a
// pre-signed URL. *siastorage.SDK satisfies this interface directly; tests
// substitute an in-memory fake.
type ShareDownloader interface {
	// DownloadSharedObject streams an object's plaintext from a pre-signed
	// share URL (no profile/app key needed — the URL embeds the encryption key).
	DownloadSharedObject(ctx context.Context, sharedURL string, opts ...siastorage.DownloadOption) (io.ReadCloser, error)
	// Close releases SDK-held resources.
	Close() error
}

// ParseShareRemote reports whether url is a `pinner::share/...` remote and, if
// so, returns the pre-signed share-URL embedded in it. The profile-less helper
// path uses this to decide it needs no vault profile.
func ParseShareRemote(url string) (string, bool) {
	if !strings.HasPrefix(url, shareRemotePrefix) {
		return "", false
	}
	rest := url[len(shareRemotePrefix):]
	if rest == "" {
		return "", false
	}
	return rest, true
}

// ShareStore is a read-only Store backed by a ShareDocument reached over
// self-contained bearer URLs. List advertises the archived refs from the
// current tip; Download reconstructs a full pack of the requested tip by
// downloading every archived pack (in replay order) and re-encoding it
// in-process via go-git. It never touches a vault profile or app key.
type ShareStore struct {
	downloader ShareDownloader
	shareURL   string // pre-signed URL of the git.share document

	doc *gitarchive.ShareDocument // cached after the first List/Download
	tip *gitarchive.TipBytes      // cached current tip parsed from doc.TipURL
}

var _ Store = (*ShareStore)(nil)

// NewShareStore builds a profile-less store over a pre-signed share document
// URL. The downloader does the actual bearer reads.
func NewShareStore(d ShareDownloader, shareURL string) *ShareStore {
	return &ShareStore{downloader: d, shareURL: shareURL}
}

// NewShareStoreForRemote parses a `pinner::share/...` remote URL and returns a
// profile-less ShareStore for it. It returns an error when url is not a share
// remote.
func NewShareStoreForRemote(d ShareDownloader, remoteURL string) (*ShareStore, error) {
	shareURL, ok := ParseShareRemote(remoteURL)
	if !ok {
		return nil, fmt.Errorf("gitremote: %q is not a pinner share remote", remoteURL)
	}
	return NewShareStore(d, shareURL), nil
}

// Close releases the downloader.
func (s *ShareStore) Close() error {
	if s.downloader != nil {
		return s.downloader.Close()
	}
	return nil
}

// load downloads the share document and its tip once and caches them. All
// reads go through DownloadSharedObject (bearer URLs) — no profile is resolved.
func (s *ShareStore) load(ctx context.Context) error {
	if s.doc != nil && s.tip != nil {
		return nil
	}
	if s.downloader == nil {
		return fmt.Errorf("gitremote: share store has no downloader")
	}

	docRaw, err := readShared(ctx, s.downloader, s.shareURL)
	if err != nil {
		return fmt.Errorf("gitremote: read share document: %w", err)
	}
	doc, err := gitarchive.ParseShareDocument(docRaw)
	if err != nil {
		return err
	}
	tipRaw, err := readShared(ctx, s.downloader, doc.TipURL)
	if err != nil {
		return fmt.Errorf("gitremote: read share tip: %w", err)
	}
	tip, err := gitarchive.ParseTipBytes(tipRaw)
	if err != nil {
		return err
	}
	s.doc = doc
	s.tip = tip
	return nil
}

func readShared(ctx context.Context, d ShareDownloader, url string) ([]byte, error) {
	rc, err := d.DownloadSharedObject(ctx, url)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// List advertises the archived refs from the share tip, sorted by name.
func (s *ShareStore) List(ctx context.Context, forPush bool) ([]Ref, error) {
	if err := s.load(ctx); err != nil {
		return nil, err
	}
	refs := make([]Ref, 0, len(s.tip.Refs))
	for name, hash := range s.tip.Refs {
		refs = append(refs, Ref{Name: name, Hash: plumbing.NewHash(hash)})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs, nil
}

// Download reconstructs a full pack of everything reachable from targetHash by
// downloading every archived pack over its pre-signed URL (in the share
// document's pack order), ingesting them into an in-memory store, and
// re-encoding a combined pack in-process via the shared replay helper.
// refName is not used beyond validating the ref exists in the tip.
func (s *ShareStore) Download(ctx context.Context, refName, targetHash string) (io.ReadCloser, error) {
	if err := s.load(ctx); err != nil {
		return nil, err
	}
	// Share fetches are whole-history: the archive only ever sends what the
	// consumer lacks, so a full pack of the requested tip is always sufficient.
	// Download every pack (replay order), verify its SHA-256 digest against the
	// digest recorded in the tip document (when present), then re-encode a
	// single full pack.
	openers := make([]func() (io.ReadCloser, error), 0, len(s.doc.PackURLs))
	digests := make([]string, 0, len(s.tip.Packs))
	for _, p := range s.tip.Packs {
		digests = append(digests, p.Digest)
	}
	for i, u := range s.doc.PackURLs {
		u := u
		want := ""
		if i < len(digests) {
			want = digests[i]
		}
		openers = append(openers, func() (io.ReadCloser, error) {
			rc, err := s.downloader.DownloadSharedObject(ctx, u)
			if err != nil {
				return nil, fmt.Errorf("gitremote: download shared pack: %w", err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("gitremote: read shared pack: %w", err)
			}
			if want != "" {
				if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
					return nil, fmt.Errorf("gitremote: shared pack digest mismatch: tip declares %s, payload computes %s", want, got)
				}
			}
			return io.NopCloser(bytes.NewReader(data)), nil
		})
	}
	pack, err := replayPacksAndEncode(openers, plumbing.NewHash(targetHash))
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(pack)), nil
}

// Upload rejects writes — share archives are read-only.
func (s *ShareStore) Upload(ctx context.Context, dstRef, srcHash string, pack io.Reader) error {
	return fmt.Errorf("%w: upload to a share", ErrShareReadonly)
}

// Delete rejects writes — share archives are read-only.
func (s *ShareStore) Delete(ctx context.Context, dstRef string) error {
	return fmt.Errorf("%w: delete from a share", ErrShareReadonly)
}

// NewStoreFromRemote routes a helper remote URL to the right Store:
//
//   - `pinner::share/...` → a profile-less ShareStore (no vault profile);
//   - anything else (e.g. `pinner::<locker>`) → the profile-backed SessionStore.
//
// main() calls this with os.Args[1] so the helper serves both the account-scoped
// archive (`pinner::<locker>`) and the profile-less share clone
// (`pinner::share/<URL>`).
func NewStoreFromRemote(ctx context.Context, remoteURL string) (Store, error) {
	if shareURL, ok := ParseShareRemote(remoteURL); ok {
		d, err := newSharedDownloader(os.Getenv("PINNER_SIA_INDEXER"))
		if err != nil {
			return nil, err
		}
		return NewShareStore(d, shareURL), nil
	}
	// Account-scoped path (`pinner::<locker>`): a profile-backed SessionStore
	// scoped to the locker embedded in the remote URL.
	if locker, ok := ParseLockedRemote(remoteURL); ok {
		s, err := NewSessionStoreFromEnv(ctx)
		if err != nil {
			return nil, err
		}
		return s.ForLocker(locker), nil
	}
	// Fallback for a bare/malformed remote: an unscoped session store (its data
	// operations will report a missing-locker/not-wired error as appropriate).
	return NewSessionStoreFromEnv(ctx)
}

// newSharedDownloader builds a Sia SDK used only for self-contained shared
// reads. It has no vault profile/app key: the shared object's encryption key is
// embedded in the pre-signed URL, so the SDK app key merely authenticates the
// (throwaway) indexer session. Requires the indexer origin via
// PINNER_SIA_INDEXER until Step 10 wires a configured default.
func newSharedDownloader(indexerURL string) (ShareDownloader, error) {
	if indexerURL == "" {
		return nil, fmt.Errorf("gitremote: shared download needs a Sia indexer URL (set PINNER_SIA_INDEXER)")
	}
	key := types.GeneratePrivateKey()
	metadata := siastorage.AppMetadata{
		ID:          vault.AppID(),
		Name:        "Pinner CLI Git Archive (shared)",
		Description: "Read-only shared git archive access",
		ServiceURL:  indexerURL,
	}
	builder := siastorage.NewBuilder(indexerURL, metadata)
	sdk, err := builder.SDK(key)
	if err != nil {
		return nil, fmt.Errorf("gitremote: build shared SDK: %w", err)
	}
	return sdk, nil
}
