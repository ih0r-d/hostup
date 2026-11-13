package system

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
)

type MemoryInfo struct {
	TotalBytes     uint64 `json:"total_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
}

type DiskInfo struct {
	Path           string `json:"path"`
	TotalBytes     uint64 `json:"total_bytes"`
	UsedBytes      uint64 `json:"used_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
}

type CPUInfo struct {
	LogicalCPUs int `json:"logical_cpus"`
}

type Diagnostics struct {
	Platform Platform   `json:"platform"`
	Memory   MemoryInfo `json:"memory"`
	Disk     DiskInfo   `json:"disk"`
	CPU      CPUInfo    `json:"cpu"`
}

// Diagnose collects read-only Linux diagnostics without requiring privileges.
func Diagnose() (Diagnostics, error) {
	platform, err := DetectPlatform()
	if err != nil {
		return Diagnostics{}, err
	}
	return collectDiagnostics(platform, os.ReadFile, readDiskUsage)
}

func collectDiagnostics(platform Platform, readFile func(string) ([]byte, error), diskUsage func(string) (DiskInfo, error)) (Diagnostics, error) {
	data, err := readFile("/proc/meminfo")
	if err != nil {
		return Diagnostics{}, fmt.Errorf("read memory information: %w", err)
	}
	memory, err := parseMemory(bytes.NewReader(data))
	if err != nil {
		return Diagnostics{}, fmt.Errorf("parse memory information: %w", err)
	}
	disk, err := diskUsage("/")
	if err != nil {
		return Diagnostics{}, fmt.Errorf("read root filesystem usage: %w", err)
	}
	return Diagnostics{
		Platform: platform,
		Memory:   memory,
		Disk:     disk,
		CPU:      CPUInfo{LogicalCPUs: runtime.NumCPU()},
	}, nil
}

func parseMemory(reader io.Reader) (MemoryInfo, error) {
	var memory MemoryInfo
	var totalFound, availableFound bool
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok || (key != "MemTotal" && key != "MemAvailable") {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) != 2 || fields[1] != "kB" {
			return MemoryInfo{}, fmt.Errorf("invalid %s: expected a value in kB", key)
		}
		kilobytes, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return MemoryInfo{}, fmt.Errorf("invalid %s: %w", key, err)
		}
		if kilobytes > ^uint64(0)/1024 {
			return MemoryInfo{}, fmt.Errorf("%s overflows bytes", key)
		}
		if key == "MemTotal" {
			memory.TotalBytes, totalFound = kilobytes*1024, true
		} else {
			memory.AvailableBytes, availableFound = kilobytes*1024, true
		}
	}
	if err := scanner.Err(); err != nil {
		return MemoryInfo{}, fmt.Errorf("read meminfo: %w", err)
	}
	if !totalFound || !availableFound {
		return MemoryInfo{}, fmt.Errorf("meminfo missing MemTotal or MemAvailable")
	}
	if memory.TotalBytes == 0 || memory.AvailableBytes > memory.TotalBytes {
		return MemoryInfo{}, fmt.Errorf("invalid memory totals")
	}
	return memory, nil
}
