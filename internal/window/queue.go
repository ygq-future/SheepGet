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

// ErrSubmitInProgress 表示这一项正在提交中（引擎里那次调用还没回来）。此时取消只会让用户
// 以为取消了、实际却会建出任务，因此明确拒绝。这条文案会原样显示在文件信息窗口里
// （与引擎里那些用户可见的错误一致），所以用中文。
var ErrSubmitInProgress = errors.New("正在提交这一次下载，暂时无法取消")

// WindowView abstracts a native OS/Wails window.
//
// 它的方法可能在主线程被占住时长时间不返回（Wails 的窗口调用会同步派发回主线程等待执行），
// 因此调用方不得在持有队列锁时调用它们——一律经由 windowActions 收集、在锁外执行。
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

	// 下面三个字段描述这一项自己有没有「引擎调用在路上」，不下发给界面：
	// submitting 表示提交的引擎调用还没回来；preDownloadStarting 与其完成信号一起表示
	// 预下载正在启动（见 asyncProbeItem）。两者存在的理由都是同一个——引擎调用不能放在
	// 队列锁里，于是锁一放开，这些中间状态就必须能被其他操作看见。
	submitting           bool
	preDownloadStarting  bool
	preDownloadStartedCh chan struct{}
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
	mu         sync.Mutex
	engine     DownloadEngine
	settings   SettingsProvider
	windowView WindowView
	// runOps 决定窗口副作用在哪里执行；生产实现异步执行，不会阻塞调用方（见 windowops.go）。
	runOps      windowOps
	items       []*FileInfoItem
	activeIndex int
	// requestSeq 单调递增，保证每一项的 ID 互不相同：界面按 ID 分项保存草稿、按 ID 路由探测结果，
	// 时间戳在同一刻会重复，不能当唯一标识用。
	requestSeq      uint64
	onShowCompleted func(taskID string)
}

func NewQueueController(eng DownloadEngine, settings SettingsProvider, winView WindowView) *QueueController {
	return newQueueController(eng, settings, winView, newAsyncOps())
}

// newQueueController 允许调用方决定窗口副作用在哪里执行。生产代码只用 NewQueueController
// （异步执行）；测试用同步执行器，以便在每次操作后确定地断言窗口调用。
func newQueueController(eng DownloadEngine, settings SettingsProvider, winView WindowView, runOps windowOps) *QueueController {
	return &QueueController{
		engine:      eng,
		settings:    settings,
		windowView:  winView,
		runOps:      runOps,
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
	actions := newWindowActions(qc.windowView)
	// defer 后进先出：先注册窗口副作用、后注册解锁，于是解锁一定先于副作用执行。
	defer actions.run(qc.runOps)
	defer qc.mu.Unlock()

	if index < 0 || index >= len(qc.items) {
		return nil, fmt.Errorf("queue index out of range: %d", index)
	}

	qc.activeIndex = index
	qc.updateQueueNumbersLocked()
	activeItem := qc.items[qc.activeIndex]
	activeCopy := *activeItem

	actions.emit(events.FileInfoNext, &activeCopy)

	return &activeCopy, nil
}

func (qc *QueueController) updateQueueNumbersLocked() {
	total := len(qc.items)
	for i, item := range qc.items {
		item.QueueIndex = i + 1
		item.QueueTotal = total
	}
}

// indexOfLocked 返回某一项在队列里的位置，找不到（已被取消或已提交）返回 -1。
func (qc *QueueController) indexOfLocked(id string) int {
	for i, item := range qc.items {
		if item.ID == id {
			return i
		}
	}
	return -1
}

// itemByIDLocked 按 ID 取队列项；不在队列里时返回 nil。
func (qc *QueueController) itemByIDLocked(id string) *FileInfoItem {
	if idx := qc.indexOfLocked(id); idx >= 0 {
		return qc.items[idx]
	}
	return nil
}

// Enqueue adds a download request to the window queue or handles duplicate skip policy.
func (qc *QueueController) Enqueue(ctx context.Context, req DownloadRequest) (*DownloadResponse, error) {
	qc.mu.Lock()
	actions := newWindowActions(qc.windowView)
	// defer 后进先出：先注册窗口副作用、后注册解锁，于是解锁一定先于副作用执行——窗口调用
	// 绝不发生在锁内，交接响应也不必等窗口显示完成。
	defer actions.run(qc.runOps)
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
	// 目录是这份请求自己给的、还是按分类裁出来的，决定探测补齐真名后要不要跟着重算落点。
	dirFromCategory := req.Directory == ""
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
		showCompleted := qc.onShowCompleted
		completedTaskID := dupTask.ID
		actions.add(func() { showCompleted(completedTaskID) })
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

	// 立刻显示窗口并置前，界面同时拿到本次登记的这一项。
	actions.show()
	actions.focus()
	if len(qc.items) == 1 {
		itemCopy := *item
		actions.emit(events.FileInfoNext, &itemCopy)
	} else {
		// If already open with an item, update it immediately to the latest manual click so the user immediately sees response
		actions.emit(events.FileInfoQueueUpdated, map[string]int{
			"index": qc.items[0].QueueIndex,
			"total": qc.items[0].QueueTotal,
		})
	}

	// Trigger background async probe if URL is present
	if req.URL != "" {
		itemID := item.ID
		reqURL := req.URL
		reqHeaders := req.Headers
		go qc.asyncProbeItem(itemID, reqURL, reqHeaders, dir, dirFromCategory, policy, preDownload, maxConn)
	}

	return &DownloadResponse{
		Handled:   true,
		Action:    "enqueued",
		RequestID: item.ID,
	}, nil
}

func (qc *QueueController) asyncProbeItem(itemID, reqURL string, headers map[string]string, dir string, dirFromCategory bool, policy config.DuplicateURLPolicy, preDownload bool, maxConn int) {
	probeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	probe, err := qc.engine.ProbeURL(probeCtx, reqURL)
	if err != nil && probe == nil {
		return
	}

	start := qc.applyProbeResult(itemID, reqURL, headers, dir, dirFromCategory, policy, preDownload, maxConn, probe)
	if start == nil {
		return
	}

	// 启动预下载同样会联网（StartPreDownloadWithHeaders 内部要探测 URL），因此和上面的探测
	// 一样必须在锁外：锁被它握住的这段时间，队列的每个操作都要排队。
	preTask, preErr := qc.engine.StartPreDownloadWithHeaders(context.Background(), start.url, start.dir, start.filename, start.maxConn, start.headers)
	if preErr != nil {
		preTask = nil
	}
	if qc.finishPreDownloadStart(start, preTask) && preTask != nil {
		// 启动期间这一项被取消了：把刚启动的任务收掉，否则它会一直跑在后台，
		// 变成一份用户从未确认过的下载。
		_ = qc.engine.CancelPreDownload(context.Background(), preTask.ID)
	}
}

// preDownloadStart 是一次待启动的预下载：它在锁内被定下来，在锁外执行。
type preDownloadStart struct {
	item     *FileInfoItem
	url      string
	dir      string
	filename string
	maxConn  int
	headers  map[string]string
}

// applyProbeResult 把探测结果落进队列项（锁内），并返回需要启动的预下载。
// 不需要启动、或这一项已经不在队列里时返回 nil。它不调用会联网的引擎方法。
func (qc *QueueController) applyProbeResult(itemID, reqURL string, headers map[string]string, dir string, dirFromCategory bool, policy config.DuplicateURLPolicy, preDownload bool, maxConn int, probe *engine.ProbeResult) *preDownloadStart {
	qc.mu.Lock()
	actions := newWindowActions(qc.windowView)
	// 同 Enqueue：解锁先于窗口副作用执行。
	defer actions.run(qc.runOps)
	defer qc.mu.Unlock()

	targetItem := qc.itemByIDLocked(itemID)
	if targetItem == nil {
		return nil
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
			// 真名补齐后必须按它重算命中分类与保存目录：登记时只能拿 URL 猜名字，而真实后缀
			// 常常只存在于响应头（例如 GitHub 资产链接的路径里只有一个 GUID，没有后缀）。
			// 分类规则只在后端裁决一次，这里复用同一处实现；用户自己指定过目录时不改动它。
			if dirFromCategory {
				cat, resolvedDir := qc.settings.Get().Download.ResolveDestination(targetItem.Filename)
				targetItem.CategoryID = cat.ID
				targetItem.Directory = resolvedDir
				dir = resolvedDir
			}
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
		showCompleted := qc.onShowCompleted
		completedTaskID := targetItem.DuplicateTask.ID
		actions.add(func() { showCompleted(completedTaskID) })
	}

	var start *preDownloadStart
	if preDownload && !targetItem.FileConflict && targetItem.DuplicateTask == nil && targetItem.PreDownloadTaskID == "" && !targetItem.preDownloadStarting {
		// 只在这里占位，真正启动放到锁外。占位让提交知道「预下载正在启动」：那期间
		// PreDownloadTaskID 还是空的，直接提交会在同一链接上建出第二份任务。
		targetItem.preDownloadStarting = true
		targetItem.preDownloadStartedCh = make(chan struct{})
		start = &preDownloadStart{
			item:     targetItem,
			url:      reqURL,
			dir:      dir,
			filename: targetItem.Filename,
			maxConn:  maxConn,
			headers:  headers,
		}
	}

	itemCopy := *targetItem
	actions.emit(events.FileInfoUpdated, &itemCopy)

	return start
}

// finishPreDownloadStart 收尾一次预下载启动：记录任务 ID，并解除「正在启动」这个状态。
// 返回 true 表示这一项在启动期间已经不在队列里（被取消或已提交），刚启动的任务需要被收掉。
func (qc *QueueController) finishPreDownloadStart(start *preDownloadStart, preTask *task.Task) bool {
	qc.mu.Lock()
	actions := newWindowActions(qc.windowView)
	defer actions.run(qc.runOps)
	defer qc.mu.Unlock()

	inQueue := qc.indexOfLocked(start.item.ID) >= 0
	start.item.preDownloadStarting = false
	if ch := start.item.preDownloadStartedCh; ch != nil {
		close(ch)
		start.item.preDownloadStartedCh = nil
	}

	if preTask == nil {
		return false
	}
	if !inQueue {
		return true
	}

	start.item.PreDownloadTaskID = preTask.ID
	itemCopy := *start.item
	actions.emit(events.FileInfoUpdated, &itemCopy)
	return false
}

// Submit confirms the active file info item with final user choices.
//
// 分成三段：锁内取事实与占位，锁外调引擎（会联网），再回锁内落地。
// 中间那段不能放在锁里——真实引擎的探测会重试三次、还有一次 HEAD 兜底，HTTP 客户端没有超时，
// 锁被它握住时读当前项、取消、下一次交接全都排在后面，交接会在 2.5 秒上限处静默失败。
func (qc *QueueController) Submit(ctx context.Context, sub FileInfoSubmission) (*task.Task, error) {
	plan, err := qc.beginSubmit(ctx, sub)
	if err != nil {
		return nil, err
	}

	resTask, err := qc.runSubmit(ctx, plan)

	qc.mu.Lock()
	actions := newWindowActions(qc.windowView)
	// 同 Enqueue：解锁先于窗口副作用执行，界面推进不必等确认动作里的其他调用返回。
	defer actions.run(qc.runOps)
	defer qc.mu.Unlock()

	plan.apply()
	plan.item.submitting = false

	if err != nil {
		return nil, err
	}

	// 按 ID 摘除：提交期间用户可能已经切到别的项上，按位置删会删错人。
	if idx := qc.indexOfLocked(plan.item.ID); idx >= 0 {
		qc.items = append(qc.items[:idx], qc.items[idx+1:]...)
		if qc.activeIndex > idx {
			qc.activeIndex--
		}
		qc.advanceQueueLocked(actions)
	}

	return resTask, nil
}

// submitPlan 是提交穿过锁的一次快照：锁内取事实，锁外调引擎，回来后按 ID 落地。
type submitPlan struct {
	item      *FileInfoItem
	url       string
	directory string
	filename  string
	maxConn   int
	headers   map[string]string
	policy    config.DuplicateURLPolicy
	sub       FileInfoSubmission
	preTaskID string
	// probeDuplicate 表示登记时没查到同链接历史任务，提交前再确认一次。
	probeDuplicate bool

	duplicateTask *task.Task
	decision      duplicate.Decision
}

// apply 把锁外算出的裁决写回这一项，界面刷新时看到的是与事实一致的结果。
func (p *submitPlan) apply() {
	if p.duplicateTask != nil {
		p.item.DuplicateTask = p.duplicateTask
	}
	p.item.DuplicateDecision = p.decision
}

// beginSubmit 在锁内确认这一项可以提交，标出「提交在路上」，并取走这次提交需要的事实。
func (qc *QueueController) beginSubmit(ctx context.Context, sub FileInfoSubmission) (*submitPlan, error) {
	for {
		qc.mu.Lock()

		if len(qc.items) == 0 {
			qc.mu.Unlock()
			return nil, fmt.Errorf("no active file info request")
		}
		if qc.activeIndex >= len(qc.items) {
			qc.activeIndex = 0
		}
		active := qc.items[qc.activeIndex]
		if sub.RequestID != active.ID {
			qc.mu.Unlock()
			return nil, ErrStaleFileInfoSubmission
		}
		if active.preDownloadStarting {
			// 预下载正在启动，这一项的 PreDownloadTaskID 还是空的：此刻提交会走「新建任务」，
			// 在同一链接上建出第二份。等它落地再重新读一遍这一项。
			started := active.preDownloadStartedCh
			qc.mu.Unlock()
			select {
			case <-started:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if active.submitting {
			qc.mu.Unlock()
			return nil, ErrSubmitInProgress
		}
		active.submitting = true

		plan := &submitPlan{
			item:           active,
			url:            sub.URL,
			directory:      sub.Directory,
			filename:       sub.Filename,
			maxConn:        sub.MaxConn,
			headers:        active.Headers,
			policy:         qc.settings.Get().Download.DuplicateURLPolicy,
			sub:            sub,
			preTaskID:      active.PreDownloadTaskID,
			probeDuplicate: active.DuplicateTask == nil && sub.URL != "",
			duplicateTask:  active.DuplicateTask,
		}
		qc.mu.Unlock()
		return plan, nil
	}
}

// runSubmit 在锁外完成这次提交的引擎调用；它只读快照，不碰队列状态。
func (qc *QueueController) runSubmit(ctx context.Context, plan *submitPlan) (*task.Task, error) {
	if plan.probeDuplicate {
		// 登记时没查到同链接历史任务，提交前再确认一次：用户可能在这中间又下载过同一个链接。
		if probe, _ := qc.engine.ProbeURL(ctx, plan.url); probe != nil && probe.DuplicateTask != nil {
			plan.duplicateTask = probe.DuplicateTask
		}
	}

	// 用户可能改过目录或文件名，因此按最终落点重新裁决，而不是沿用登记时的结果。
	// 策略取当前设置：它只影响默认动作，而用户已选定动作时默认动作不会被使用。
	plan.decision = duplicate.Decide(duplicate.Facts{
		Policy:              plan.policy,
		HasHistory:          plan.duplicateTask != nil,
		HistoryCompleted:    plan.duplicateTask != nil && plan.duplicateTask.Status == task.StatusCompleted,
		DestinationOccupied: engine.DestinationOccupied(plan.directory, plan.filename, plan.duplicateTask),
	})

	action := plan.sub.Action
	if action == "" {
		action = plan.decision.Default
	}

	switch {
	case action == duplicate.ActionReuse && plan.sub.ReuseTaskID != "":
		return qc.engine.ReuseExistingFile(ctx, plan.sub.ReuseTaskID, plan.directory, plan.filename)
	case plan.duplicateTask != nil:
		if action == "" {
			// 局面需要用户先决定怎么处理，界面不能在没有选择的情况下提交。
			return nil, ErrDuplicateChoiceRequired
		}
		return qc.engine.ResolveDuplicate(ctx, plan.duplicateTask.ID, string(action), plan.directory, plan.filename, plan.maxConn)
	case plan.preTaskID != "":
		return qc.engine.ConfirmPreDownload(ctx, plan.preTaskID, plan.directory, plan.filename, plan.maxConn)
	default:
		return qc.engine.AddTaskWithHeaders(ctx, plan.url, plan.directory, plan.filename, plan.maxConn, plan.headers)
	}
}

// CancelCurrent cancels the active file info item and advances the queue.
func (qc *QueueController) CancelCurrent(ctx context.Context) error {
	qc.mu.Lock()
	actions := newWindowActions(qc.windowView)
	defer actions.run(qc.runOps)
	defer qc.mu.Unlock()

	if len(qc.items) == 0 {
		actions.hide()
		return nil
	}

	if qc.activeIndex >= len(qc.items) {
		qc.activeIndex = 0
	}
	active := qc.items[qc.activeIndex]

	if active.submitting {
		// 提交的引擎调用已经在路上：这时删掉这一项，用户以为取消了，任务却已经建出来。
		// 明确拒绝，由界面提示「正在提交」，而不是把这次取消落到队列里的下一项头上。
		return ErrSubmitInProgress
	}

	if active.PreDownloadTaskID != "" {
		_ = qc.engine.CancelPreDownload(ctx, active.PreDownloadTaskID)
	}

	// Remove cancelled item from queue
	qc.items = append(qc.items[:qc.activeIndex], qc.items[qc.activeIndex+1:]...)
	if qc.activeIndex >= len(qc.items) && len(qc.items) > 0 {
		qc.activeIndex = len(qc.items) - 1
	}
	qc.advanceQueueLocked(actions)

	return nil
}

func (qc *QueueController) advanceQueueLocked(actions *windowActions) {
	if len(qc.items) > 0 {
		qc.updateQueueNumbersLocked()
		if qc.activeIndex >= len(qc.items) {
			qc.activeIndex = len(qc.items) - 1
		}
		nextItem := *qc.items[qc.activeIndex]
		actions.emit(events.FileInfoNext, &nextItem)
	} else {
		qc.activeIndex = 0
		actions.hide()
	}
}
