//go:build gitarchive_sia

package gitremote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/go-git/go-git/v5/plumbing"

	"go.lumeweb.com/pinner-cli/internal/core/gitarchive"
)

// This file implements the REAL Sia-backed SessionStore, compiled in only with
// the `gitarchive_sia` build tag (opt-in, since it requires a live Sia indexer
// configuration to be meaningful). It drives the actual Sia SDK through the
// gitarchive session: pack/tip upload via gitarchive.UploadPack/UploadTip,
// download over gitarchive.FetchObjectBytes (which verifies each card digest),
// and generation CAS on publish. Every Git operation still runs in-process via
// go-git — no git subprocess, no shell-outs.

// loadTip returns the current archived tip for the store's locker and its
// on-disk object key, or (nil, nil) when the locker has no tip yet. It
// downloads and digest-verifies the tip payload each time.
func (s *SessionStore) loadTip(ctx context.Context) (*gitarchive.TipBytes, *gitarchive.GitObject, error) {
	if s.session == nil {
		return nil, nil, errors.New("gitremote: session store has no session")
	}
	tipObj, _, err := gitarchive.FindTipForLocker(s.session.DB, s.locker)
	if err != nil {
		return nil, nil, err
	}
	if tipObj == nil {
		return nil, nil, nil
	}
	key, err := gitarchive.ParseObjectKey(tipObj.ObjectKey)
	if err != nil {
		return nil, nil, err
	}
	_, data, err := gitarchive.FetchObjectBytes(ctx, s.session, key)
	if err != nil {
		return nil, nil, err
	}
	tip, err := gitarchive.ParseTipBytes(data)
	if err != nil {
		return nil, nil, err
	}
	return tip, tipObj, nil
}

// List advertises the archived refs by loading the current tip's refs map,
// sorted by name (deterministic protocol output).
func (s *SessionStore) List(ctx context.Context, forPush bool) ([]Ref, error) {
	tip, _, err := s.loadTip(ctx)
	if err != nil {
		return nil, err
	}
	if tip == nil {
		return nil, nil
	}
	refs := make([]Ref, 0, len(tip.Refs))
	for name, hash := range tip.Refs {
		refs = append(refs, Ref{Name: name, Hash: plumbing.NewHash(hash)})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs, nil
}

// Download reconstructs a full pack of everything reachable from targetHash by
// downloading every archived pack (in replay order) over the Sia SDK and
// re-encoding a single full pack in-process. Each pack's payload is
// digest-verified against its card by FetchObjectBytes before replay, so a
// corrupted/tampered pack surfaces as an error here.
func (s *SessionStore) Download(ctx context.Context, refName, targetHash string) (io.ReadCloser, error) {
	if s.locker == "" {
		return nil, errors.New("gitremote: session store has no locker")
	}
	tip, _, err := s.loadTip(ctx)
	if err != nil {
		return nil, err
	}
	if tip == nil {
		return nil, fmt.Errorf("gitremote: locker %q has no archived tips to download", s.locker)
	}
	openers := make([]func() (io.ReadCloser, error), 0, len(tip.Packs))
	for _, p := range tip.Packs {
		p := p
		openers = append(openers, func() (io.ReadCloser, error) {
			key, err := gitarchive.ParseObjectKey(p.Key)
			if err != nil {
				return nil, err
			}
			_, data, err := gitarchive.FetchObjectBytes(ctx, s.session, key)
			if err != nil {
				return nil, err
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

// Upload persists the incoming pack as a new git.pack card and publishes a new
// git.tip card (gen+1, prev=old tip) carrying the updated refs and the appended
// pack. It refuses to publish if the generation moved since it read the old tip
// (gen CAS), so a concurrent upload cannot silently clobber a newer archive.
func (s *SessionStore) Upload(ctx context.Context, dstRef, srcHash string, pack io.Reader) error {
	if s.locker == "" {
		return errors.New("gitremote: session store has no locker")
	}
	tip, tipObj, err := s.loadTip(ctx)
	if err != nil {
		return err
	}

	oldRefs := map[string]string{}
	var oldPacks []gitarchive.PackRef
	oldKey, gen := "", 0
	prevName, prevLineage := "", []string{}
	if tip != nil {
		oldRefs = tip.Refs
		oldPacks = tip.Packs
		oldKey = tipObj.ObjectKey
		gen = tip.Gen
		prevName, prevLineage = tip.Name, tip.Lineage
	}

	newRefs := copyStringMap(oldRefs)
	newRefs[dstRef] = srcHash
	newPacks := append([]gitarchive.PackRef{}, oldPacks...)

	data, err := io.ReadAll(pack)
	if err != nil {
		return fmt.Errorf("gitremote: read incoming pack: %w", err)
	}
	// An empty pack (incremental encode found nothing new) adds no object — the
	// ref move alone is a new tip over the existing pack set.
	if len(data) > 0 {
		uo, err := gitarchive.UploadPack(ctx, s.session, s.locker,
			fmt.Sprintf("%s:%s:%d", dstRef, srcHash, gen), false, bytes.NewReader(data))
		if err != nil {
			return err
		}
		newPacks = append(newPacks, gitarchive.PackRef{Key: uo.Key.String(), Full: false, Digest: uo.Digest})
	}

	// Gen CAS: publish must not move a generation that changed after we read it.
	if err := s.casPublish(ctx, oldKey); err != nil {
		return err
	}

	newGen := gen + 1
	newTip := &gitarchive.TipBytes{
		Locker:  s.locker,
		Gen:     newGen,
		Name:    prevName,
		Lineage: prevLineage,
		Refs:    newRefs,
		Packs:   newPacks,
		Prev:    oldKey,
	}
	return s.publishTip(ctx, newTip, newGen, oldKey)
}

// Delete publishes a new tip with dstRef removed from the refs map (the
// protocol's `:dst` delete-ref push). No pack object is produced; only the
// reference set changes.
func (s *SessionStore) Delete(ctx context.Context, dstRef string) error {
	if s.locker == "" {
		return errors.New("gitremote: session store has no locker")
	}
	tip, tipObj, err := s.loadTip(ctx)
	if err != nil {
		return err
	}
	if tip == nil {
		return nil // nothing archived to delete
	}
	newRefs := copyStringMap(tip.Refs)
	delete(newRefs, dstRef)
	oldKey := tipObj.ObjectKey

	if err := s.casPublish(ctx, oldKey); err != nil {
		return err
	}

	newGen := tip.Gen + 1
	newTip := &gitarchive.TipBytes{
		Locker:  s.locker,
		Gen:     newGen,
		Name:    tip.Name,
		Lineage: tip.Lineage,
		Refs:    newRefs,
		Packs:   tip.Packs,
		Prev:    oldKey,
	}
	return s.publishTip(ctx, newTip, newGen, oldKey)
}

// casPublish verifies the archived tip has not moved since the caller read it
// ("" meaning no tip existed before). It is the generation compare-and-swap: if
// the current tip key differs from the one the caller based its new tip on, the
// publish is refused. This is a local best-effort CAS over the profile cache DB
// (the coordination point for single-profile writers); it surfaces a clear
// conflict instead of silently overwriting a newer generation.
func (s *SessionStore) casPublish(ctx context.Context, expectKey string) error {
	_, tipObj, err := s.loadTip(ctx)
	if err != nil {
		return err
	}
	currentKey := ""
	if tipObj != nil {
		currentKey = tipObj.ObjectKey
	}
	if currentKey != expectKey {
		return fmt.Errorf(
			"gitremote: publish refused: archive generation moved (concurrent publish); re-fetch and retry")
	}
	return nil
}

// publishTip uploads the new tip card and bumps the tracked TipGen in git_repos.
func (s *SessionStore) publishTip(ctx context.Context, tip *gitarchive.TipBytes, gen int, prev string) error {
	raw, err := json.Marshal(tip)
	if err != nil {
		return fmt.Errorf("gitremote: marshal tip document: %w", err)
	}
	if _, err := gitarchive.UploadTip(ctx, s.session, s.locker, gen, prev, bytes.NewReader(raw)); err != nil {
		return err
	}
	if err := s.session.DB.Model(&gitarchive.GitRepo{}).
		Where("locker = ?", s.locker).
		Update("tip_gen", gen).Error; err != nil {
		return fmt.Errorf("gitremote: record tip gen for %q: %w", s.locker, err)
	}
	return nil
}

// copyStringMap returns a shallow copy of m (nil-safe).
func copyStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
