//go:build windows

package main

import (
	"fmt"
	"golang.org/x/sys/windows"
)

func checkDiskSpace(path string, requiredBytes int64) error {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil
	}

	var freeBytesAvailable, totalNumberOfBytes, totalNumberOfFreeBytes uint64
	err = windows.GetDiskFreeSpaceEx(pathPtr, &freeBytesAvailable, &totalNumberOfBytes, &totalNumberOfFreeBytes)
	if err != nil {
		return nil
	}

	if int64(freeBytesAvailable) < requiredBytes {
		return fmt.Errorf("insufficient disk space (Free: %.2f GiB, Required: %.2f GiB)",
			float64(freeBytesAvailable)/(1024*1024*1024), float64(requiredBytes)/(1024*1024*1024))
	}
	return nil
}