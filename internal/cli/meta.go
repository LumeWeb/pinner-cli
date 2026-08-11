package cli

import (
	"fmt"
	"strings"
)

func parseMetaPairs(pairs []string) (map[string]string, error) {
	result := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 || parts[0] == "" {
			return nil, fmt.Errorf("invalid metadata pair %q: expected key=value format", pair)
		}
		result[parts[0]] = parts[1]
	}
	return result, nil
}

func metaMapToSlice(m map[string]string) []string {
	slice := make([]string, 0, len(m)*2)
	for k, v := range m {
		slice = append(slice, k, v)
	}
	return slice
}
