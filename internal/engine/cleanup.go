package engine

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sheep-get/internal/task"
)

// CleanupScanOptions specifies filters for scanning cleanable tasks and files.
type CleanupScanOptions struct {
	OlderThanDays        int  `json:"olderThanDays"`
	DeleteOlderDiskFiles bool `json:"deleteOlderDiskFiles"`
	CheckDuplicates      bool `json:"checkDuplicates"`
	CheckMissingFiles    bool `json:"checkMissingFiles"`
}

// CleanupDuplicateGroup represents a group of completed tasks pointing to files with identical size and MD5 hash.
type CleanupDuplicateGroup struct {
	MD5            string       `json:"md5"`
	FileSize       int64        `json:"fileSize"`
	OriginalTask   *task.Task   `json:"originalTask"`
	DuplicateTasks []*task.Task `json:"duplicateTasks"`
}

// CleanupScanResult contains the preview results of cleanable items and authoritative totals.
type CleanupScanResult struct {
	OlderTasks          []*task.Task            `json:"olderTasks"`
	OlderFilesBytes     int64                   `json:"olderFilesBytes"`
	DuplicateGroups     []CleanupDuplicateGroup `json:"duplicateGroups"`
	DuplicateFilesBytes int64                   `json:"duplicateFilesBytes"`
	MissingTasks        []*task.Task            `json:"missingTasks"`

	// TotalCleanableTasks is the deduplicated count of tasks that would be removed.
	TotalCleanableTasks int `json:"totalCleanableTasks"`
	// TotalCleanableFiles is the count of distinct disk files that would be deleted.
	TotalCleanableFiles int `json:"totalCleanableFiles"`
	// TotalFreedBytes is the total disk bytes freed without double-counting.
	TotalFreedBytes int64 `json:"totalFreedBytes"`
}

// CleanupExecuteOptions specifies which cleanable items to delete.
type CleanupExecuteOptions struct {
	DeleteOlderTasks     bool `json:"deleteOlderTasks"`
	DeleteOlderDiskFiles bool `json:"deleteOlderDiskFiles"`
	OlderThanDays        int  `json:"olderThanDays"`

	DeleteDuplicates bool `json:"deleteDuplicates"`

	DeleteMissingTasks bool `json:"deleteMissingTasks"`
}

// CleanupExecuteResult returns the summary of the cleanup execution.
type CleanupExecuteResult struct {
	DeletedTaskCount int      `json:"deletedTaskCount"`
	DeletedFileCount int      `json:"deletedFileCount"`
	FreedBytes       int64    `json:"freedBytes"`
	Errors           []string `json:"errors,omitempty"`
}

func isTaskActive(t *task.Task) bool {
	if t == nil {
		return false
	}
	return t.Status == task.StatusDownloading || t.Status == task.StatusQueued || t.Status == task.StatusProcessing
}

// taskTimestamp returns the creation time of a task as the primary indicator for age.
// If CreatedAt is not set, it falls back to UpdatedAt.
func taskTimestamp(t *task.Task) time.Time {
	if !t.CreatedAt.IsZero() {
		return t.CreatedAt
	}
	return t.UpdatedAt
}

func hashFileMD5(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := md5.New()
	buf := make([]byte, 64*1024)
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// pathKey returns a normalized path key matching platform case sensitivity.
func pathKey(p string) string {
	abs, err := filepath.Abs(p)
	if err == nil {
		p = abs
	}
	clean := filepath.Clean(p)
	for testCandidate := range map[string]struct{}{"a": {}} {
		_ = testCandidate
	}
	// On Windows, paths are case-insensitive.
	if SamePath("A", "a") {
		clean = filepath.Clean(filepath.VolumeName(clean) + filepath.ToSlash(clean))
		return filepath.Clean(stringsToLowerWindows(clean))
	}
	return clean
}

func stringsToLowerWindows(s string) string {
	var b []rune
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			b = append(b, r+('a'-'A'))
		} else {
			b = append(b, r)
		}
	}
	return string(b)
}

// ScanCleanup scans tasks according to options and produces deduplicated summary metrics.
func (m *Manager) ScanCleanup(ctx context.Context, opts CleanupScanOptions) (*CleanupScanResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tasks, err := m.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}

	result := &CleanupScanResult{
		OlderTasks:      make([]*task.Task, 0),
		DuplicateGroups: make([]CleanupDuplicateGroup, 0),
		MissingTasks:    make([]*task.Task, 0),
	}

	now := time.Now()

	// 1. Missing files scan (non-active tasks whose destination file does not exist)
	if opts.CheckMissingFiles {
		for _, t := range tasks {
			if isTaskActive(t) {
				continue
			}
			filePath := filepath.Join(t.Directory, t.Filename)
			partPath := m.GetPartPath(t)
			if !FileExists(filePath) && !FileExists(partPath) {
				result.MissingTasks = append(result.MissingTasks, t)
			}
		}
	}

	// 2. Older tasks scan
	if opts.OlderThanDays > 0 {
		threshold := now.Add(-time.Duration(opts.OlderThanDays) * 24 * time.Hour)
		for _, t := range tasks {
			if isTaskActive(t) {
				continue
			}
			ts := taskTimestamp(t)
			if !ts.IsZero() && ts.Before(threshold) {
				result.OlderTasks = append(result.OlderTasks, t)
				filePath := filepath.Join(t.Directory, t.Filename)
				if fi, statErr := os.Stat(filePath); statErr == nil && !fi.IsDir() {
					result.OlderFilesBytes += fi.Size()
				}
			}
		}
	}

	// 3. Duplicate files scan (completed tasks with existing file > 0 bytes)
	if opts.CheckDuplicates {
		sizeMap := make(map[int64][]*task.Task)
		for _, t := range tasks {
			if isTaskActive(t) || t.Status != task.StatusCompleted {
				continue
			}
			filePath := filepath.Join(t.Directory, t.Filename)
			fi, statErr := os.Stat(filePath)
			if statErr != nil || fi.IsDir() || fi.Size() <= 0 {
				continue
			}
			sizeMap[fi.Size()] = append(sizeMap[fi.Size()], t)
		}

		md5Cache := make(map[string]string)

		for size, candidates := range sizeMap {
			if len(candidates) < 2 {
				continue
			}

			hashGroups := make(map[string][]*task.Task)
			for _, t := range candidates {
				if err := ctx.Err(); err != nil {
					return nil, err
				}

				filePath := filepath.Join(t.Directory, t.Filename)
				pKey := pathKey(filePath)
				hash, ok := md5Cache[pKey]
				if !ok {
					var hErr error
					hash, hErr = hashFileMD5(filePath)
					if hErr != nil {
						continue
					}
					md5Cache[pKey] = hash
				}
				hashGroups[hash] = append(hashGroups[hash], t)
			}

			for hash, groupTasks := range hashGroups {
				if len(groupTasks) < 2 {
					continue
				}

				orig, dups := pickOriginalAndDuplicates(groupTasks)
				if orig == nil || len(dups) == 0 {
					continue
				}

				group := CleanupDuplicateGroup{
					MD5:            hash,
					FileSize:       size,
					OriginalTask:   orig,
					DuplicateTasks: dups,
				}
				result.DuplicateGroups = append(result.DuplicateGroups, group)

				seenPaths := make(map[string]bool)
				origPath := filepath.Join(orig.Directory, orig.Filename)
				seenPaths[pathKey(origPath)] = true

				for _, dup := range dups {
					p := filepath.Join(dup.Directory, dup.Filename)
					k := pathKey(p)
					if !seenPaths[k] {
						seenPaths[k] = true
						result.DuplicateFilesBytes += size
					}
				}
			}
		}
	}

	// 4. Calculate authoritative deduplicated totals across all selected options
	allCleanableTasks := make(map[string]*task.Task)
	distinctFilesFreed := make(map[string]int64)

	// Duplicates: remove tasks and duplicate files
	if opts.CheckDuplicates {
		for _, g := range result.DuplicateGroups {
			if g.OriginalTask == nil {
				continue
			}
			origP := filepath.Join(g.OriginalTask.Directory, g.OriginalTask.Filename)
			origKey := pathKey(origP)

			for _, dup := range g.DuplicateTasks {
				allCleanableTasks[dup.ID] = dup
				dupP := filepath.Join(dup.Directory, dup.Filename)
				dupKey := pathKey(dupP)
				if dupKey != origKey && !SamePath(dupP, origP) {
					distinctFilesFreed[dupKey] = g.FileSize
				}
			}
		}
	}

	// Missing tasks: remove tasks, no files
	if opts.CheckMissingFiles {
		for _, t := range result.MissingTasks {
			allCleanableTasks[t.ID] = t
		}
	}

	// Older tasks: remove tasks, optionally delete files
	if opts.OlderThanDays > 0 {
		for _, t := range result.OlderTasks {
			allCleanableTasks[t.ID] = t
			if opts.DeleteOlderDiskFiles {
				p := filepath.Join(t.Directory, t.Filename)
				k := pathKey(p)
				if fi, statErr := os.Stat(p); statErr == nil && !fi.IsDir() {
					distinctFilesFreed[k] = fi.Size()
				}
			}
		}
	}

	result.TotalCleanableTasks = len(allCleanableTasks)
	result.TotalCleanableFiles = len(distinctFilesFreed)
	for _, bytes := range distinctFilesFreed {
		result.TotalFreedBytes += bytes
	}

	return result, nil
}

// pickOriginalAndDuplicates identifies the primary (original) file and marks copies as duplicates.
func pickOriginalAndDuplicates(tasks []*task.Task) (*task.Task, []*task.Task) {
	if len(tasks) == 0 {
		return nil, nil
	}
	if len(tasks) == 1 {
		return tasks[0], nil
	}

	sorted := make([]*task.Task, len(tasks))
	copy(sorted, tasks)
	sort.Slice(sorted, func(i, j int) bool {
		tI, tJ := taskTimestamp(sorted[i]), taskTimestamp(sorted[j])
		if !tI.Equal(tJ) {
			return tI.Before(tJ)
		}
		return sorted[i].ID < sorted[j].ID
	})

	var bestOrigIndex = -1

	// Strategy A: Find a task that is a clean base file (not a numbered copy itself)
	// which other tasks in the group derive from.
	for i, candidate := range sorted {
		stem, ext := ExtractStemAndExt(candidate.Filename)
		if _, isCopy := IsNumberedCopyOf(candidate.Filename, stem, ext); isCopy {
			continue
		}
		hasCopies := false
		for j, other := range sorted {
			if i == j {
				continue
			}
			if _, ok := IsNumberedCopyOf(other.Filename, stem, ext); ok {
				hasCopies = true
				break
			}
		}
		if hasCopies {
			bestOrigIndex = i
			break
		}
	}

	// Strategy B: If no exact base file exists in group (e.g. only "file (1).mp4" and "file (2).mp4"),
	// pick the lowest numbered copy or earliest.
	if bestOrigIndex == -1 {
		for i, candidate := range sorted {
			candExt := filepath.Ext(candidate.Filename)
			candStem := strings.TrimSuffix(candidate.Filename, candExt)
			m := numberedCopyStrictRegex.FindStringSubmatch(candStem)
			if len(m) == 3 {
				baseStem := m[1]
				hasOtherCopies := false
				for j, other := range sorted {
					if i == j {
						continue
					}
					if _, ok := IsNumberedCopyOf(other.Filename, baseStem, candExt); ok {
						hasOtherCopies = true
						break
					}
				}
				if hasOtherCopies {
					bestOrigIndex = i
					break
				}
			}
		}
	}

	// Strategy C: Fallback to earliest created task (index 0 in sorted)
	if bestOrigIndex == -1 {
		bestOrigIndex = 0
	}

	orig := sorted[bestOrigIndex]
	dups := make([]*task.Task, 0, len(sorted)-1)
	for i, t := range sorted {
		if i != bestOrigIndex {
			dups = append(dups, t)
		}
	}

	return orig, dups
}

// ExecuteCleanup executes deletion of selected cleanup categories.
func (m *Manager) ExecuteCleanup(ctx context.Context, opts CleanupExecuteOptions) (*CleanupExecuteResult, error) {
	scanRes, err := m.ScanCleanup(ctx, CleanupScanOptions{
		OlderThanDays:        opts.OlderThanDays,
		DeleteOlderDiskFiles: opts.DeleteOlderDiskFiles,
		CheckDuplicates:      opts.DeleteDuplicates,
		CheckMissingFiles:    opts.DeleteMissingTasks,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to scan cleanable items: %w", err)
	}

	result := &CleanupExecuteResult{
		Errors: make([]string, 0),
	}
	deletedTaskIDs := make(map[string]bool)
	deletedFileKeys := make(map[string]bool)

	// A. Delete Duplicates
	if opts.DeleteDuplicates {
		for _, group := range scanRes.DuplicateGroups {
			if group.OriginalTask == nil {
				continue
			}
			origPath := filepath.Join(group.OriginalTask.Directory, group.OriginalTask.Filename)
			origKey := pathKey(origPath)

			for _, dupTask := range group.DuplicateTasks {
				if isTaskActive(dupTask) || deletedTaskIDs[dupTask.ID] {
					continue
				}

				dupPath := filepath.Join(dupTask.Directory, dupTask.Filename)
				dupKey := pathKey(dupPath)

				// Only remove disk file if it is NOT the original task's file
				if dupKey != origKey && !SamePath(dupPath, origPath) && !deletedFileKeys[dupKey] {
					if fi, statErr := os.Stat(dupPath); statErr == nil && !fi.IsDir() {
						result.FreedBytes += fi.Size()
						result.DeletedFileCount++
					}
					m.RemoveTaskFiles(dupTask)
					deletedFileKeys[dupKey] = true
				}

				if delErr := m.Delete(ctx, dupTask.ID); delErr == nil {
					deletedTaskIDs[dupTask.ID] = true
					result.DeletedTaskCount++
				} else {
					result.Errors = append(result.Errors, fmt.Sprintf("failed to delete task %s: %v", dupTask.ID, delErr))
				}
			}
		}
	}

	// B. Delete Missing Tasks (record only, file already missing)
	if opts.DeleteMissingTasks {
		for _, missingTask := range scanRes.MissingTasks {
			if isTaskActive(missingTask) || deletedTaskIDs[missingTask.ID] {
				continue
			}
			if delErr := m.Delete(ctx, missingTask.ID); delErr == nil {
				deletedTaskIDs[missingTask.ID] = true
				result.DeletedTaskCount++
			} else {
				result.Errors = append(result.Errors, fmt.Sprintf("failed to delete task %s: %v", missingTask.ID, delErr))
			}
		}
	}

	// C. Delete Older Tasks
	if opts.DeleteOlderTasks && opts.OlderThanDays > 0 {
		for _, oldTask := range scanRes.OlderTasks {
			if isTaskActive(oldTask) || deletedTaskIDs[oldTask.ID] {
				continue
			}

			oldPath := filepath.Join(oldTask.Directory, oldTask.Filename)
			oldKey := pathKey(oldPath)

			if opts.DeleteOlderDiskFiles && !deletedFileKeys[oldKey] {
				if fi, statErr := os.Stat(oldPath); statErr == nil && !fi.IsDir() {
					result.FreedBytes += fi.Size()
					result.DeletedFileCount++
				}
				m.RemoveTaskFiles(oldTask)
				deletedFileKeys[oldKey] = true
			}

			if delErr := m.Delete(ctx, oldTask.ID); delErr == nil {
				deletedTaskIDs[oldTask.ID] = true
				result.DeletedTaskCount++
			} else {
				result.Errors = append(result.Errors, fmt.Sprintf("failed to delete task %s: %v", oldTask.ID, delErr))
			}
		}
	}

	return result, nil
}
