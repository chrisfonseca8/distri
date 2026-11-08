package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// readHardware reads the machine's CPU count and memory.
func readHardware() (HardwareInfo, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return HardwareInfo{}, fmt.Errorf("read machine memory: %w", err)
	}
	defer file.Close()

	var memoryBytes uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kilobytes, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return HardwareInfo{}, fmt.Errorf("parse MemTotal: %w", err)
			}
			memoryBytes = kilobytes * 1024
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return HardwareInfo{}, fmt.Errorf("scan /proc/meminfo: %w", err)
	}
	if memoryBytes == 0 {
		return HardwareInfo{}, fmt.Errorf("MemTotal was not present in /proc/meminfo")
	}
	return HardwareInfo{LogicalCPUs: runtime.NumCPU(), MemoryBytes: memoryBytes, Source: "runtime.NumCPU and /proc/meminfo as visible to the load-generator container"}, nil
}
