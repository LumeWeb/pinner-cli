//go:build !windows

package cli

import "os"

// replaceDownloadedFile atomically replaces the destination with the temp
// file. On Unix, os.Rename is atomic and overwrites any existing destination.
func replaceDownloadedFile(tmp, dest string) error {
	return os.Rename(tmp, dest)
}
