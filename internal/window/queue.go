// Package window manages desktop window lifecycle, single-instance fileinfo queue,
// and cross-window coordination.
package window

import (
	"context"
	"errors"
	"fmt"
	"sheep-get/internal/config"
	"sheep-get/internal/duplicate"
	"sheep-get/internal/engine"
	"sheep-get/internal/events"
	"sheep-get/internal/task"
	"sync"
	"time"
)

// ErrDuplicateChoiceRequired 表示这次提交没有携带动作，而当前局面必须由用户先选定一项
// （目标位置已有成品文件，且策略没有给出默认动作）。
var ErrDuplicateChoiceRequired = errors.New("duplicate action required before submitting")

// ErrStaleFileInfoSubmission 表示这次提交针对的已经不是窗口当前显示的任务。
// 提交只作用于当前项，界面重复提交时不能让上一项的落点作用到下一项上。
var ErrStaleFileInfoSubmission = errors.New("submission targets a task that is no longer active")

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
	ReuseExistingFile(ctx context.Context, taskID, targetDir, targetFilename string) (*task.Task, error)
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
	ID                string     `json:"id"`
	URL               string     `json:"url"`
	Filename          string     `json:"filename"`
	SuggestedFilename string     `json:"suggestedFilename"`
	Directory         string     `json:"directory"`
	CategoryID        string     `json:"categoryId,omitempty"`
	TotalBytes        int64      `json:"totalBytes"`
	MimeType          string     `json:"mimeType"`
	Resumable         bool       `json:"resumable"`
	MaxConn           int        `json:"maxConn"`
	PreDownload       bool       `json:"preDownload"`
	PreDownloadTaskID string     `json:"preDownloadTaskId,omitempty"`
	FileConflict      bool       `json:"fileConflict"`
	DuplicateTask     *task.Task `json:"duplicateTask,omitempty"`
	// DuplicateDecision 是这次重复的裁决结果：界面只渲染它给出的选项与默认项。
	// 策略本身不下发给窗口，避免窗口把它当成第二份规则来源。
	DuplicateDecision duplicate.Decision `json:"duplicateDecision"`
	QueueIndex        int                `json:"queueIndex"`
	QueueTotal        int                `json:"queueTotal"`
	Headers           map[string]string  `json:"headers,omitempty"`
}

// FileInfoSubmission represents user confirmation from the FileInfo window.
type FileInfoSubmission struct {
	RequestID   string `json:"requestId"`
	URL         string `json:"url"`
	Filename    string `json:"filename"`
	Directory   string `json:"directory"`
	MaxConn     int    `json:"maxConn"`
	PreDownload bool   `json:"preDownload"`
	// Action 是用户为这次重复选定的动作，取自文件信息窗口收到的裁决选项；
	// 为空时由后端按裁决的默认动作执行。
	Action      duplicate.Action `json:"action,omitempty"`
	ReuseTaskID string           `json:"reuseTaskId,omitempty"`
}

// QueueController coordinates the single-instance FileInfo window and its FIFO queue.
type QueueController struct {
	mu          sync.Mutex
	engine      DownloadEngine
	settings    SettingsProvider
	windowView  WindowView
	items       []*FileInfoItem
	activeIndex int
	// requestSeq 单调递增，保证每一项的 ID 互不相同：界面按 ID 分项保存草稿、按 ID 路由探测结果，
	// 时间戳在同一刻会重复，不能当唯一标识用。
	requestSeq      uint64
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
		qc.windowView.Emit(events.FileInfoNext, activeItem)
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

	filename := req.Filename
	if filename == "" && req.URL != "" {
		filename = engine.URLFilename(req.URL)
	}
	if filename == "" && dupTask != nil && dupTask.Filename != "" {
		filename = dupTask.Filename
	}
	if filename == "" && req.URL != "" {
		filename = engine.DefaultFilename
	}

	dir := req.Directory
	categoryID := ""
	if filename != "" {
		// 分类只决定目录；手动指定目录时仍然给出命中分类，供界面展示与"记住分类"使用。
		cat, resolvedDir := activeSettings.Download.ResolveDestination(filename)
		categoryID = cat.ID
		if dir == "" {
			dir = resolvedDir
		}
	} else if dir == "" {
		dir = activeSettings.Download.DefaultDirectory
	}

	conflict := false
	suggested := ""
	if filename != "" {
		conflict, suggested = engine.CheckFileConflict(dir, filename)
		if copyName, err := qc.engine.NumberedCopyName(ctx, dir, filename); err == nil && copyName != "" {
			suggested = copyName
		}
	}
	// 重复链接该给哪些动作、默认哪个，只由后端裁决一次；界面不再推导策略含义。
	decision := duplicate.Decide(duplicate.Facts{
		Policy:              policy,
		HasHistory:          dupTask != nil,
		HistoryCompleted:    dupTask != nil && dupTask.Status == task.StatusCompleted,
		DestinationOccupied: engine.DestinationOccupied(dir, filename, dupTask),
	})

	// 只有裁决确实要做「序号副本」时才预置编号名称。目标位置没有成品文件时既没有要避让的
	// 文件、动作也不是副本，编号名称只会让界面显示的名字与实际落点不符。
	if decision.Default == duplicate.ActionCopy {
		filename = suggested
		conflict = false
	}

	// 「跳过并显示完成」策略下立即唤起完成区域，且早于文件信息窗口夺焦，保持既有焦点顺序。
	if decision.ShowCompleted && qc.onShowCompleted != nil {
		qc.onShowCompleted(dupTask.ID)
	}

	qc.requestSeq++
	item := &FileInfoItem{
		ID:                fmt.Sprintf("req_%d", qc.requestSeq),
		URL:               req.URL,
		Filename:          filename,
		SuggestedFilename: suggested,
		Directory:         dir,
		CategoryID:        categoryID,
		TotalBytes:        -1,
		MimeType:          "",
		Resumable:         false,
		MaxConn:           maxConn,
		PreDownload:       preDownload,
		FileConflict:      conflict,
		DuplicateTask:     dupTask,
		DuplicateDecision: decision,
		Headers:           req.Headers,
	}

	qc.items = append(qc.items, item)
	qc.updateQueueNumbersLocked()

	// Show window and bring to focus immediately
	if qc.windowView != nil {
		qc.windowView.Show()
		qc.windowView.Focus()
		if len(qc.items) == 1 {
			qc.windowView.Emit(events.FileInfoNext, item)
		} else {
			// If already open with an item, update it immediately to the latest manual click so the user immediately sees response
			qc.windowView.Emit(events.FileInfoQueueUpdated, map[string]int{
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
		if probe.Filename != "" && (targetItem.Filename == engine.DefaultFilename || targetItem.Filename == engine.URLFilename(reqURL)) {
			targetItem.Filename = probe.Filename
		}
	}

	conflict, suggested := engine.CheckFileConflict(dir, targetItem.Filename)
	if copyName, err := qc.engine.NumberedCopyName(context.Background(), dir, targetItem.Filename); err == nil && copyName != "" {
		suggested = copyName
	}
	targetItem.SuggestedFilename = suggested
	targetItem.FileConflict = conflict

	// 探测补齐了历史任务与目标位置的现状，重新裁决一次，使界面刷新后的选项与事实一致。
	decision := duplicate.Decide(duplicate.Facts{
		Policy:              policy,
		HasHistory:          targetItem.DuplicateTask != nil,
		HistoryCompleted:    targetItem.DuplicateTask != nil && targetItem.DuplicateTask.Status == task.StatusCompleted,
		DestinationOccupied: engine.DestinationOccupied(dir, targetItem.Filename, targetItem.DuplicateTask),
	})
	targetItem.DuplicateDecision = decision

	if decision.Default == duplicate.ActionCopy {
		targetItem.Filename = suggested
		targetItem.FileConflict = false
	}

	if decision.ShowCompleted && qc.onShowCompleted != nil {
		qc.onShowCompleted(targetItem.DuplicateTask.ID)
	}

	if preDownload && !targetItem.FileConflict && targetItem.DuplicateTask == nil && targetItem.PreDownloadTaskID == "" {
		if preTask, err := qc.engine.StartPreDownloadWithHeaders(context.Background(), reqURL, dir, targetItem.Filename, maxConn, headers); err == nil && preTask != nil {
			targetItem.PreDownloadTaskID = preTask.ID
		}
	}

	if qc.windowView != nil {
		qc.windowView.Emit(events.FileInfoUpdated, targetItem)
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
	if sub.RequestID != active.ID {
		return nil, ErrStaleFileInfoSubmission
	}

	// If active.DuplicateTask is nil, check if sub.URL matches an existing task
	if active.DuplicateTask == nil && sub.URL != "" {
		probe, _ := qc.engine.ProbeURL(ctx, sub.URL)
		if probe != nil && probe.DuplicateTask != nil {
			active.DuplicateTask = probe.DuplicateTask
		}
	}
	// 用户可能改过目录或文件名，因此按最终落点重新裁决，而不是沿用登记时的结果。
	// 策略取当前设置：它只影响默认动作，而用户已选定动作时默认动作不会被使用。
	decision := duplicate.Decide(duplicate.Facts{
		Policy:              qc.settings.Get().Download.DuplicateURLPolicy,
		HasHistory:          active.DuplicateTask != nil,
		HistoryCompleted:    active.DuplicateTask != nil && active.DuplicateTask.Status == task.StatusCompleted,
		DestinationOccupied: engine.DestinationOccupied(sub.Directory, sub.Filename, active.DuplicateTask),
	})
	active.DuplicateDecision = decision

	action := sub.Action
	if action == "" {
		action = decision.Default
	}

	var (
		resTask *task.Task
		err     error
	)

	switch {
	case action == duplicate.ActionReuse && sub.ReuseTaskID != "":
		resTask, err = qc.engine.ReuseExistingFile(ctx, sub.ReuseTaskID, sub.Directory, sub.Filename)
	case active.DuplicateTask != nil:
		if action == "" {
			// 局面需要用户先决定怎么处理，界面不能在没有选择的情况下提交。
			return nil, ErrDuplicateChoiceRequired
		}
		resTask, err = qc.engine.ResolveDuplicate(ctx, active.DuplicateTask.ID, string(action), sub.Directory, sub.Filename, sub.MaxConn)
	case active.PreDownloadTaskID != "":
		resTask, err = qc.engine.ConfirmPreDownload(ctx, active.PreDownloadTaskID, sub.Directory, sub.Filename, sub.MaxConn)
	default:
		resTask, err = qc.engine.AddTaskWithHeaders(ctx, sub.URL, sub.Directory, sub.Filename, sub.MaxConn, active.Headers)
	}
	if err != nil {
		return nil, err
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
			qc.windowView.Emit(events.FileInfoNext, nextItem)
		}
	} else {
		qc.activeIndex = 0
		if qc.windowView != nil {
			qc.windowView.Hide()
		}
	}
}
