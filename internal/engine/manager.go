package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sheep-get/internal/config"
	"sheep-get/internal/credentials"
	"sheep-get/internal/hls"
	"sheep-get/internal/logging"
	"sheep-get/internal/mediainfo"
	"sheep-get/internal/task"
)

var (
	ErrTaskAlreadyRunning = errors.New("task already running")
	ErrCannotResume       = errors.New("cannot resume task in current status")
	// ErrProcessingRetryRequired 表示传输已完成、只有处理失败；重新传输既不必要也会丢弃已下载分片。
	ErrProcessingRetryRequired = errors.New("task failed during media processing; retry processing instead")
	// ErrNotProcessingFailure 表示任务并非处理失败，不能按仅重试处理处理。
	ErrNotProcessingFailure = errors.New("task did not fail during media processing")
)

// TaskListener allows observing task state transitions and progress updates.
type TaskListener interface {
	OnTaskUpdated(t *task.Task)
}

// TaskDeleteListener allows observing task deletions.
type TaskDeleteListener interface {
	OnTaskDeleted(taskID string)
}

// Config holds manager settings.
type Config struct {
	MaxActiveTasks            int    `json:"maxActiveTasks"`
	TempDirectory             string `json:"tempDirectory"`
	UseServerFileTime         bool   `json:"useServerFileTime"`
	DefaultConnectionsPerTask int    `json:"defaultConnectionsPerTask"`
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
	// HLS 是这条链接的 HLS 事实；为空表示它不是清单，按普通文件下载。
	HLS *HLSProbe `json:"hls,omitempty"`
}

// ConsistencyResult reports whether an updated URL is consistent with original task file.
type ConsistencyResult struct {
	Consistent bool   `json:"consistent"`
	Reason     string `json:"reason,omitempty"`
	TotalBytes int64  `json:"totalBytes"`
	ETag       string `json:"etag"`
	Resumable  bool   `json:"resumable"`
}

// SamePath checks whether two directory paths point to the same location,
// respecting platform case sensitivity (Windows case-insensitive).
func SamePath(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	ca := filepath.Clean(a)
	cb := filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

// SameFilename checks whether two filenames match, respecting platform case sensitivity.
func SameFilename(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

var numberedCopyStrictRegex = regexp.MustCompile(`^(.+) \((\d+)\)$`)

// ExtractStemAndExt returns the base stem (without any (n) suffix) and extension.
func ExtractStemAndExt(filename string) (string, string) {
	ext := filepath.Ext(filename)
	stem := strings.TrimSuffix(filename, ext)
	if m := numberedSuffixRegex.FindStringSubmatch(stem); len(m) == 2 {
		stem = m[1]
	}
	return stem, ext
}

// IsNumberedCopyOf checks if candidate is a numbered copy of baseStem with matching ext (e.g. "name (1).ext").
// It returns the copy index and true, or 0 and false if it is the base file or unrelated.
func IsNumberedCopyOf(candidate, baseStem, ext string) (int, bool) {
	candExt := filepath.Ext(candidate)
	if !strings.EqualFold(candExt, ext) {
		return 0, false
	}
	candStem := strings.TrimSuffix(candidate, candExt)
	m := numberedCopyStrictRegex.FindStringSubmatch(candStem)
	if len(m) != 3 {
		return 0, false
	}
	if !strings.EqualFold(m[1], baseStem) {
		return 0, false
	}
	num, err := strconv.Atoi(m[2])
	if err != nil || num <= 0 {
		return 0, false
	}
	return num, true
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
	logger       atomic.Pointer[slog.Logger]
}

// SetLogger 设定任务日志出口，并同步给底层的传输器——两者的日志出口永远是同一个。
func (m *Manager) SetLogger(logger *slog.Logger) {
	if logger == nil {
		logger = discardLog
	}
	m.logger.Store(logger)
	if m.downloader != nil {
		m.downloader.SetLogger(logger)
	}
}

func (m *Manager) log() *slog.Logger {
	if logger := m.logger.Load(); logger != nil {
		return logger
	}
	return discardLog
}

// NewManager creates a download manager.
func NewManager(store task.TaskStore, downloader *HTTPDownloader, cfg Config) *Manager {
	if cfg.MaxActiveTasks <= 0 {
		cfg.MaxActiveTasks = 3 // default 3 active tasks as per spec A10
	}
	if cfg.DefaultConnectionsPerTask <= 0 {
		cfg.DefaultConnectionsPerTask = config.DefaultConnectionsPerTask
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

	if cfg.TempDirectory != "" {
		downloader.SetTempDirectory(cfg.TempDirectory)
	}
	downloader.SetUseServerFileTime(cfg.UseServerFileTime)
	downloader.SetDefaultConcurrency(cfg.DefaultConnectionsPerTask)

	go m.speedTicker()
	return m
}
func (m *Manager) AddListener(l TaskListener) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, l)
}

// notify 广播一次任务变化。
//
// 交给监听者的一律是自有副本：事件载荷会被另一条 goroutine 序列化（Wails 的 mailbox 派发），
// 而发出事件的这一方随后还会继续改写自己的任务对象（状态、错误信息、分片进度）。共享同一个
// 对象就是一次数据竞争——监听者读到正在被改写的结构，还可能收到自相矛盾的载荷。
func (m *Manager) notify(t *task.Task) {
	m.notifyPayload(t.Clone())
}

// notifyPayload 广播一份已经确定不会再被改写的载荷；调用方负责保证这一点。
func (m *Manager) notifyPayload(payload *task.Task) {
	if payload == nil {
		return
	}
	for _, l := range m.listeners {
		l.OnTaskUpdated(payload)
	}
}

func (m *Manager) notifyDelete(taskID string) {
	for _, l := range m.listeners {
		if dl, ok := l.(TaskDeleteListener); ok {
			dl.OnTaskDeleted(taskID)
		}
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
// headers 是这次请求的上下文（Referer/Cookie 等）：需要登录或防盗链才能访问的链接，只有带上
// 它才探得出真实的大小与文件名——交接过来的链接正是这种情形。
func (m *Manager) ProbeURL(ctx context.Context, urlStr string, headers map[string]string) (*ProbeResult, error) {
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

	probe, probeErr := m.probeResource(ctx, urlStr, credentials.New(headers))
	probe.DuplicateTask = dupTask
	return probe, probeErr
}

// probeResource 把下载器的探测结果整理成建任务与界面共用的那份事实。探测失败时它仍然返回一个
// 带兜底文件名的结果：界面据此显示「未知大小」，而失败的下载也还要留下一条看得见的任务记录。
func (m *Manager) probeResource(ctx context.Context, urlStr string, creds credentials.RequestCredentials) (*ProbeResult, error) {
	info, err := m.downloader.Probe(ctx, urlStr, creds)

	probe := &ProbeResult{URL: urlStr, TotalBytes: -1}
	if info != nil {
		probe.Filename = info.Filename
		probe.TotalBytes = info.TotalBytes
		probe.Resumable = info.Resumable
		probe.ETag = info.ETag
		probe.LastModified = info.LastModified
		probe.ContentType = info.ContentType
	}

	if looksLikePlaylist(urlStr, probe.ContentType) {
		hlsProbe, hlsErr := m.inspectPlaylist(ctx, urlStr, creds)
		if hlsErr != nil {
			// 看起来是清单却读不出来，就不要把它当普通文件接着走：把一个 m3u8 下成成品文件
			// 不是用户要的东西，按失败报出来更接近事实。
			if probe.Filename == "" {
				probe.Filename = extractFilenameFromURL(urlStr)
			}
			probe.Filename = HLSOutputName(urlStr, probe.Filename)
			return probe, hlsErr
		}
		probe.HLS = hlsProbe
		// 清单自己的字节数不是这次下载的大小——真正的分片要等清晰度选定后才知道。
		probe.TotalBytes = -1
		probe.Filename = HLSOutputName(urlStr, probe.Filename)

		// 只有一版就没有选择可做，直接定下来。多清晰度时留在这里等用户选：
		// 替用户在几版之间挑一个，等于替他做了一个他没做过的决定。
		if len(hlsProbe.Variants) == 1 {
			src, resolveErr := m.resolveHLS(ctx, urlStr, hlsProbe.Variants[0].URI, creds)
			if resolveErr != nil {
				return probe, resolveErr
			}
			ApplyHLSSelection(probe, src)
		}
	}

	if probe.Filename == "" {
		probe.Filename = extractFilenameFromURL(urlStr)
	}
	return probe, err
}

// ProbeMediaDuration 读远端媒体的时长（秒），供文件信息窗口在下载前展示。
// 它不是任务流程的一环：读不出来就不显示，绝不因此影响下载本身。
func (m *Manager) ProbeMediaDuration(ctx context.Context, urlStr, filename string, totalBytes int64, headers map[string]string) (float64, bool) {
	return m.downloader.ProbeMediaDuration(ctx, urlStr, filename, credentials.New(headers), totalBytes)
}

// ResolveDuplicate resolves a duplicate task with strategy "continue", "redownload", "copy", or "show_completed".
func (m *Manager) ResolveDuplicate(ctx context.Context, taskID, strategy, dir, filename string, maxConn int) (*task.Task, error) {
	return m.ResolveDuplicateFromProbe(ctx, taskID, strategy, dir, filename, maxConn, nil)
}

// ResolveDuplicateFromProbe 支持在重新下载或副本保存时应用当次选定的探测事实（如 HLS 清晰度与大小）。
func (m *Manager) ResolveDuplicateFromProbe(ctx context.Context, taskID, strategy, dir, filename string, maxConn int, probe *ProbeResult) (*task.Task, error) {
	t, err := m.store.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}
	switch strategy {
	case "continue":
		// 续传沿用同一位置的既有分片，因此清理也按该位置进行：同链接的失效残留记录
		// （同落点、或同名但文件已不在磁盘上）一样要收掉，否则它们会一直挂在任务列表里。
		m.cleanStaleDuplicateRecords(ctx, taskID, t.URL, t.Directory, t.Filename)
		if t.Status == task.StatusPaused || t.Status == task.StatusError {
			if err := m.Resume(ctx, taskID); err != nil {
				return nil, err
			}
		}
		return t, nil

	case "redownload":
		if filename == "" {
			filename = t.Filename
		}
		if dir == "" {
			dir = t.Directory
		}
		if maxConn <= 0 {
			maxConn = t.MaxConcurrency
		}
		removeDestinationFiles(dir, filename)

		// 被这次下载替换掉的历史记录：落在同一目标位置，或成品文件已经不在磁盘上（也不剩分片），
		// 就都是残留记录，无论它当初落在哪个目录都要清掉——否则换过默认目录、手动存到别处或
		// 搬走过文件之后，这条记录会一直挂在列表里。文件仍完好的（可能在别的目录）保留，
		// 那是用户自己的一份成品，不是残留。
		if SamePath(t.Directory, dir) || !FileExists(filepath.Join(t.Directory, t.Filename)) {
			_ = m.Delete(ctx, taskID)
		}
		m.cleanStaleDuplicateRecords(ctx, taskID, t.URL, dir, filename)

		totalBytes := t.TotalBytes
		media := t.Media
		if probe != nil {
			if probe.TotalBytes > 0 {
				totalBytes = probe.TotalBytes
			}
			if probe.HLS != nil && probe.HLS.Media != nil {
				media = probe.HLS.Media
				filename = HLSOutputName(t.URL, filename)
			}
		}

		newTask := &task.Task{
			ID:             fmt.Sprintf("task_%d", time.Now().UnixNano()),
			URL:            t.URL,
			Filename:       filename,
			Directory:      dir,
			TempDir:        m.getTempDir(),
			TotalBytes:     totalBytes,
			Downloaded:     0,
			Status:         task.StatusQueued,
			MaxConcurrency: maxConn,
			Resumable:      t.Resumable,
			ETag:           t.ETag,
			LastModified:   t.LastModified,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
			RequestHeaders: t.RequestHeaders.Clone(),
			Media:          media,
		}
		if err := m.store.Save(ctx, newTask); err != nil {
			return nil, err
		}
		m.notify(newTask)
		m.schedule()
		return newTask, nil
	case "copy":
		if filename == "" {
			filename = t.Filename
		}
		if dir == "" {
			dir = t.Directory
		}
		// Clean up missing copy tasks now that user explicitly confirmed creating a copy
		_, _ = m.CleanMissingNumberedCopies(ctx, t.URL, dir, filename)
		filename = m.Occupancy(ctx, nil).NumberedCopy(dir, filename)
		if maxConn <= 0 {
			maxConn = t.MaxConcurrency
		}

		totalBytes := t.TotalBytes
		media := t.Media
		if probe != nil {
			if probe.TotalBytes > 0 {
				totalBytes = probe.TotalBytes
			}
			if probe.HLS != nil && probe.HLS.Media != nil {
				media = probe.HLS.Media
				filename = HLSOutputName(t.URL, filename)
			}
		}

		newTask := &task.Task{
			ID:             fmt.Sprintf("task_%d", time.Now().UnixNano()),
			URL:            t.URL,
			Filename:       filename,
			Directory:      dir,
			TempDir:        m.getTempDir(),
			TotalBytes:     totalBytes,
			Downloaded:     0,
			Status:         task.StatusQueued,
			MaxConcurrency: maxConn,
			Resumable:      t.Resumable,
			ETag:           t.ETag,
			LastModified:   t.LastModified,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
			RequestHeaders: t.RequestHeaders.Clone(),
			Media:          media,
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

// cleanStaleDuplicateRecords 清理同一 URL 的失效残留记录：既包括会落在同一目标位置的记录，
// 也包括同名但成品文件已不在磁盘上的幽灵记录。正在传输、排队或处理中的任务以及 excludeID
// 本身不受影响。续传与重新下载共用它，使「确认一次就把该链接的残留收干净」的行为保持一致。
func (m *Manager) cleanStaleDuplicateRecords(ctx context.Context, excludeID, url, dir, filename string) {
	existingList, err := m.store.List(ctx)
	if err != nil {
		return
	}
	for _, et := range existingList {
		if et.ID == excludeID || et.URL != url {
			continue
		}
		if et.Status == task.StatusDownloading || et.Status == task.StatusQueued || et.Status == task.StatusProcessing {
			continue
		}
		isSameDest := SamePath(et.Directory, dir) && SameFilename(et.Filename, filename)
		isStaleGhost := SameFilename(et.Filename, filename) && !FileExists(filepath.Join(et.Directory, et.Filename))
		if isSameDest || isStaleGhost {
			_ = m.Delete(ctx, et.ID)
		}
	}
}

// ReuseExistingFile moves an existing identical file from another directory to targetDir/targetFilename,
// cleans any duplicate stale tasks whose files do not exist, and registers/updates the file as a completed task.
func (m *Manager) ReuseExistingFile(ctx context.Context, taskID, targetDir, targetFilename string) (*task.Task, error) {
	t, err := m.store.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}
	if t.Status != task.StatusCompleted {
		return nil, fmt.Errorf("task is not completed (status: %s)", t.Status)
	}

	srcPath := filepath.Join(t.Directory, t.Filename)
	if !FileExists(srcPath) {
		return nil, fmt.Errorf("source file does not exist on disk: %s", srcPath)
	}

	if targetFilename == "" {
		targetFilename = t.Filename
	}
	if targetDir == "" {
		targetDir = t.Directory
	}

	destPath := filepath.Join(targetDir, targetFilename)
	if !SamePath(srcPath, destPath) {
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create target directory: %w", err)
		}
		if err := moveFile(srcPath, destPath); err != nil {
			return nil, fmt.Errorf("failed to move file to %s: %w", destPath, err)
		}
	}

	// Clean other duplicate tasks for this URL where disk file does not exist
	if existingList, err := m.store.List(ctx); err == nil {
		for _, et := range existingList {
			if et.ID == taskID || et.URL != t.URL {
				continue
			}
			if et.Status == task.StatusDownloading || et.Status == task.StatusQueued || et.Status == task.StatusProcessing {
				continue
			}
			targetFilePath := filepath.Join(et.Directory, et.Filename)
			if !FileExists(targetFilePath) {
				_ = m.Delete(ctx, et.ID)
			}
		}
	}

	t.Directory = targetDir
	t.Filename = targetFilename
	t.Status = task.StatusCompleted
	t.Downloaded = t.TotalBytes
	t.ErrorMsg = ""
	t.UpdatedAt = time.Now()

	if err := m.store.Save(ctx, t); err != nil {
		return nil, err
	}
	m.notify(t)
	return t, nil
}

// CleanMissingNumberedCopies scans tasks in dir matching the numbered copy pattern for filename
// (excluding the original base file itself). Any copy whose destination file no longer exists on disk
// (and is not actively downloading or preparing) is deleted from the task store.
// If urlStr is non-empty, only copies sharing the same URL are cleaned.
func (m *Manager) CleanMissingNumberedCopies(ctx context.Context, urlStr, dir, filename string) ([]string, error) {
	if dir == "" || filename == "" {
		return nil, nil
	}
	baseStem, ext := ExtractStemAndExt(filename)
	existingList, err := m.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read task list: %w", err)
	}

	var deletedIDs []string
	for _, t := range existingList {
		if !SamePath(t.Directory, dir) {
			continue
		}
		if urlStr != "" && t.URL != urlStr {
			continue
		}
		// Only check numbered copies, never touch the original task
		if _, ok := IsNumberedCopyOf(t.Filename, baseStem, ext); !ok {
			continue
		}
		// Do not delete active downloading/queued/processing tasks
		if t.Status == task.StatusDownloading || t.Status == task.StatusQueued || t.Status == task.StatusProcessing {
			continue
		}
		// Check if file exists on disk
		filePath := filepath.Join(t.Directory, t.Filename)
		if !FileExists(filePath) {
			if delErr := m.Delete(ctx, t.ID); delErr == nil {
				deletedIDs = append(deletedIDs, t.ID)
			}
		}
	}
	return deletedIDs, nil
}

// Occupancy 组装一份目标落点的占用判定：任务库快照取自本地存储，排队项来源由调用方补上
// （引擎不认识文件信息窗口的队列）。
//
// 任务库读不出来时按「没有记录」处理并记一条日志：命名不能因为一次读库失败就整个失败，
// 那会让窗口连一个可用的建议名字都给不出。磁盘与排队项两类来源仍然有效，判定只是少了一类。
func (m *Manager) Occupancy(ctx context.Context, reserved Reserved) Occupancy {
	existingList, err := m.store.List(ctx)
	if err != nil {
		m.log().Warn("occupancy: task list unavailable", "error", err.Error())
		return Occupancy{Reserved: reserved}
	}
	return Occupancy{Tasks: existingList, Reserved: reserved}
}

// FindDuplicateTask finds an existing task with matching urlStr from local store without network probe.
func (m *Manager) FindDuplicateTask(ctx context.Context, urlStr string) (*task.Task, error) {
	if urlStr == "" {
		return nil, nil
	}
	existingList, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, ext := range existingList {
		if ext.URL == urlStr {
			return ext, nil
		}
	}
	return nil, nil
}

// PreDownloadRequest 是启动一次提前下载的输入。
type PreDownloadRequest struct {
	URL       string
	Directory string
	Filename  string
	MaxConn   int
	Headers   map[string]string
	// Probe 是登记时已经完成的探测结果（HLS 链接还带着选定清晰度后的来源）。
	// 为 nil 时自己探一次。
	Probe *ProbeResult
}

// StartPreDownloadFromProbe creates a task that begins transferring while the file info dialog is
// still open.
//
// 它接受一份现成的探测结果：HLS 链接的清晰度是在打开对话框之前选定的，那份事实必须原样
// 传给任务，否则提前下载下来的会是另一个版本（Ticket 07：提前下载使用已选版本）。
func (m *Manager) StartPreDownloadFromProbe(ctx context.Context, req PreDownloadRequest) (*task.Task, error) {
	creds := credentials.New(req.Headers)
	probe := req.Probe
	var probeErr error
	if probe == nil {
		probe, probeErr = m.probeResource(ctx, req.URL, creds)
	}
	return m.createTask(ctx, req.URL, req.Directory, req.Filename, req.MaxConn, creds, probe, probeErr)
}

// StartPreDownload creates a pre-download task without request context.
func (m *Manager) StartPreDownload(ctx context.Context, urlStr, dir, filename string, maxConn int) (*task.Task, error) {
	return m.StartPreDownloadFromProbe(ctx, PreDownloadRequest{
		URL: urlStr, Directory: dir, Filename: filename, MaxConn: maxConn,
	})
}

// StartPreDownloadWithHeaders creates a pre-download task with optional request headers.
func (m *Manager) StartPreDownloadWithHeaders(ctx context.Context, urlStr, dir, filename string, maxConn int, headers map[string]string) (*task.Task, error) {
	return m.StartPreDownloadFromProbe(ctx, PreDownloadRequest{
		URL: urlStr, Directory: dir, Filename: filename, MaxConn: maxConn, Headers: headers,
	})
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
				if err := moveFile(oldDest, newDest); err != nil {
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

			oldPart := m.downloader.GetPartPath(t)
			tempTask := *t
			tempTask.Directory = finalDir
			tempTask.Filename = finalFilename
			newPart := m.downloader.GetPartPath(&tempTask)
			if oldPart != newPart {
				if _, err := os.Stat(oldPart); err == nil {
					// 分片搬不过去就放弃已有进度：留在旧位置的分片与新落点的下载记录已脱节。
					if err := moveFile(oldPart, newPart); err != nil {
						_ = os.Remove(oldPart)
						t.Chunks = nil
						t.Downloaded = 0
					}
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
		probeHeaders = credentials.New(headers)
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
		t.RequestHeaders = credentials.New(headers)
	}
	// Adopt the refreshed resource's metadata so size, resumability and validators stop describing
	// the dead link. A failed probe is not fatal here: the caller already verified the link, and the
	// transfer itself reports a real error if it cannot proceed.
	//
	// HLS 任务换链接要重新选定清晰度，认不出原来那一个就拒绝这次更新——替用户挑版本不是
	// 这里该做的事。
	if err := m.adoptNewURL(ctx, t, newURL); err != nil {
		return nil, err
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

// adoptNewURL 把一次针对新链接的探测结果落进已有任务：大小、断点、校验信息，以及 HLS 来源。
//
// 换了链接就不再是同一份媒体，旧的 HLS 状态一律清掉——留着它会让任务按上一个链接选定的
// 清晰度去下载另一个版本。
func (m *Manager) adoptNewURL(ctx context.Context, t *task.Task, newURL string) error {
	previous := t.Media
	probe, _ := m.probeResource(ctx, newURL, t.RequestHeaders)
	t.URL = newURL

	t.Media = nil
	t.MediaInputs = nil
	t.TransferDone = false
	if probe == nil {
		return nil
	}
	t.TotalBytes = probe.TotalBytes
	t.Resumable = probe.Resumable
	t.ETag = probe.ETag
	t.LastModified = probe.LastModified

	if probe.HLS == nil {
		// 新链接不是清单：名字回到探测给出的那个，不再按成品规则保留 .mp4。
		if previous != nil && probe.Filename != "" {
			t.Filename = probe.Filename
		}
		return nil
	}

	// 新链接还是清单：成品名不变（仍然是处理出来的 MP4），清晰度沿用原来选定的那一个。
	t.Filename = HLSOutputName(newURL, t.Filename)
	picked, ok := reuseVariant(previous, probe.HLS.Variants)
	if !ok {
		return errors.New("新链接的清晰度清单与原来选定的对不上，请重新添加这次下载并选择清晰度")
	}
	source, err := m.resolveHLS(ctx, newURL, picked.URI, t.RequestHeaders.RawHeaders())
	if err != nil {
		return err
	}
	t.Media = source
	t.TotalBytes = source.TotalBytes
	return nil
}

// reuseVariant 在新清单里找回原来选定的清晰度。候选唯一时它本来就没有选择可做；
// 候选多于一个时只认地址完全一致的那一个，认不出来就说明该由用户重新选。
func reuseVariant(previous *hls.Source, variants []hls.Variant) (hls.Variant, bool) {
	if len(variants) == 1 {
		return variants[0], true
	}
	if previous == nil {
		return hls.Variant{}, false
	}
	for _, v := range variants {
		if v.URI == previous.Variant.URI {
			return v, true
		}
	}
	return hls.Variant{}, false
}

// ResetAndDownloadWithNewURL resets progress and downloads from scratch with new URL.
func (m *Manager) ResetAndDownloadWithNewURL(ctx context.Context, taskID, newURL string, headers map[string]string) (*task.Task, error) {
	_ = m.Pause(ctx, taskID)
	m.waitTaskIdle(taskID, 5*time.Second)
	t, err := m.store.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	m.RemoveTaskFiles(t)

	if headers != nil {
		t.RequestHeaders = credentials.New(headers)
	}
	if err := m.adoptNewURL(ctx, t, newURL); err != nil {
		return nil, err
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
}

// AddTask probes the URL, checks duplicates, creates a task in queued or paused state, and triggers queue scheduling.
func (m *Manager) AddTask(ctx context.Context, urlStr, dir, filename string, maxConn int) (*task.Task, error) {
	return m.AddTaskWithHeaders(ctx, urlStr, dir, filename, maxConn, nil)
}

// AddTaskWithHeaders creates a task carrying optional request headers context.
func (m *Manager) AddTaskWithHeaders(ctx context.Context, urlStr, dir, filename string, maxConn int, headers map[string]string) (*task.Task, error) {
	creds := credentials.New(headers)
	probe, probeErr := m.probeResource(ctx, urlStr, creds)
	return m.createTask(ctx, urlStr, dir, filename, maxConn, creds, probe, probeErr)
}

// AddTaskFromProbe 用一次已经完成的探测结果建任务，自己不再联网。
//
// 文件信息窗口靠它做到「提交瞬时」：链接在登记那一刻就已经探过一次（用户读对话框的这段时间
// 足够它跑完），提交只是把手上已有的事实落成任务，不必为同一份元数据再付一次请求往返
// （真实探测会重试三次，还带一次 HEAD 兜底）。
func (m *Manager) AddTaskFromProbe(ctx context.Context, urlStr, dir, filename string, maxConn int, headers map[string]string, probe *ProbeResult, probeErr error) (*task.Task, error) {
	return m.createTask(ctx, urlStr, dir, filename, maxConn, credentials.New(headers), probe, probeErr)
}

// createTask 是两条建任务路径的共同实现：先挡住重复链接，再按探测结果落成一条任务。
// 探测失败时仍然建一条可见的失败任务（A03），而不是什么都不留下。
func (m *Manager) createTask(ctx context.Context, urlStr, dir, filename string, maxConn int, creds credentials.RequestCredentials, probe *ProbeResult, probeErr error) (*task.Task, error) {
	// Check duplicate URL in existing tasks
	existingList, _ := m.store.List(ctx)
	for _, ext := range existingList {
		if ext.URL == urlStr && ext.Status != task.StatusCompleted {
			return nil, fmt.Errorf("该下载链接已存在于任务列表中（状态：%s），请勿重复添加", ext.Status)
		}
	}

	now := time.Now()
	if probeErr != nil {
		// As per A03: even if probe/network fails immediately upon manual confirmation, keep task with error
		t := &task.Task{
			ID:             fmt.Sprintf("task_%d", now.UnixNano()),
			URL:            urlStr,
			Filename:       filename,
			Directory:      dir,
			TempDir:        m.getTempDir(),
			TotalBytes:     -1,
			Status:         task.StatusError,
			FailurePhase:   task.FailurePhaseTransfer,
			ErrorMsg:       probeErr.Error(),
			MaxConcurrency: maxConn,
			RequestHeaders: creds,
			CreatedAt:      now,
			UpdatedAt:      now,
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

	// 清单还没定下要下哪一版就不能建任务：下出来的会是清单本身（只是名字被改成 .mp4），
	// 既不是用户要的成品，也永远不会成功。多清晰度时这一步该由用户选定，其余情形
	// probeResource 已经替它定好了。
	if probe.HLS != nil && probe.HLS.Media == nil {
		return nil, errors.New("这份清单还没有选定清晰度，请先选定一个")
	}

	if filename == "" {
		filename = probe.Filename
	}
	// HLS 的成品是处理出来的 MP4：无论名字从哪来（用户输入、服务器头、URL），
	// 后缀都要落在成品上，不能留下一个名字与内容不符的文件。
	if probe.HLS != nil {
		filename = HLSOutputName(urlStr, filename)
		if len(probe.HLS.Variants) > 1 && probe.HLS.Media != nil {
			filename = HLSVariantFilename(filename, probe.HLS.Media.Variant)
		}
	}
	if maxConn <= 0 {
		maxConn = m.config.DefaultConnectionsPerTask
	}

	t := &task.Task{
		ID:             fmt.Sprintf("task_%d", now.UnixNano()),
		URL:            urlStr,
		Filename:       filename,
		Directory:      dir,
		TempDir:        m.getTempDir(),
		TotalBytes:     probe.TotalBytes,
		Downloaded:     0,
		Status:         task.StatusQueued,
		MaxConcurrency: maxConn,
		Resumable:      probe.Resumable,
		ETag:           probe.ETag,
		LastModified:   probe.LastModified,
		RequestHeaders: creds,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	// HLS 任务的总大小来自选定的清晰度：清单自己的字节数与这次下载无关。
	if probe.HLS != nil && probe.HLS.Media != nil {
		t.Media = probe.HLS.Media
		t.TotalBytes = probe.HLS.Media.TotalBytes
		// 清单声明的时长是下载前就能拿到的已知信息，界面据此展示；成品出来之后
		// 不再覆盖它——同一个数值来回变只会让人以为哪里错了。
		t.Duration = probe.HLS.Media.Duration
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

// Retry retries a failed or paused task, restarting its transfer from the last checkpoint.
// 处理阶段失败的任务必须走 RetryProcessing：分片已就绪，重新传输既无必要也会使分片失效。
func (m *Manager) Retry(ctx context.Context, id string) error {
	t, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if t.FailurePhase == task.FailurePhaseProcessing {
		return ErrProcessingRetryRequired
	}
	return m.Resume(ctx, id)
}

// RetryProcessing 重新跑一次媒体处理，复用已经落盘的分片而不是重新传输。
//
// 它不联网：清单、清晰度与分片落点都记在任务里，处理失败留下的分片也原样保留，
// 于是重试既不必再取一次清单，也不会丢掉上一次失败的原因（ADR-0004）。
func (m *Manager) RetryProcessing(ctx context.Context, id string) error {
	t, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if t.FailurePhase != task.FailurePhaseProcessing {
		return ErrNotProcessingFailure
	}
	if !t.IsHLS() || !t.TransferDone {
		// 没有处理阶段的任务谈不上「仅重试处理」；分片不在就不能假装它还在。
		return ErrNotProcessingFailure
	}

	t.Status = task.StatusQueued
	t.FailurePhase = task.FailurePhaseNone
	t.ErrorMsg = ""
	t.UpdatedAt = time.Now()
	if err := m.store.Save(ctx, t); err != nil {
		return err
	}
	m.notify(t)
	m.schedule()
	return nil
}

// Delete removes task from store and stops running transfer.
func (m *Manager) Delete(ctx context.Context, id string) error {
	_ = m.Pause(ctx, id)
	err := m.store.Delete(ctx, id)
	if err == nil {
		m.notifyDelete(id)
	}
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

// SetTempDirectory updates the configured temporary directory for downloads.
func (m *Manager) SetTempDirectory(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.TempDirectory = dir
	if m.downloader != nil {
		m.downloader.SetTempDirectory(dir)
	}
}

// SetUseServerFileTime updates whether completed files should adopt server Last-Modified time.

// GetPartPath returns the temporary part file path for a task, delegating to downloader.
func (m *Manager) GetPartPath(t *task.Task) string {
	if m.downloader != nil {
		return m.downloader.GetPartPath(t)
	}
	return filepath.Join(t.Directory, t.Filename+".sheepget")
}

func (m *Manager) getTempDir() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.config.TempDirectory
}
func (m *Manager) SetUseServerFileTime(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.UseServerFileTime = enabled
	if m.downloader != nil {
		m.downloader.SetUseServerFileTime(enabled)
	}
}

// SetProxy updates the proxy configuration on the downloader.
func (m *Manager) SetProxy(mode, customAddr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.downloader != nil {
		return m.downloader.SetProxy(mode, customAddr)
	}
	return nil
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
	taskStarted := time.Now()
	t, err := m.store.Get(bgCtx, taskID)
	if err != nil {
		m.finishTask(taskID)
		return
	}

	t.Status = task.StatusDownloading
	// 本次运行重新开始，清掉上一轮遗留的失败阶段。
	t.FailurePhase = task.FailurePhaseNone
	t.UpdatedAt = time.Now()
	_ = m.store.Save(bgCtx, t)
	m.notify(t)

	sink := newProgressSink(bgCtx, m, taskID)

	// TransferDone 是「分片已经全部就绪」这一事实的记录。处理失败重试时它让任务直接进入
	// 处理阶段，不必再为取一次清单把网络走一遍（ADR-0004：两条失败路径各自恢复）。
	var transferErr error
	if !t.TransferDone {
		// 传输期间任务的可变对象只属于传输侧：交出去的是工作副本，这里手上的那份不再被改写，
		// 所有对外发布都经 sink 走副本。传输结束后以工作副本为准合并——分片进度、ETag、
		// 大小这些由传输侧写出的全部事实都在里面，因此失败与暂停路径保存的也是最新状态，
		// 断点续传不受影响。
		work := t.Clone()
		transferErr = m.runTransfer(ctx, work, sink)
		*t = *work
		if transferErr == nil {
			t.TransferDone = true
			_ = m.store.Save(bgCtx, t)
		}
	}

	// 传输一结束就释放下载名额：媒体处理不再占用它，排队中的下一个任务可以进来。
	m.finishTask(taskID)
	m.schedule()

	var processErr error
	if transferErr == nil && t.IsHLS() {
		t.Status = task.StatusProcessing
		t.UpdatedAt = time.Now()
		_ = m.store.Save(bgCtx, t)
		m.notify(t)

		processErr = m.runMediaProcessing(ctx, t)
		if processErr == nil {
			// 成品已经生成，分片不再有任何用途。
			m.removeSegmentDir(t)
		}
	}

	t.Speed = 0
	switch {
	case transferErr != nil && canceledOrPaused(transferErr, ctx):
		t.Status = task.StatusPaused
		t.FailurePhase = task.FailurePhaseNone
	case transferErr != nil && m.pausedElsewhere(bgCtx, taskID):
		// A concurrent Pause already persisted the paused state; keep it.
		t.Status = task.StatusPaused
		t.FailurePhase = task.FailurePhaseNone
	case transferErr != nil:
		t.Status = task.StatusError
		t.FailurePhase = task.FailurePhaseTransfer
		t.ErrorMsg = transferErr.Error()
	case processErr != nil && canceledOrPaused(processErr, ctx):
		// 处理中途被暂停或取消：分片原样保留，继续时从处理阶段接着走。
		t.Status = task.StatusPaused
		t.FailurePhase = task.FailurePhaseNone
	case processErr != nil && m.pausedElsewhere(bgCtx, taskID):
		t.Status = task.StatusPaused
		t.FailurePhase = task.FailurePhaseNone
	case processErr != nil:
		t.Status = task.StatusError
		t.FailurePhase = task.FailurePhaseProcessing
		t.ErrorMsg = processErr.Error()
	default:
		t.Status = task.StatusCompleted
		t.FailurePhase = task.FailurePhaseNone
		if t.TotalBytes > 0 {
			// For a known size the transfer is complete by definition; when the size was never
			// known, keep the bytes actually written instead of reporting the unknown marker.
			t.Downloaded = t.TotalBytes
		}
		t.ErrorMsg = ""
		// 文件已经落地：媒体时长从这里读，不再回头看远端。读不出来就留 0，
		// 界面按「没有时长」显示，而不是一直转圈。
		if t.Duration <= 0 {
			if seconds, ok := mediainfo.FromFile(filepath.Join(t.Directory, t.Filename)); ok {
				t.Duration = seconds
			}
		}
	}
	t.UpdatedAt = time.Now()
	_ = m.store.Save(bgCtx, t)
	m.notify(t)

	// 任务收尾记一行：失败了要有原因，完成了要有耗时与请求数——「是网络还是引擎」的答案就在这行。
	logFields := []any{
		"task", t.ID, "status", string(t.Status), "phase", string(t.FailurePhase),
		"bytes", t.Downloaded, "total", t.TotalBytes,
		"ms", time.Since(taskStarted).Milliseconds(), "url", logging.SafeURL(t.URL),
	}
	if t.ErrorMsg != "" {
		logFields = append(logFields, "error", t.ErrorMsg)
	}
	if t.Status == task.StatusError {
		m.log().Warn("task finished", logFields...)
	} else {
		m.log().Info("task finished", logFields...)
	}

	// Free slot and promote next queued task
	m.schedule()
}

// runTransfer 执行任务的传输阶段：HLS 任务下载分片，其余任务走 HTTP 传输。
// t 是这次传输的工作副本，sink 是它对外发布的唯一出口。
func (m *Manager) runTransfer(ctx context.Context, t *task.Task, sink TransferSink) error {
	if !t.IsHLS() {
		return m.downloader.Download(ctx, t, sink)
	}
	inputs, err := m.runHLSTransfer(ctx, t, sink)
	if err != nil {
		return err
	}
	t.MediaInputs = inputs
	return nil
}

// canceledOrPaused 判断一次失败是不是这次运行的上下文被取消（暂停或退出）导致的。
func canceledOrPaused(err error, ctx context.Context) bool {
	return errors.Is(err, context.Canceled) || ctx.Err() != nil
}

// pausedElsewhere 判断任务是否已被别的路径（例如并发到达的暂停操作）置为暂停。
func (m *Manager) pausedElsewhere(ctx context.Context, taskID string) bool {
	latest, err := m.store.Get(ctx, taskID)
	return err == nil && latest.Status == task.StatusPaused
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

// SetTaskPageURL updates and persists the PageURL for the specified task.
func (m *Manager) SetTaskPageURL(ctx context.Context, taskID, pageURL string) error {
	t, err := m.store.Get(ctx, taskID)
	if err != nil {
		return err
	}
	t.PageURL = pageURL
	return m.store.Save(ctx, t)
}
