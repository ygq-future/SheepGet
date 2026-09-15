package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"sheep-get/internal/task"
)

var (
	ErrTaskAlreadyRunning = errors.New("task already running")
	ErrCannotResume       = errors.New("cannot resume task in current status")
)

// TaskListener allows observing task state transitions and progress updates.
type TaskListener interface {
	OnTaskUpdated(t *task.Task)
}

// Config holds manager settings.
type Config struct {
	MaxActiveTasks int `json:"maxActiveTasks"`
}

// ProbeResult holds the probed metadata and duplicate-task state for a URL.
type ProbeResult struct {
	URL           string     `json:"url"`
	Filename      string     `json:"filename"`
	TotalBytes    int64      `json:"totalBytes"`
	Resumable     bool       `json:"resumable"`
	ETag          string     `json:"etag"`
	LastModified  string     `json:"lastModified"`
	ContentType   string     `json:"contentType"`
	DuplicateTask *task.Task `json:"duplicateTask,omitempty"`
}

// ConsistencyResult reports whether an updated URL is consistent with original task file.
type ConsistencyResult struct {
	Consistent bool   `json:"consistent"`
	Reason     string `json:"reason,omitempty"`
	TotalBytes int64  `json:"totalBytes"`
	ETag       string `json:"etag"`
	Resumable  bool   `json:"resumable"`
}

// CheckFileConflict reports whether filename already exists in dir, and if so suggests a
// numbered alternative name that is free on disk.
func CheckFileConflict(dir, filename string) (bool, string) {
	if dir == "" || filename == "" {
		return false, filename
	}
	if !fileExists(filepath.Join(dir, filename)) {
		return false, filename
	}
	return true, numberedName(filename, func(candidate string) bool {
		return fileExists(filepath.Join(dir, candidate))
	})
}

var numberedSuffixRegex = regexp.MustCompile(`^(.*) \(\d+\)$`)

// numberedName returns the first "name (n).ext" variant not rejected by taken.
func numberedName(filename string, taken func(string) bool) string {
	ext := filepath.Ext(filename)
	stem := strings.TrimSuffix(filename, ext)
	if m := numberedSuffixRegex.FindStringSubmatch(stem); len(m) == 2 {
		stem = m[1]
	}
	if !taken(filename) {
		return filename
	}
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if !taken(candidate) {
			return candidate
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}

// Manager orchestrates task queues, concurrency, lifecycle, and progress reporting.
type Manager struct {
	mu           sync.Mutex
	store        task.TaskStore
	downloader   *HTTPDownloader
	config       Config
	activeTasks  map[string]context.CancelFunc
	taskDone     map[string]chan struct{}
	taskSpeed    map[string]int64
	speedSamples map[string]int64
	listeners    []TaskListener
	wg           sync.WaitGroup
	closed       bool
}

// NewManager creates a download manager.
func NewManager(store task.TaskStore, downloader *HTTPDownloader, cfg Config) *Manager {
	if cfg.MaxActiveTasks <= 0 {
		cfg.MaxActiveTasks = 3 // default 3 active tasks as per spec A10
	}
	if downloader == nil {
		downloader = NewHTTPDownloader(nil)
	}

	m := &Manager{
		store:        store,
		downloader:   downloader,
		config:       cfg,
		activeTasks:  make(map[string]context.CancelFunc),
		taskDone:     make(map[string]chan struct{}),
		taskSpeed:    make(map[string]int64),
		speedSamples: make(map[string]int64),
	}

	go m.speedTicker()
	return m
}

func (m *Manager) AddListener(l TaskListener) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, l)
}

func (m *Manager) notify(t *task.Task) {
	for _, l := range m.listeners {
		l.OnTaskUpdated(t)
	}
}

// waitTaskIdle blocks until the transfer goroutine for taskID has fully stopped, so its
// final state write and its open file handles are settled before the caller moves or renames files.
func (m *Manager) waitTaskIdle(taskID string, timeout time.Duration) {
	m.mu.Lock()
	done, running := m.taskDone[taskID]
	m.mu.Unlock()
	if !running {
		return
	}

	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// ProbeURL inspects the URL metadata and reports whether an existing task already uses the URL.
func (m *Manager) ProbeURL(ctx context.Context, urlStr string) (*ProbeResult, error) {
	existingList, err := m.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read task list: %w", err)
	}

	var dupTask *task.Task
	for _, ext := range existingList {
		if ext.URL == urlStr {
			dupTask = ext
			break
		}
	}

	info, probeErr := m.downloader.Probe(ctx, urlStr, nil)

	result := &ProbeResult{
		URL:           urlStr,
		DuplicateTask: dupTask,
		TotalBytes:    -1,
	}

	if info != nil {
		result.Filename = info.Filename
		result.TotalBytes = info.TotalBytes
		result.Resumable = info.Resumable
		result.ETag = info.ETag
		result.LastModified = info.LastModified
		result.ContentType = info.ContentType
	} else {
		result.Filename = extractFilenameFromURL(urlStr)
	}

	return result, probeErr
}

// ResolveDuplicate resolves a duplicate task with strategy "continue", "redownload", "copy", or "show_completed".
func (m *Manager) ResolveDuplicate(ctx context.Context, taskID, strategy, dir, filename string, maxConn int) (*task.Task, error) {
	t, err := m.store.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	switch strategy {
	case "continue":
		if t.Status == task.StatusPaused || t.Status == task.StatusError {
			if err := m.Resume(ctx, taskID); err != nil {
				return nil, err
			}
		}
		return t, nil

	case "redownload":
		_ = m.Pause(ctx, taskID)
		m.waitTaskIdle(taskID, 5*time.Second)
		destPath := filepath.Join(t.Directory, t.Filename)
		_ = os.Remove(destPath)
		_ = os.Remove(destPath + ".sheepget")

		if dir != "" {
			t.Directory = dir
		}
		if filename != "" {
			t.Filename = filename
		}
		if maxConn > 0 {
			t.MaxConcurrency = maxConn
		}
		t.Downloaded = 0
		t.Chunks = nil
		t.Status = task.StatusQueued
		t.ErrorMsg = ""
		t.UpdatedAt = time.Now()
		if err := m.store.Save(ctx, t); err != nil {
			return nil, err
		}
		m.notify(t)
		m.schedule()
		return t, nil

	case "copy":
		if filename == "" {
			filename = t.Filename
		}
		if dir == "" {
			dir = t.Directory
		}
		filename, err = m.NumberedCopyName(ctx, dir, filename)
		if err != nil {
			return nil, err
		}
		if maxConn <= 0 {
			maxConn = t.MaxConcurrency
		}
		newTask := &task.Task{
			ID:             fmt.Sprintf("task_%d", time.Now().UnixNano()),
			URL:            t.URL,
			Filename:       filename,
			Directory:      dir,
			TotalBytes:     t.TotalBytes,
			Downloaded:     0,
			Status:         task.StatusQueued,
			MaxConcurrency: maxConn,
			Resumable:      t.Resumable,
			ETag:           t.ETag,
			LastModified:   t.LastModified,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := m.store.Save(ctx, newTask); err != nil {
			return nil, err
		}
		m.notify(newTask)
		m.schedule()
		return newTask, nil

	case "show_completed":
		return t, nil

	default:
		return nil, fmt.Errorf("unknown duplicate strategy: %s", strategy)
	}
}

// NumberedCopyName returns the first "name (n).ext" variant free on disk and in the task list.
func (m *Manager) NumberedCopyName(ctx context.Context, dir, filename string) (string, error) {
	existingList, err := m.store.List(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to read task list: %w", err)
	}
	return numberedName(filename, func(candidate string) bool {
		if fileExists(filepath.Join(dir, candidate)) {
			return true
		}
		for _, et := range existingList {
			if et.Directory == dir && et.Filename == candidate {
				return true
			}
		}
		return false
	}), nil
}

// StartPreDownload creates a task that begins transferring while the file info dialog is still open.
func (m *Manager) StartPreDownload(ctx context.Context, urlStr, dir, filename string, maxConn int) (*task.Task, error) {
	return m.StartPreDownloadWithHeaders(ctx, urlStr, dir, filename, maxConn, nil)
}

// StartPreDownloadWithHeaders creates a pre-download task with optional request headers.
func (m *Manager) StartPreDownloadWithHeaders(ctx context.Context, urlStr, dir, filename string, maxConn int, headers map[string]string) (*task.Task, error) {
	info, err := m.downloader.Probe(ctx, urlStr, headers)
	if err != nil {
		// As per A03: a probe failure on a confirmed manual download still keeps a visible task.
		t := &task.Task{
			ID:             fmt.Sprintf("task_%d", time.Now().UnixNano()),
			URL:            urlStr,
			Filename:       filename,
			Directory:      dir,
			TotalBytes:     -1,
			Status:         task.StatusError,
			ErrorMsg:       err.Error(),
			MaxConcurrency: maxConn,
			RequestHeaders: headers,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if t.Filename == "" {
			t.Filename = extractFilenameFromURL(urlStr)
		}
		if saveErr := m.store.Save(ctx, t); saveErr != nil {
			return nil, saveErr
		}
		m.notify(t)
		return t, nil
	}

	if filename == "" {
		filename = info.Filename
	}
	if maxConn <= 0 {
		maxConn = DefaultMaxConcurrency
	}

	t := &task.Task{
		ID:             fmt.Sprintf("task_%d", time.Now().UnixNano()),
		URL:            urlStr,
		Filename:       filename,
		Directory:      dir,
		TotalBytes:     info.TotalBytes,
		Downloaded:     0,
		Status:         task.StatusQueued,
		MaxConcurrency: maxConn,
		Resumable:      info.Resumable,
		ETag:           info.ETag,
		LastModified:   info.LastModified,
		RequestHeaders: headers,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := m.store.Save(ctx, t); err != nil {
		return nil, err
	}
	m.notify(t)
	m.schedule()
	return t, nil
}

// ConfirmPreDownload finalizes the save directory, filename and connection count of a task whose
// file info dialog was still open, moving any in-progress data to the confirmed destination.
func (m *Manager) ConfirmPreDownload(ctx context.Context, taskID, finalDir, finalFilename string, maxConn int) (*task.Task, error) {
	t, err := m.store.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	if finalDir == "" {
		finalDir = t.Directory
	}
	if finalFilename == "" {
		finalFilename = t.Filename
	}

	// If name or directory changed, move files accordingly
	if finalDir != t.Directory || finalFilename != t.Filename {
		oldDest := filepath.Join(t.Directory, t.Filename)
		newDest := filepath.Join(finalDir, finalFilename)
		if err := os.MkdirAll(finalDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create target directory: %w", err)
		}

		if t.Status == task.StatusCompleted {
			if _, err := os.Stat(oldDest); err == nil {
				if err := os.Rename(oldDest, newDest); err != nil {
					return nil, fmt.Errorf("failed to move completed file: %w", err)
				}
			}
		} else {
			// Stop the transfer and wait for its file handles to close before moving partial data.
			m.mu.Lock()
			cancel, wasActive := m.activeTasks[taskID]
			if wasActive {
				delete(m.activeTasks, taskID)
			}
			m.mu.Unlock()
			if wasActive {
				cancel()
			}
			m.waitTaskIdle(taskID, 5*time.Second)

			// The transfer goroutine persists its own pause state, so re-read before editing.
			if latest, getErr := m.store.Get(ctx, taskID); getErr == nil {
				t = latest
			}

			oldPart := oldDest + ".sheepget"
			newPart := newDest + ".sheepget"
			if _, err := os.Stat(oldPart); err == nil {
				if renameErr := os.Rename(oldPart, newPart); renameErr != nil {
					// Partial data cannot be relocated: drop it and restart cleanly so the
					// confirmed destination never receives bytes from the wrong file.
					_ = os.Remove(oldPart)
					t.Chunks = nil
					t.Downloaded = 0
				}
			}
			t.Status = task.StatusQueued
			t.ErrorMsg = ""
			t.Speed = 0
		}
		t.Directory = finalDir
		t.Filename = finalFilename
	}

	if maxConn > 0 {
		t.MaxConcurrency = maxConn
	}
	t.UpdatedAt = time.Now()
	if err := m.store.Save(ctx, t); err != nil {
		return nil, err
	}
	m.notify(t)
	m.schedule()
	return t, nil
}

// CancelPreDownload applies the cancel semantics of the file info dialog: a running transfer stops
// and stays as a paused task, a task that already finished keeps its completed state and file.
func (m *Manager) CancelPreDownload(ctx context.Context, taskID string) error {
	t, err := m.store.Get(ctx, taskID)
	if err != nil {
		return nil
	}

	if t.Status == task.StatusCompleted || t.Status == task.StatusError || t.Status == task.StatusPaused {
		return nil
	}

	if err := m.Pause(ctx, taskID); err != nil {
		return err
	}
	m.waitTaskIdle(taskID, 5*time.Second)
	return nil
}

// CheckURLConsistency decides whether an updated URL still serves the same resource, so that
// already-downloaded bytes can be reused. headers replaces the task request context when non-nil.
// Per A13 the answer is only "consistent" when identity is positively verified: size and range
// support alone cannot detect a replaced resource, so a validator (ETag, else Last-Modified) must match.
func (m *Manager) CheckURLConsistency(ctx context.Context, taskID, newURL string, headers map[string]string) (*ConsistencyResult, error) {
	t, err := m.store.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	probeHeaders := t.RequestHeaders
	if headers != nil {
		probeHeaders = headers
	}

	info, err := m.downloader.Probe(ctx, newURL, probeHeaders)
	if err != nil {
		return &ConsistencyResult{
			Consistent: false,
			Reason:     "无法连接到新链接: " + err.Error(),
		}, nil
	}

	result := &ConsistencyResult{
		TotalBytes: info.TotalBytes,
		ETag:       info.ETag,
		Resumable:  info.Resumable,
	}

	if t.TotalBytes > 0 && info.TotalBytes > 0 && t.TotalBytes != info.TotalBytes {
		result.Reason = fmt.Sprintf("文件大小不一致（原文件: %d 字节, 新链接: %d 字节）", t.TotalBytes, info.TotalBytes)
		return result, nil
	}

	if t.Downloaded > 0 && !info.Resumable {
		result.Reason = "新链接不支持断点续传 (Range 请求)"
		return result, nil
	}

	if t.Downloaded == 0 {
		// No bytes have been written yet, so there is nothing to mix: any URL is safe to adopt.
		result.Consistent = true
		return result, nil
	}

	switch {
	case t.ETag != "" && info.ETag != "":
		if t.ETag != info.ETag {
			result.Reason = fmt.Sprintf("文件版本 (ETag) 不一致（原文件: %s, 新链接: %s）", t.ETag, info.ETag)
			return result, nil
		}
	case t.LastModified != "" && info.LastModified != "":
		if t.LastModified != info.LastModified {
			result.Reason = fmt.Sprintf("文件修改时间不一致（原文件: %s, 新链接: %s）", t.LastModified, info.LastModified)
			return result, nil
		}
	default:
		// No shared validator: equal size may still be a different resource, so refuse to resume.
		result.Reason = "无法确认是同一文件（缺少 ETag 或 Last-Modified 校验信息），建议重新下载"
		return result, nil
	}

	result.Consistent = true
	return result, nil
}

// UpdateTaskURL adopts a refreshed URL and request context, then resumes the transfer so the
// confirmed-same file continues from its existing progress.
func (m *Manager) UpdateTaskURL(ctx context.Context, taskID, newURL string, headers map[string]string) (*task.Task, error) {
	wasActive := false
	m.mu.Lock()
	if _, running := m.activeTasks[taskID]; running {
		wasActive = true
	}
	m.mu.Unlock()
	if wasActive {
		return nil, ErrTaskAlreadyRunning
	}

	t, err := m.store.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	t.URL = newURL
	if headers != nil {
		t.RequestHeaders = headers
	}
	// Adopt the refreshed resource's metadata so size, resumability and validators stop describing
	// the dead link. A failed probe is not fatal here: the caller already verified the link, and the
	// transfer itself reports a real error if it cannot proceed.
	if info, probeErr := m.downloader.Probe(ctx, newURL, t.RequestHeaders); probeErr == nil {
		t.TotalBytes = info.TotalBytes
		t.Resumable = info.Resumable
		t.ETag = info.ETag
		t.LastModified = info.LastModified
	}
	t.ErrorMsg = ""
	if t.Status != task.StatusCompleted {
		t.Status = task.StatusQueued
	}
	t.UpdatedAt = time.Now()
	if err := m.store.Save(ctx, t); err != nil {
		return nil, err
	}
	m.notify(t)
	m.schedule()
	return t, nil
}

// ResetAndDownloadWithNewURL resets progress and downloads from scratch with new URL.
func (m *Manager) ResetAndDownloadWithNewURL(ctx context.Context, taskID, newURL string, headers map[string]string) (*task.Task, error) {
	_ = m.Pause(ctx, taskID)
	m.waitTaskIdle(taskID, 5*time.Second)
	t, err := m.store.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	destPath := filepath.Join(t.Directory, t.Filename)
	_ = os.Remove(destPath)
	_ = os.Remove(destPath + ".sheepget")

	if headers != nil {
		t.RequestHeaders = headers
	}
	info, _ := m.downloader.Probe(ctx, newURL, t.RequestHeaders)
	t.URL = newURL
	t.Downloaded = 0
	t.Chunks = nil
	t.Status = task.StatusQueued
	t.ErrorMsg = ""
	if info != nil {
		t.TotalBytes = info.TotalBytes
		t.Resumable = info.Resumable
		t.ETag = info.ETag
		t.LastModified = info.LastModified
	}
	t.UpdatedAt = time.Now()
	if err := m.store.Save(ctx, t); err != nil {
		return nil, err
	}
	m.notify(t)
	m.schedule()
	return t, nil
}

// AddTask probes the URL, checks duplicates, creates a task in queued or paused state, and triggers queue scheduling.
func (m *Manager) AddTask(ctx context.Context, urlStr, dir, filename string, maxConn int) (*task.Task, error) {
	return m.AddTaskWithHeaders(ctx, urlStr, dir, filename, maxConn, nil)
}

// AddTaskWithHeaders creates a task carrying optional request headers context.
func (m *Manager) AddTaskWithHeaders(ctx context.Context, urlStr, dir, filename string, maxConn int, headers map[string]string) (*task.Task, error) {
	// Check duplicate URL in existing tasks
	existingList, _ := m.store.List(ctx)
	for _, ext := range existingList {
		if ext.URL == urlStr && ext.Status != task.StatusCompleted {
			return nil, fmt.Errorf("该下载链接已存在于任务列表中（状态：%s），请勿重复添加", ext.Status)
		}
	}
	info, err := m.downloader.Probe(ctx, urlStr, headers)
	if err != nil {
		// As per A03: even if probe/network fails immediately upon manual confirmation, keep task with error
		t := &task.Task{
			ID:             fmt.Sprintf("task_%d", time.Now().UnixNano()),
			URL:            urlStr,
			Filename:       filename,
			Directory:      dir,
			TotalBytes:     -1,
			Status:         task.StatusError,
			ErrorMsg:       err.Error(),
			MaxConcurrency: maxConn,
			RequestHeaders: headers,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if t.Filename == "" {
			t.Filename = extractFilenameFromURL(urlStr)
		}
		_ = m.store.Save(ctx, t)
		m.notify(t)
		return t, nil
	}

	if filename == "" {
		filename = info.Filename
	}
	if maxConn <= 0 {
		maxConn = DefaultMaxConcurrency
	}

	t := &task.Task{
		ID:             fmt.Sprintf("task_%d", time.Now().UnixNano()),
		URL:            urlStr,
		Filename:       filename,
		Directory:      dir,
		TotalBytes:     info.TotalBytes,
		Downloaded:     0,
		Status:         task.StatusQueued,
		MaxConcurrency: maxConn,
		Resumable:      info.Resumable,
		ETag:           info.ETag,
		LastModified:   info.LastModified,
		RequestHeaders: headers,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := m.store.Save(ctx, t); err != nil {
		return nil, err
	}

	m.notify(t)
	m.schedule()
	return t, nil
}

// Pause stops an active or queued task, releasing slot and scheduling next.
func (m *Manager) Pause(ctx context.Context, id string) error {
	m.mu.Lock()
	cancel, active := m.activeTasks[id]
	if active {
		delete(m.activeTasks, id)
		cancel()
	}
	m.mu.Unlock()

	t, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}

	if t.Status == task.StatusCompleted {
		return nil
	}

	t.Status = task.StatusPaused
	t.Speed = 0
	t.UpdatedAt = time.Now()
	_ = m.store.Save(ctx, t)
	m.notify(t)

	m.schedule()
	return nil
}

// Resume enqueues or starts a paused or errored task.
func (m *Manager) Resume(ctx context.Context, id string) error {
	t, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}

	if t.Status == task.StatusCompleted || t.Status == task.StatusDownloading {
		return nil
	}

	t.Status = task.StatusQueued
	t.ErrorMsg = ""
	t.UpdatedAt = time.Now()
	if err := m.store.Save(ctx, t); err != nil {
		return err
	}

	m.notify(t)
	m.schedule()
	return nil
}

// Retry retries a failed or paused task from beginning or checkpoint.
func (m *Manager) Retry(ctx context.Context, id string) error {
	return m.Resume(ctx, id)
}

// Delete removes task from store and stops running transfer.
func (m *Manager) Delete(ctx context.Context, id string) error {
	_ = m.Pause(ctx, id)
	err := m.store.Delete(ctx, id)
	return err
}

// List returns all tasks.
func (m *Manager) List(ctx context.Context) ([]*task.Task, error) {
	return m.store.List(ctx)
}

// SetMaxActiveTasks updates concurrency limit and triggers scheduling.
func (m *Manager) SetMaxActiveTasks(n int) {
	m.mu.Lock()
	if n > 0 {
		m.config.MaxActiveTasks = n
	}
	m.mu.Unlock()
	m.schedule()
}

func (m *Manager) schedule() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return
	}

	ctx := context.Background()
	tasks, err := m.store.List(ctx)
	if err != nil {
		return
	}

	activeCount := len(m.activeTasks)
	availableSlots := m.config.MaxActiveTasks - activeCount
	if availableSlots <= 0 {
		return
	}

	// Schedule in order of creation (queue discipline)
	for _, t := range tasks {
		if availableSlots <= 0 {
			break
		}
		if t.Status == task.StatusQueued {
			if _, running := m.activeTasks[t.ID]; !running {
				taskCtx, cancel := context.WithCancel(context.Background())
				m.activeTasks[t.ID] = cancel
				availableSlots--

				m.wg.Add(1)
				done := make(chan struct{})
				m.taskDone[t.ID] = done
				go func(id string, finished chan struct{}) {
					defer m.wg.Done()
					defer func() {
						m.mu.Lock()
						close(finished)
						delete(m.taskDone, id)
						m.mu.Unlock()
					}()
					m.runTask(taskCtx, id)
				}(t.ID, done)
			}
		}
	}
}

func (m *Manager) runTask(ctx context.Context, taskID string) {
	bgCtx := context.Background()
	t, err := m.store.Get(bgCtx, taskID)
	if err != nil {
		m.finishTask(taskID)
		return
	}

	t.Status = task.StatusDownloading
	t.UpdatedAt = time.Now()
	_ = m.store.Save(bgCtx, t)
	m.notify(t)

	var lastSavedDownloaded int64
	var lastSaveTime time.Time

	progressCb := func(downloaded int64, chunkIndex int, chunkDownloaded int64) {
		m.mu.Lock()
		delta := downloaded - t.Downloaded
		if delta > 0 {
			m.speedSamples[taskID] += delta
		}
		t.Downloaded = downloaded
		t.Speed = m.taskSpeed[taskID]
		m.mu.Unlock()

		// Throttle persistence to disk (at most once every 500ms or 1MB)
		now := time.Now()
		if now.Sub(lastSaveTime) > 500*time.Millisecond || (downloaded-lastSavedDownloaded) > 1024*1024 {
			lastSaveTime = now
			lastSavedDownloaded = downloaded
			_ = m.store.Save(bgCtx, t)
		}
		m.notify(t)
	}

	err = m.downloader.Download(ctx, t, progressCb)

	m.finishTask(taskID)

	t.Speed = 0
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			t.Status = task.StatusPaused
		} else if latest, getErr := m.store.Get(bgCtx, taskID); getErr == nil && latest.Status == task.StatusPaused {
			// A concurrent Pause already persisted the paused state; keep it.
			t.Status = task.StatusPaused
		} else {
			t.Status = task.StatusError
			t.ErrorMsg = err.Error()
		}
	} else {
		t.Status = task.StatusCompleted
		if t.TotalBytes > 0 {
			// For a known size the transfer is complete by definition; when the size was never
			// known, keep the bytes actually written instead of reporting the unknown marker.
			t.Downloaded = t.TotalBytes
		}
		t.ErrorMsg = ""
	}
	t.UpdatedAt = time.Now()
	_ = m.store.Save(bgCtx, t)
	m.notify(t)

	// Free slot and promote next queued task
	m.schedule()
}

func (m *Manager) finishTask(taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.activeTasks, taskID)
	delete(m.taskSpeed, taskID)
	delete(m.speedSamples, taskID)
}

func (m *Manager) speedTicker() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return
		}
		for id, delta := range m.speedSamples {
			m.taskSpeed[id] = delta
			m.speedSamples[id] = 0
		}
		m.mu.Unlock()
	}
}

// Close stops the manager and cancels all running tasks.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	for _, cancel := range m.activeTasks {
		cancel()
	}
	m.activeTasks = make(map[string]context.CancelFunc)
	m.mu.Unlock()
	m.wg.Wait()
}
