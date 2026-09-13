package gitarchive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.sia.tech/core/types"
	"gorm.io/gorm"

	"go.lumeweb.com/pinner-cli/internal/core/objmeta"
)

// This file implements `pinner git share`: it mints self-contained bearer share
// URLs for an archived locker's tip + packs and records them in a git.share
// ShareDocument object. The minted URLs are pre-signed (see SDKClient
// documentation): each embeds the object's encryption key, so anyone holding a
// URL can read the object WITHOUT a profile/app key. The profile-less helper
// share path (`pinner::share/...`) consumes exactly these URLs later.

// DefaultShareTTL is the default validity window for a minted share URL
// (30 days, per the plan). Callers override it with --until.
const DefaultShareTTL = 30 * 24 * time.Hour

// ShareDocument is the git.share object payload ("share bytes"). Order of
// PackURLs matches the tip's pack order so a consumer playing the packs back in
// order reconstructs the archived history. Until is a unix-seconds expiry.
type ShareDocument struct {
	Locker   string   `json:"locker"`
	Gen      int      `json:"gen"`
	Name     string   `json:"name"`
	TipURL   string   `json:"tip_url"`
	PackURLs []string `json:"pack_urls"`
	Until    int64    `json:"until"` // unix seconds
}

// PackRef is one archived pack referenced from a tip document. Key is the hex
// object key of the git.pack object; order within the tip's Packs slice is
// replay order.
type PackRef struct {
	Key    string `json:"key"`
	Full   bool   `json:"full"`
	Digest string `json:"digest"`
}

// TipBytes is the git.tip object payload ("tip bytes"). Refs maps ref names to
// their tip sha1 hashes; Packs lists the archived packs in replay order.
type TipBytes struct {
	Locker  string            `json:"locker"`
	Gen     int               `json:"gen"`
	Name    string            `json:"name"`
	Lineage []string          `json:"lineage"`
	Refs    map[string]string `json:"refs"`
	Packs   []PackRef         `json:"packs"`
	Prev    string            `json:"prev,omitempty"`
}

// ParseShareDocument decodes a git.share object payload.
func ParseShareDocument(data []byte) (*ShareDocument, error) {
	var doc ShareDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("gitarchive: parse share document: %w", err)
	}
	if doc.Locker == "" || doc.TipURL == "" {
		return nil, fmt.Errorf("gitarchive: share document missing locker or tip_url")
	}
	return &doc, nil
}

// ParseTipBytes decodes a git.tip object payload.
func ParseTipBytes(data []byte) (*TipBytes, error) {
	var tip TipBytes
	if err := json.Unmarshal(data, &tip); err != nil {
		return nil, fmt.Errorf("gitarchive: parse tip document: %w", err)
	}
	if tip.Locker == "" {
		return nil, fmt.Errorf("gitarchive: tip document missing locker")
	}
	return &tip, nil
}

// hashFromHex parses a 40-hex object key string into a types.Hash256. It
// delegates to the exported ParseObjectKey — the single source of truth for
// hex↔Hash256 conversion shared by the helper store and share minting.
func hashFromHex(s string) (types.Hash256, error) {
	return ParseObjectKey(s)
}

// ShareResult is the outcome of MintShare: the top-level share URL of the share
// document object, the validity window, and the inner URLs so callers can show
// nested detail under --verbose.
type ShareResult struct {
	// Locker is the archived locker that was shared.
	Locker string
	// Gen is the archived tip generation that was shared.
	Gen int
	// Name is the locker's display name.
	Name string
	// URL is the pre-signed share URL (of the git.share document object). This
	// is the value a consumer feeds to `git clone pinner::share/<URL>`.
	URL string
	// Until is when the pre-signed URLs expire.
	Until time.Time
	// TipURL is the pre-signed URL of the tip object.
	TipURL string
	// PackURLs are the pre-signed URLs of the archived packs, in replay order.
	PackURLs []string
	// Document is the serialized share document (for --verbose / tests).
	Document *ShareDocument
}

// FindTipForLocker returns the current git.tip object row for locker (the one
// with the greatest generation in its card body) and its decoded card body, or
// (nil, nil) when the locker has no archived tip.
func FindTipForLocker(db *gorm.DB, locker string) (*GitObject, *objmeta.TipBody, error) {
	var objs []GitObject
	if err := db.Where("locker = ? AND kind = ?", locker, string(objmeta.KindGitTip)).Find(&objs).Error; err != nil {
		return nil, nil, fmt.Errorf("gitarchive: find tip for %q: %w", locker, err)
	}
	var best *GitObject
	var bestBody *objmeta.TipBody
	for i := range objs {
		var b objmeta.TipBody
		if err := json.Unmarshal(objs[i].Body, &b); err != nil {
			continue // legacy/foreign tip row without a TipBody; skip
		}
		if best == nil || b.Gen > bestBody.Gen {
			cp := objs[i]
			bb := b
			best = &cp
			bestBody = &bb
		}
	}
	return best, bestBody, nil
}

// MintShare mints a share for locker valid until validUntil. It:
//
//  1. locates the current tip object and downloads/parses its tip document to
//     learn the archived packs and refs;
//  2. mints a pre-signed URL for the tip and one for every archived pack;
//  3. uploads a git.share ShareDocument object carrying those URLs;
//  4. mint a pre-signed URL for the share document itself (the share URL).
//
// All SDK calls go through the session's mockable SDKClient, so the whole flow
// is exercised against a fake SDK in tests.
func MintShare(ctx context.Context, s *Session, locker string, validUntil time.Time) (*ShareResult, error) {
	tipObj, tipBody, err := FindTipForLocker(s.DB, locker)
	if err != nil {
		return nil, err
	}
	if tipObj == nil {
		return nil, fmt.Errorf("gitarchive: locker %q has no archived tip; run `pinner git watch` to publish it first", locker)
	}

	sdk, err := s.ensureSDK()
	if err != nil {
		return nil, err
	}

	tipKey, err := hashFromHex(tipObj.ObjectKey)
	if err != nil {
		return nil, err
	}

	// Download the tip payload to learn name + the archived pack list.
	_, tipData, err := FetchObjectBytes(ctx, s, tipKey)
	if err != nil {
		return nil, err
	}
	tipDoc, err := ParseTipBytes(tipData)
	if err != nil {
		return nil, err
	}

	tipURL, err := sdk.CreateSharedObjectURL(ctx, tipKey, validUntil)
	if err != nil {
		return nil, fmt.Errorf("gitarchive: mint tip share URL: %w", err)
	}

	packURLs := make([]string, 0, len(tipDoc.Packs))
	for _, p := range tipDoc.Packs {
		pk, err := hashFromHex(p.Key)
		if err != nil {
			return nil, err
		}
		u, err := sdk.CreateSharedObjectURL(ctx, pk, validUntil)
		if err != nil {
			return nil, fmt.Errorf("gitarchive: mint pack share URL: %w", err)
		}
		packURLs = append(packURLs, u)
	}

	name := tipDoc.Name
	if name == "" {
		name = locker
	}
	gen := tipDoc.Gen
	if gen == 0 && tipBody != nil {
		gen = tipBody.Gen
	}

	doc := &ShareDocument{
		Locker:   locker,
		Gen:      gen,
		Name:     name,
		TipURL:   tipURL,
		PackURLs: packURLs,
		Until:    validUntil.Unix(),
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("gitarchive: marshal share document: %w", err)
	}
	shareBody, err := json.Marshal(objmeta.ShareBody{Locker: locker, Gen: gen, Until: validUntil.Unix()})
	if err != nil {
		return nil, fmt.Errorf("gitarchive: marshal share body: %w", err)
	}
	uploaded, err := UploadObject(ctx, s, objmeta.KindGitShare, shareBody, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}

	shareURL, err := sdk.CreateSharedObjectURL(ctx, uploaded.Key, validUntil)
	if err != nil {
		return nil, fmt.Errorf("gitarchive: mint share document URL: %w", err)
	}

	return &ShareResult{
		Locker:   locker,
		Gen:      gen,
		Name:     name,
		URL:      shareURL,
		Until:    validUntil,
		TipURL:   tipURL,
		PackURLs: packURLs,
		Document: doc,
	}, nil
}
