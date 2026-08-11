//go:build windows

package cli

import (
	"os"
	"testing"
)

// assertPrivateTempPerms is a no-op on Windows, where Go's os.File.Mode reports
// permissive POSIX-style bits (0777/0666) regardless of actual ACLs, so the
// Unix 0700/0600 assertions cannot be meaningfully enforced here.
func assertPrivateTempPerms(t *testing.T, tf *os.File) {
	t.Helper()
	_ = tf
}
