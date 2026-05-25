//go:build !linux && !darwin && !windows

package cli

func availableMemory() int64 {
	return 0
}
