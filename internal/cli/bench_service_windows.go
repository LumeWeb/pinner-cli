//go:build windows

package cli

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32         = windows.NewLazyDLL("kernel32.dll")
	procGlobalMemory = kernel32.NewProc("GlobalMemoryStatusEx")
)

func availableMemory() int64 {
	var status memoryStatusEx
	status.dwLength = uint32(unsafe.Sizeof(status))

	_, _, _ = procGlobalMemory.Call(uintptr(unsafe.Pointer(&status)))

	return int64(status.ullAvailPhys)
}

type memoryStatusEx struct {
	dwLength                uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}
