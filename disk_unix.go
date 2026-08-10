//go:build !windows

package main

import (
	"fmt"
	"syscall"
)

func checkDiskSpace(path string, requiredBytes int64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil
	}

	freeBytes := int64(stat.Bavail) * int64(stat.Bsize)
	if freeBytes < requiredBytes {
		return fmt.Errorf("insufficient disk space (Free: %.2f GiB, Required: %.2f GiB)",
			float64(freeBytes)/(1024*1024*1024), float64(requiredBytes)/(1024*1024*1024))
	}
	return nil
}