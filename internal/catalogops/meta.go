package catalogops

import (
	"fmt"
	"strings"
)

// splitMetaPairs parses a list of `key=value` strings into a flat, alternating
// [key, value, key, value, ...] slice. The core UpdateMetadata / UpdatePin
// services require even-length pair slices.
//
// Iteration is in input order (deterministic). Each entry is split on the
// first `=`, and an empty key or a missing `=` is rejected.
func splitMetaPairs(pairs []string) ([]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(pairs)*2)
	for _, pair := range pairs {
		k, v, ok := strings.Cut(pair, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid metadata pair %q: expected key=value format", pair)
		}
		out = append(out, k, v)
	}
	return out, nil
}
