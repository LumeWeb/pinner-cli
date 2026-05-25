//go:build linux

package cli

import "syscall"

func availableMemory() int64 {
	var si syscall.Sysinfo_t
	if err := syscall.Sysinfo(&si); err != nil {
		return 0
	}
	return int64(si.Freeram) * int64(si.Unit)
}
