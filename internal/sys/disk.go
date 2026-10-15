package sys

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrInsufficientDiskSpace 表示目标目录或临时目录所在的卷可用空间不足。
var ErrInsufficientDiskSpace = errors.New("磁盘可用空间不足")

var availableDiskSpaceProvider = osAvailableDiskSpace

// SetAvailableDiskSpaceForTest 供测试替换底层磁盘空间查询函数，返回恢复函数。
func SetAvailableDiskSpaceForTest(fn func(dir string) (uint64, error)) (restore func()) {
	old := availableDiskSpaceProvider
	if fn == nil {
		availableDiskSpaceProvider = osAvailableDiskSpace
	} else {
		availableDiskSpaceProvider = fn
	}
	return func() { availableDiskSpaceProvider = old }
}

// AvailableDiskSpace 返回 path 所在磁盘卷的当前可用字节数。
// 若 path 尚不存在，自动向上回溯到最近一个已存在的父目录进行查询。
func AvailableDiskSpace(path string) (uint64, error) {
	target, err := resolveExistingDir(path)
	if err != nil {
		return 0, err
	}
	return availableDiskSpaceProvider(target)
}

// CheckDiskSpace 检查 path 所在卷是否至少有 requiredBytes 的可用空间。
// 若 requiredBytes <= 0 或无法从操作系统获取卷信息（如特殊虚拟文件系统），则放行不误杀。
func CheckDiskSpace(path string, requiredBytes int64) error {
	if requiredBytes <= 0 {
		return nil
	}
	avail, err := AvailableDiskSpace(path)
	if err != nil {
		// 无法查询空间信息时不阻断下载，交由实际文件写入错误兜底。
		return nil
	}
	if avail < uint64(requiredBytes) {
		return fmt.Errorf("%w：需要 %d 字节，当前可用 %d 字节（路径：%s）",
			ErrInsufficientDiskSpace, requiredBytes, avail, path)
	}
	return nil
}

// SameVolume 判断两个路径是否位于同一个磁盘卷/挂载点上。
// 同卷内的重命名移动不额外占用第二份空间，跨卷移动则需要目标卷同时具备完整空间。
func SameVolume(pathA, pathB string) bool {
	dirA, errA := resolveExistingDir(pathA)
	dirB, errB := resolveExistingDir(pathB)
	if errA != nil || errB != nil {
		return false
	}
	return osSameVolume(dirA, dirB)
}

func resolveExistingDir(path string) (string, error) {
	if path == "" {
		path = "."
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	curr := abs
	for {
		info, statErr := os.Stat(curr)
		if statErr == nil {
			if info.IsDir() {
				return curr, nil
			}
			return filepath.Dir(curr), nil
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			return abs, statErr
		}
		curr = parent
	}
}
