package gitremote

import (
	"fmt"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
)

// replayPacksAndEncode reconstructs a full pack of everything reachable from
// target by playing every archived pack (in replay order) into an in-memory
// object store and then re-encoding a single full pack. It is the single DRY
// implementation shared by the account-scoped SessionStore.Download (which
// opens packs by object key) and the profile-less ShareStore.Download (which
// opens packs over pre-signed share URLs): each caller only supplies the
// ordered list of pack-openers, so the replay/encode logic lives in one place.
//
// opening is called lazily per pack and returns a reader over the raw pack
// bytes; every reader is closed after ingest. All object work runs in-process
// via go-git — no git subprocess.
func replayPacksAndEncode(openers []func() (io.ReadCloser, error), target plumbing.Hash) ([]byte, error) {
	mem := memory.NewStorage()
	for i, open := range openers {
		rc, err := open()
		if err != nil {
			return nil, fmt.Errorf("gitremote: open pack %d: %w", i, err)
		}
		ingestErr := IngestPack(mem, rc)
		rc.Close()
		if ingestErr != nil {
			return nil, fmt.Errorf("gitremote: ingest pack %d: %w", i, ingestErr)
		}
	}
	pack, err := EncodeIncrementalPack(mem, []plumbing.Hash{target}, nil)
	if err != nil {
		return nil, err
	}
	return pack, nil
}
