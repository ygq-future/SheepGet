// Package window manages desktop window lifecycle, single-instance fileinfo queue,
// and cross-window coordination.
package window

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"sheep-get/internal/config"
	"sheep-get/internal/engine"
	"sheep-get/internal/task"
	"strings"
	"sync"
	"time"
)

// WindowView abstracts a native OS/Wails window.
type WindowView interface {
	Show()
	Hide()
	Focus()
	Emit(event string, data any)
}

// DownloadEngine abstracts download engine operations required by the window controller.
type DownloadEngine interface {
	ProbeURL(ctx context.Context, urlStr string) (*engine.ProbeResult, error)
	FindDuplicateTask(ctx context.Context, urlStr string) (*task.Task, error)
	AddTask(ctx context.Context, urlStr, dir, filename string, maxConn int) (*task.Task, error)
	AddTaskWithHeaders(ctx context.Context, urlStr, dir, filename string, maxConn int, headers map[string]string) (*task.Task, error)
	StartPreDownload(ctx context.Context, urlStr, dir, filename string, maxConn int) (*task.Task, error)
	StartPreDownloadWithHeaders(ctx context.Context, urlStr, dir, filename string, maxConn int, headers map[string]string) (*task.Task, error)
	ConfirmPreDownload(ctx context.Context, taskID, finalDir, finalFilename string, maxConn int) (*task.Task, error)
	CancelPreDownload(ctx context.Context, taskID string) error
	ResolveDuplicate(ctx context.Context, taskID, strategy, dir, filename string, maxConn int) (*task.Task, error)
	NumberedCopyName(ctx context.Context, dir, filename string) (string, error)
}

// SettingsProvider provides active application configuration.
type SettingsProvider interface {
	Get() config.Settings
}

// DownloadRequest represents an incoming download request to be displayed in the FileInfo window.
type DownloadRequest struct {
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers,omitempty"`
	Directory   string            `json:"directory,omitempty"`
	Filename    string            `json:"filename,omitempty"`
	MaxConn     int               `json:"maxConn,omitempty"`
	PreDownload *bool             `json:"preDownload,omitempty"`
}

// DownloadResponse is the result of enqueuing or dispatching a download request.
type DownloadResponse struct {
	Handled   bool   `json:"handled"`
	Action    string `json:"action"` // "enqueued", "skip_show_completed", etc.
	RequestID string `json:"requestId,omitempty"`
	TaskID    string `json:"taskId,omitempty"`
}

// FileInfoItem represents a single item waiting in the file info window queue.
type FileInfoItem struct {
	ID                string                    `json:"id"`
	URL               string                    `json:"url"`
	Filename          string                    `json:"filename"`
	SuggestedFilename string                    `json:"suggestedFilename"`
	Directory         string                    `json:"directory"`
	TotalBytes        int64                     `json:"totalBytes"`
	MimeType          string                    `json:"mimeType"`
	Resumable         bool                      `json:"resumable"`
	MaxConn           int                       `json:"maxConn"`
	PreDownload       bool                      `json:"preDownload"`
	PreDownloadTaskID string                    `json:"preDownloadTaskId,omitempty"`
	FileConflict      bool                      `json:"fileConflict"`
	DuplicateTask     *task.Task                `json:"duplicateTask,omitempty"`
	DuplicatePolicy   config.DuplicateURLPolicy `json:"duplicatePolicy"`
	QueueIndex        int                       `json:"queueIndex"`
	QueueTotal        int                       `json:"queueTotal"`
	Headers           map[string]string         `json:"headers,omitempty"`
}

// FileInfoSubmission represents user confirmation from the FileInfo window.
type FileInfoSubmission struct {
	RequestID         string `json:"requestId"`
	URL               string `json:"url"`
	Filename          string `json:"filename"`
	Directory         string `json:"directory"`
	MaxConn           int    `json:"maxConn"`
	DuplicateStrategy string `json:"duplicateStrategy,omitempty"` // "prompt", "continue", "redownload", "copy", "continue_overwrite", "show_completed"
	PreDownload       bool   `json:"preDownload"`
	OverwriteConflict bool   `json:"overwriteConflict"`
}

// QueueController coordinates the single-instance FileInfo window and its FIFO queue.
type QueueController struct {
	mu              sync.Mutex
	engine          DownloadEngine
	settings        SettingsProvider
	windowView      WindowView
	items           []*FileInfoItem
	activeIndex     int
	onShowCompleted func(taskID string)
}

func NewQueueController(eng DownloadEngine, settings SettingsProvider, winView WindowView) *QueueController {
	return &QueueController{
		engine:      eng,
		settings:    settings,
		windowView:  winView,
		items:       make([]*FileInfoItem, 0),
		activeIndex: 0,
	}
}

// SetOnShowCompleted registers a hook to be triggered when skip_show_completed policy is invoked.
func (qc *QueueController) SetOnShowCompleted(fn func(taskID string)) {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	qc.onShowCompleted = fn
}

// QueueLength returns the number of items currently in the queue.
func (qc *QueueController) QueueLength() int {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	return len(qc.items)
}

// GetActive returns the currently displayed FileInfoItem, or nil if queue is empty.
func (qc *QueueController) GetActive() (*FileInfoItem, error) {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	if len(qc.items) == 0 {
		return nil, nil
	}
	if qc.activeIndex >= len(qc.items) {
		qc.activeIndex = 0
	}
	qc.updateQueueNumbersLocked()
	item := *qc.items[qc.activeIndex]
	return &item, nil
}

// GetQueueItems returns a copy of all items currently in the queue.
func (qc *QueueController) GetQueueItems() []*FileInfoItem {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	qc.updateQueueNumbersLocked()
	res := make([]*FileInfoItem, len(qc.items))
	for i, it := range qc.items {
		copyItem := *it
		res[i] = &copyItem
	}
	return res
}

// SwitchActive switches the currently active item in the queue to index and notifies the window.
func (qc *QueueController) SwitchActive(index int) (*FileInfoItem, error) {
	qc.mu.Lock()
	defer qc.mu.Unlock()

	if index < 0 || index >= len(qc.items) {
		return nil, fmt.Errorf("queue index out of range: %d", index)
	}

	qc.activeIndex = index
	qc.updateQueueNumbersLocked()
	activeItem := qc.items[qc.activeIndex]

	if qc.windowView != nil {
		qc.windowView.Emit("fileinfo:next", activeItem)
	}

	copyItem := *activeItem
	return &copyItem, nil
}
func (qc *QueueController) updateQueueNumbersLocked() {
	total := len(qc.items)
	for i, item := range qc.items {
		item.QueueIndex = i + 1
		item.QueueTotal = total
	}
}

// Enqueue adds a download request to the window queue or handles duplicate skip policy.
func (qc *QueueController) Enqueue(ctx context.Context, req DownloadRequest) (*DownloadResponse, error) {
	qc.mu.Lock()
	defer qc.mu.Unlock()

	activeSettings := qc.settings.Get()
	maxConn := req.MaxConn
	if maxConn <= 0 {
		maxConn = activeSettings.Download.DefaultConnectionsPerTask
	}

	preDownload := activeSettings.Download.PreDownload
	if req.PreDownload != nil {
		preDownload = *req.PreDownload
	}

	policy := activeSettings.Download.DuplicateURLPolicy

	// Fast local duplicate check without blocking on network probe
	var dupTask *task.Task
	if req.URL != "" {
		dupTask, _ = qc.engine.FindDuplicateTask(ctx, req.URL)
	}

	// Check Duplicate policy: skip_show_completed
	// Opens the download completed dialog for reference without closing/suppressing the FileInfo dialog
	if dupTask != nil && dupTask.Status == task.StatusCompleted {
		if policy == config.DuplicatePolicySkipShowCompleted || policy == config.DuplicatePolicySkipShowLegacy {
			if qc.onShowCompleted != nil {
				qc.onShowCompleted(dupTask.ID)
			}
		}
	}
	filename := req.Filename
	if filename == "" && req.URL != "" {
		urlPath := strings.Split(req.URL, "?")[0]
		base := path.Base(urlPath)
		if base != "" && base != "." && base != "/" {
			filename = base
		}
	}
	if filename == "" && dupTask != nil && dupTask.Filename != "" {
		filename = dupTask.Filename
	}
	if filename == "" && req.URL != "" {
		filename = "download.bin"
	}

	dir := req.Directory
	if dir == "" {
		if filename != "" {
			dir = activeSettings.Download.ResolveCategoryDirectory(filename)
		} else {
			dir = activeSettings.Download.DefaultDirectory
		}
	}

	conflict := false
	suggested := ""
	if filename != "" {
		conflict, suggested = engine.CheckFileConflict(dir, filename)
		if copyName, err := qc.engine.NumberedCopyName(ctx, dir, filename); err == nil && copyName != "" {
			suggested = copyName
		}
	}
	// If duplicate policy is numbered_copy and duplicate exists, auto-fill numbered copy name
	if dupTask != nil && (policy == config.DuplicatePolicyNumberedCopy) {
		filename = suggested
		conflict = false
	}
	item := &FileInfoItem{
		ID:                fmt.Sprintf("req_%d", time.Now().UnixNano()),
		URL:               req.URL,
		Filename:          filename,
		SuggestedFilename: suggested,
		Directory:         dir,
		TotalBytes:        -1,
		MimeType:          "",
		Resumable:         false,
		MaxConn:           maxConn,
		PreDownload:       preDownload,
		FileConflict:      conflict,
		DuplicateTask:     dupTask,
		DuplicatePolicy:   policy,
		Headers:           req.Headers,
	}

	qc.items = append(qc.items, item)
	qc.updateQueueNumbersLocked()

	// Show window and bring to focus immediately
	if qc.windowView != nil {
		qc.windowView.Show()
		qc.windowView.Focus()
		if len(qc.items) == 1 {
			qc.windowView.Emit("fileinfo:next", item)
		} else {
			// If already open with an item, update it immediately to the latest manual click so the user immediately sees response
			qc.windowView.Emit("fileinfo:queue_updated", map[string]int{
				"index": qc.items[0].QueueIndex,
				"total": qc.items[0].QueueTotal,
			})
		}
	}

	// Trigger background async probe if URL is present
	if req.URL != "" {
		itemID := item.ID
		reqURL := req.URL
		reqHeaders := req.Headers
		go qc.asyncProbeItem(itemID, reqURL, reqHeaders, dir, policy, preDownload, maxConn)
	}

	return &DownloadResponse{
		Handled:   true,
		Action:    "enqueued",
		RequestID: item.ID,
	}, nil
}

func (qc *QueueController) asyncProbeItem(itemID, reqURL string, headers map[string]string, dir string, policy config.DuplicateURLPolicy, preDownload bool, maxConn int) {
	probeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	probe, err := qc.engine.ProbeURL(probeCtx, reqURL)
	if err != nil && probe == nil {
		return
	}

	qc.mu.Lock()
	defer qc.mu.Unlock()

	var targetItem *FileInfoItem
	for _, it := range qc.items {
		if it.ID == itemID {
			targetItem = it
			break
		}
	}
	if targetItem == nil {
		return
	}

	if probe != nil {
		targetItem.TotalBytes = probe.TotalBytes
		targetItem.MimeType = probe.ContentType
		targetItem.Resumable = probe.Resumable
		if probe.DuplicateTask != nil {
			targetItem.DuplicateTask = probe.DuplicateTask
		}
		if probe.Filename != "" && (targetItem.Filename == "download.bin" || targetItem.Filename == path.Base(strings.Split(reqURL, "?")[0])) {
			targetItem.Filename = probe.Filename
		}
	}

	if targetItem.DuplicateTask != nil && targetItem.DuplicateTask.Status == task.StatusCompleted {
		if policy == config.DuplicatePolicySkipShowCompleted || policy == config.DuplicatePolicySkipShowLegacy {
			if qc.onShowCompleted != nil {
				qc.onShowCompleted(targetItem.DuplicateTask.ID)
			}
		}
	}

	conflict, suggested := engine.CheckFileConflict(dir, targetItem.Filename)
	if copyName, err := qc.engine.NumberedCopyName(context.Background(), dir, targetItem.Filename); err == nil && copyName != "" {
		suggested = copyName
	}
	targetItem.SuggestedFilename = suggested
	targetItem.FileConflict = conflict

	if targetItem.DuplicateTask != nil && (policy == config.DuplicatePolicyNumberedCopy) {
		targetItem.Filename = suggested
		targetItem.FileConflict = false
	}

	if preDownload && !targetItem.FileConflict && targetItem.DuplicateTask == nil && targetItem.PreDownloadTaskID == "" {
		if preTask, err := qc.engine.StartPreDownloadWithHeaders(context.Background(), reqURL, dir, targetItem.Filename, maxConn, headers); err == nil && preTask != nil {
			targetItem.PreDownloadTaskID = preTask.ID
		}
	}

	if qc.windowView != nil {
		qc.windowView.Emit("fileinfo:updated", targetItem)
	}
}

// Submit confirms the active file info item with final user choices.
func (qc *QueueController) Submit(ctx context.Context, sub FileInfoSubmission) (*task.Task, error) {
	qc.mu.Lock()
	defer qc.mu.Unlock()

	if len(qc.items) == 0 {
		return nil, fmt.Errorf("no active file info request")
	}

	if qc.activeIndex >= len(qc.items) {
		qc.activeIndex = 0
	}
	active := qc.items[qc.activeIndex]

	// If active.DuplicateTask is nil, check if sub.URL matches an existing task
	if active.DuplicateTask == nil && sub.URL != "" {
		probe, _ := qc.engine.ProbeURL(ctx, sub.URL)
		if probe != nil && probe.DuplicateTask != nil {
			active.DuplicateTask = probe.DuplicateTask
		}
	}
	var (
		resTask *task.Task
		err     error
	)

	// Duplicate resolution
	strategy := sub.DuplicateStrategy
	destFileExists := engine.FileExists(filepath.Join(sub.Directory, sub.Filename))

	if strategy == "" && active.DuplicateTask != nil {
		if !destFileExists && active.DuplicateTask.Status == task.StatusCompleted && engine.SamePath(active.DuplicateTask.Directory, sub.Directory) {
			// Disk file does not exist for completed task in the same directory:
			// user directly downloads and we clean the stale duplicate task
			strategy = "redownload"
		} else {
			switch active.DuplicatePolicy {
			case config.DuplicatePolicyContinueOverwrite, config.DuplicatePolicyOverwriteLegacy:
				strategy = "continue_overwrite"
			case config.DuplicatePolicyNumberedCopy:
				strategy = "copy"
			case config.DuplicatePolicySkipShowCompleted, config.DuplicatePolicySkipShowLegacy:
				strategy = "show_completed"
			}
		}
	}

	if strategy != "" && active.DuplicateTask != nil {
		if strategy == "continue_overwrite" {
			if active.DuplicateTask.Status == task.StatusCompleted {
				strategy = "redownload"
			} else {
				strategy = "continue"
			}
		}
		resTask, err = qc.engine.ResolveDuplicate(ctx, active.DuplicateTask.ID, strategy, sub.Directory, sub.Filename, sub.MaxConn)
		if err != nil {
			return nil, err
		}
	} else if active.PreDownloadTaskID != "" {
		resTask, err = qc.engine.ConfirmPreDownload(ctx, active.PreDownloadTaskID, sub.Directory, sub.Filename, sub.MaxConn)
		if err != nil {
			return nil, err
		}
	} else {
		resTask, err = qc.engine.AddTaskWithHeaders(ctx, sub.URL, sub.Directory, sub.Filename, sub.MaxConn, active.Headers)
		if err != nil {
			return nil, err
		}
	}

	// Remove confirmed item from queue
	qc.items = append(qc.items[:qc.activeIndex], qc.items[qc.activeIndex+1:]...)
	if qc.activeIndex >= len(qc.items) && len(qc.items) > 0 {
		qc.activeIndex = len(qc.items) - 1
	}
	qc.advanceQueueLocked()

	return resTask, nil
}

// CancelCurrent cancels the active file info item and advances the queue.
func (qc *QueueController) CancelCurrent(ctx context.Context) error {
	qc.mu.Lock()
	defer qc.mu.Unlock()

	if len(qc.items) == 0 {
		if qc.windowView != nil {
			qc.windowView.Hide()
		}
		return nil
	}

	if qc.activeIndex >= len(qc.items) {
		qc.activeIndex = 0
	}
	active := qc.items[qc.activeIndex]

	if active.PreDownloadTaskID != "" {
		_ = qc.engine.CancelPreDownload(ctx, active.PreDownloadTaskID)
	}

	// Remove cancelled item from queue
	qc.items = append(qc.items[:qc.activeIndex], qc.items[qc.activeIndex+1:]...)
	if qc.activeIndex >= len(qc.items) && len(qc.items) > 0 {
		qc.activeIndex = len(qc.items) - 1
	}
	qc.advanceQueueLocked()

	return nil
}

func (qc *QueueController) advanceQueueLocked() {
	if len(qc.items) > 0 {
		qc.updateQueueNumbersLocked()
		if qc.activeIndex >= len(qc.items) {
			qc.activeIndex = len(qc.items) - 1
		}
		nextItem := qc.items[qc.activeIndex]
		if qc.windowView != nil {
			qc.windowView.Emit("fileinfo:next", nextItem)
		}
	} else {
		qc.activeIndex = 0
		if qc.windowView != nil {
			qc.windowView.Hide()
		}
	}
}
