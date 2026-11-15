//go:build !linux

package system

import (
	"fmt"
	"runtime"
)

func readDiskUsage(path string) (DiskInfo, error) {
	return DiskInfo{}, fmt.Errorf("disk diagnostics unsupported on OS: %s", runtime.GOOS)
}
