package system

import "syscall"

func readDiskUsage(path string) (DiskInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return DiskInfo{}, err
	}
	blockSize := uint64(stat.Bsize)
	return DiskInfo{
		Path:           path,
		TotalBytes:     stat.Blocks * blockSize,
		UsedBytes:      (stat.Blocks - stat.Bfree) * blockSize,
		AvailableBytes: stat.Bavail * blockSize,
	}, nil
}
