//go:build darwin

package cli

import (
	"os/exec"
	"strconv"
	"strings"
)

func availableMemory() int64 {
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0
	}

	var pageSize int64
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
		case "Mach Virtual Memory Statistics: (page size of":
			pageSize = val
		case "Pages free":
			freePages = val
		case "Pages inactive":
			inactivePages = val
		}
	}

	if pageSize <= 0 {
		pageSize = 16384
	}

	return (freePages + inactivePages) * pageSize
}
