package main

import (
	"os"
	"strconv"
	"strings"
)

// readCgroupCPUUsec returns the CPU time, in microseconds, consumed by this
// container so far (cgroup v2). The load generator uses it to report its own CPU
// use, so a saturated generator is not mistaken for a server limit.
func readCgroupCPUUsec() (uint64, bool) {
	data, err := os.ReadFile("/sys/fs/cgroup/cpu.stat")
	if err != nil {
		return 0, false
	}
	return parseCPUStatUsage(string(data))
}

func parseCPUStatUsage(content string) (uint64, bool) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "usage_usec" {
			value, err := strconv.ParseUint(fields[1], 10, 64)
			return value, err == nil
		}
	}
	return 0, false
}

// readCgroupMemoryBytes returns the container's current memory use (cgroup v2).
func readCgroupMemoryBytes() (uint64, bool) {
	data, err := os.ReadFile("/sys/fs/cgroup/memory.current")
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return value, err == nil
}
