//go:build windows

package sys

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func osAvailableDiskSpace(dir string) (uint64, error) {
	ptr, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var freeBytesAvailable, totalNumberOfBytes, totalNumberOfFreeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &freeBytesAvailable, &totalNumberOfBytes, &totalNumberOfFreeBytes); err != nil {
		return 0, err
	}
	return freeBytesAvailable, nil
}

func osSameVolume(dirA, dirB string) bool {
	volA := filepath.VolumeName(dirA)
	volB := filepath.VolumeName(dirB)
	if volA != "" && volB != "" {
		return strings.EqualFold(volA, volB)
	}
	return strings.EqualFold(filepath.Clean(dirA), filepath.Clean(dirB))
}
