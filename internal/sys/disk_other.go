//go:build !windows

package sys

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func osAvailableDiskSpace(dir string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}

func osSameVolume(dirA, dirB string) bool {
	infoA, errA := os.Stat(dirA)
	infoB, errB := os.Stat(dirB)
	if errA != nil || errB != nil {
		return false
	}
	statA, okA := infoA.Sys().(*syscall.Stat_t)
	statB, okB := infoB.Sys().(*syscall.Stat_t)
	if okA && okB {
		return statA.Dev == statB.Dev
	}
	return false
}
