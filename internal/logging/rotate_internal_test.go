package logging

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// 日志必须按大小滚动并封顶保留个数：这是「不许单文件无限堆叠」那条要求的落点。
// 用小的阈值直接驱动写入器，避免为了触发滚动而写满真实阈值。
func TestRotatingWriter_RotatesAndBoundsHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LogFileName)
	writer, err := newRotatingWriter(path, 120, 3)
	if err != nil {
		t.Fatalf("failed to create rotating writer: %v", err)
	}
	defer func() { _ = writer.Close() }()

	// 每条记录 100 字节，阈值 120 字节 → 每次写入都触发一次滚动。
	for i := range 10 {
		if _, err := writer.Write([]byte(strings.Repeat(string(rune('a'+i)), 99) + "\n")); err != nil {
			t.Fatalf("write %d failed: %v", i, err)
		}
	}

	files := logFilesIn(t, dir)
	if len(files) != 3 {
		t.Fatalf("expected current + 2 rotated files, got %v", files)
	}
	if files[0] != LogFileName {
		t.Fatalf("expected %s to be the newest file, got %v", LogFileName, files)
	}
	if _, err := os.Stat(filepath.Join(dir, rotatedFile(3))); !os.IsNotExist(err) {
		t.Fatalf("expected the oldest rotation to be dropped, %s still exists", rotatedFile(3))
	}

	// 最新的记录必须落在当前文件里；最旧的记录必须已被丢弃。
	current := readLog(t, path)
	if !strings.Contains(current, strings.Repeat("j", 99)) {
		t.Fatalf("expected the newest record in %s, got %q", LogFileName, current)
	}
	all := ""
	for _, name := range files {
		all += readLog(t, filepath.Join(dir, name))
	}
	if strings.Contains(all, strings.Repeat("a", 99)) {
		t.Fatal("expected the oldest record to be rotated out")
	}
}

// 轮转失败（改名被杀软或索引器占住）不能让日志永久失效：必须退回继续追加，下一次再试。
func TestRotatingWriter_SurvivesRotationFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LogFileName)
	writer, err := newRotatingWriter(path, 60, 3)
	if err != nil {
		t.Fatalf("failed to create rotating writer: %v", err)
	}
	defer func() { _ = writer.Close() }()

	blocked := errors.New("rename blocked by another process")
	restore := renameFile
	renameFile = func(_, _ string) error { return blocked }
	defer func() { renameFile = restore }()

	record := strings.Repeat("y", 39) + "\n"
	for i := range 3 {
		if _, err := writer.Write([]byte(record)); err != nil {
			t.Fatalf("write %d failed after a blocked rotation: %v", i, err)
		}
	}

	content := readLog(t, path)
	if strings.Count(content, strings.TrimSpace(record)) != 3 {
		t.Fatalf("expected all 3 records to survive a blocked rotation, got: %q", content)
	}
}

// 记录跨越阈值时也必须完整落在某一份文件里，不被截断。
func TestRotatingWriter_KeepsRecordWhole(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LogFileName)
	writer, err := newRotatingWriter(path, 1000, 2)
	if err != nil {
		t.Fatalf("failed to create rotating writer: %v", err)
	}
	defer func() { _ = writer.Close() }()

	record := strings.TrimSpace(strings.Repeat("x", 300))
	for range 4 {
		if _, err := writer.Write([]byte(record + "\n")); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}

	total := 0
	for _, name := range logFilesIn(t, dir) {
		for _, line := range strings.Split(strings.TrimSpace(readLog(t, filepath.Join(dir, name))), "\n") {
			if line == "" {
				continue
			}
			if line != record {
				t.Fatalf("record was split across rotations: %q", line)
			}
			total++
		}
	}
	if total != 4 {
		t.Fatalf("expected 4 intact records across rotated files, got %d", total)
	}
}

// logFilesIn 返回日志目录里的文件：当前文件在前，历史文件按编号升序（越靠前越新）。
func logFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read log dir: %v", err)
	}
	var files []string
	for _, entry := range entries {
		files = append(files, entry.Name())
	}
	sort.Slice(files, func(i, j int) bool { return rotationRank(files[i]) < rotationRank(files[j]) })
	return files
}

var rotationIndex = regexp.MustCompile(`\.(\d+)\.log$`)

func rotationRank(name string) int {
	if name == LogFileName {
		return 0
	}
	if match := rotationIndex.FindStringSubmatch(name); match != nil {
		if index, err := strconv.Atoi(match[1]); err == nil {
			return index
		}
	}
	return 1000
}

func rotatedFile(index int) string {
	return fmt.Sprintf("%s.%d.log", strings.TrimSuffix(LogFileName, ".log"), index)
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
