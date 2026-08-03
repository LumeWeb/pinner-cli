//go:build windows

package cli

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// replaceDownloadedFile atomically replaces the destination with the temp
// file. Windows os.Rename fails when the destination exists, so use
// MoveFileEx with MOVEFILE_REPLACE_EXISTING: the replacement is atomic — on
// failure the existing destination is left intact (unlike a remove-then-rename
// sequence, which would delete the original before the move and lose the user's
// previous file on a failed overwrite).
func replaceDownloadedFile(tmp, dest string) error {
	from, err := windows.UTF16PtrFromString(tmp)
	if err != nil {
		return fmt.Errorf("invalid temp path: %w", err)
	}
	to, err := windows.UTF16PtrFromString(dest)
	if err != nil {
		return fmt.Errorf("invalid destination path: %w", err)
	}
	const movefileReplaceExisting = 0x1
	if err := windows.MoveFileEx(from, to, movefileReplaceExisting); err != nil {
		return fmt.Errorf("replace file %s -> %s: %w", tmp, dest, err)
	}
	return nil
}
