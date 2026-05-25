//go:build darwin

package cli

import (
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func availableMemory() int64 {
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0
	}

	pageSize, err := unix.SysctlUint32("hw.pagesize")
	if err != nil {
		return 0
	}

	var freePages, inactivePages int64

	for line := range strings.SplitSeq(string(out), "\n") {
		i := strings.IndexRune(line, ':')
		if i < 0 {
			continue
		}
		key := line[:i]
		valStr := strings.TrimSpace(strings.TrimRight(line[i+1:], "."))

		val, err := strconv.ParseInt(valStr, 10, 64)
		if err != nil {
			continue
		}

		switch key {
		case "Pages free":
			freePages = val
		case "Pages inactive":
			inactivePages = val
		}
	}

	return (freePages + inactivePages) * int64(pageSize)
}
