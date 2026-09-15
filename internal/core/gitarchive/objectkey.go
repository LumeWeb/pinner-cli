package gitarchive

import (
	"fmt"

	"go.sia.tech/core/types"
)

// ParseObjectKey parses the 40-hex hex representation of a Sia object key (the
// string form stored in git_objects.ObjectKey and referenced by tip/share
// documents) into a types.Hash256. It is the single source of truth for
// converting between the on-disk hex form and the SDK's binary key, so all
// callers — helper store, share minting, downloads — agree on the format.
func ParseObjectKey(s string) (types.Hash256, error) {
	var key types.Hash256
	if err := key.UnmarshalText([]byte(s)); err != nil {
		return types.Hash256{}, fmt.Errorf("gitarchive: invalid object key %q: %w", s, err)
	}
	return key, nil
}
