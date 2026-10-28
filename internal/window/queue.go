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
	"sheep-get/internal/intake"
	"sheep-get/internal/protocol"
	"sheep-get/internal/task"
	"strings"
	"sync"
	"time"
)

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
	// VariantURI 是浏览器扩展在悬浮条上已经选好的清晰度（清单里的一个 EXT-X-STREAM-INF 地址）。
	// 有它时这一项不必再问一次清晰度：多清晰度的选择已经发生在进入本窗口之前。
	VariantURI string `json:"variantUri,omitempty"`
	PageURL    string `json:"pageUrl,omitempty"`
}

// DownloadResponse is the result of enqueuing or dispatching a download request.
type DownloadResponse struct {
	Handled   bool   `json:"handled"`
	Action    string `json:"action"` // "enqueued", "skip_show_completed", etc.
	RequestID string `json:"requestId,omitempty"`
	TaskID    string `json:"taskId,omitempty"`
}

// FileInfoItem 是文件信息窗口里的一项：下载目标（名字、落点、分类、探测事实与裁决）加上
// 这一项自己的生命周期状态。目标由 intake 决定，窗口不再维护第二份名字或落点。
type FileInfoItem struct {
	intake.Target

	ID string `json:"id"`
	// PreDownload 是这一项要不要在确认前先传起来；PreDownloadTaskID 是已经起好的那条任务。
	PreDownload       bool   `json:"preDownload"`
	PreDownloadTaskID string `json:"preDownloadTaskId,omitempty"`
	QueueIndex        int    `json:"queueIndex"`
	QueueTotal        int    `json:"queueTotal"`
	// Probing 表示登记时那次探测还在路上，界面据此显示加载态。
	Probing bool `json:"probing"`

	// 下面这些字段描述这一项自己有没有「引擎调用在路上」，不下发给界面：
	// submitting 表示提交的引擎调用还没回来；preDownloadStarting 与其完成信号一起表示
	// 预下载正在启动（见 StartPreDownload）。两者存在的理由都是同一个——引擎调用不能放在
	// 队列锁里，于是锁一放开，这些中间状态就必须能被其他操作看见。
	submitting           bool
	preDownloadStarting  bool
	preDownloadStartedCh chan struct{}
	skipConfirm          bool

	// probeDone 在探测结束时关闭，探测还在路上时提交靠在它上面等（见 beginSubmitItem）。
	probeDone   chan struct{}
	probeCancel context.CancelFunc
}

// FileInfoSubmission represents user confirmation from the FileInfo window.
type FileInfoSubmission struct {
	RequestID   string `json:"requestId"`
	URL         string `json:"url"`
	Filename    string `json:"filename"`
	Directory   string `json:"directory"`
	CategoryID  string `json:"categoryId,omitempty"`
	MaxConn     int    `json:"maxConn"`
	PreDownload bool   `json:"preDownload"`
	// Action 是用户为这次重复选定的动作，取自文件信息窗口收到的裁决选项；
	// 为空时由后端按裁决的默认动作执行。
	Action      duplicate.Action `json:"action,omitempty"`
	ReuseTaskID string           `json:"reuseTaskId,omitempty"`
}

// QueueController coordinates the single-instance FileInfo window and its FIFO queue.
//
// 它只管窗口里的顺序与每一项的中间状态：从链接到任务的判断（命名、HLS、落点、占用、
// 裁决、提交）全在 intake 后面，窗口不再自己拼名字或直接调引擎。
type QueueController struct {
	mu         sync.Mutex
	intake     *intake.Intake
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
	onSubmitTask    func(t *task.Task)
}

func NewQueueController(eng intake.Engine, settings SettingsProvider, winView WindowView) *QueueController {
	return newQueueController(eng, settings, winView, newAsyncOps())
}

// newQueueController 允许调用方决定窗口副作用在哪里执行。生产代码只用 NewQueueController
// （异步执行）；测试用同步执行器，以便在每次操作后确定地断言窗口调用。
func newQueueController(eng intake.Engine, settings SettingsProvider, winView WindowView, runOps windowOps) *QueueController {
	return &QueueController{
		intake:      intake.New(eng, settings),
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

// SetOnSubmitTask registers a hook to be triggered when a task is successfully submitted.
func (qc *QueueController) SetOnSubmitTask(fn func(t *task.Task)) {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	qc.onSubmitTask = fn
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

	actions.emitFunc(protocol.EventFileInfoNext, func() any {
		return qc.snapshotItem(activeCopy.ID, &activeCopy)
	})

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

// snapshotItem 在窗口副作用真正执行那一刻，按 ID 取一次该队列项的最新快照。
// 窗口创建/显示（Show）可能耗时数百毫秒，期间后台探测往往已经落地；发送实时快照能避免用
// 登记那一刻的旧状态（Probing: true）把前端已经收到的探测完成状态重新覆盖回转圈圈。
func (qc *QueueController) snapshotItem(id string, fallback *FileInfoItem) *FileInfoItem {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	if it := qc.itemByIDLocked(id); it != nil {
		cp := *it
		return &cp
	}
	return fallback
}

// ReservedNames 返回「队列已经发出的名字」判定器，不含 exceptID 那一项。
// 调用方是窗口之外的地方（例如 App 的冲突绑定），因此这里自己加锁。
func (qc *QueueController) ReservedNames(exceptID string) engine.Reserved {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	return qc.reservedNamesLocked(exceptID)
}

// reservedNamesLocked 把队列里其它项的目标名字取成一份快照判定器。调用方必须持有队列锁。
//
// 两项名字都要算占用：Filename 是这一项将要落下的名字，SuggestedFilename 是它留给「序号
// 副本」动作的备选名——两个都可能被提交。没有第二份来源时，两个排队项会建议同一个名字。
// 返回的是闭包捕获的快照，离开锁之后可以继续用（占用判定要在锁外逐个候选名字地问）。
func (qc *QueueController) reservedNamesLocked(exceptID string) engine.Reserved {
	type target struct{ dir, name string }
	taken := make([]target, 0, len(qc.items)*2)
	for _, item := range qc.items {
		if item == nil || item.ID == exceptID {
			continue
		}
		if item.Filename != "" {
			taken = append(taken, target{item.Directory, item.Filename})
		}
		if item.SuggestedFilename != "" && item.SuggestedFilename != item.Filename {
			taken = append(taken, target{item.Directory, item.SuggestedFilename})
		}
	}
	return func(dir, name string) bool {
		for _, t := range taken {
			if engine.SamePath(t.dir, dir) && engine.SameFilename(t.name, name) {
				return true
			}
		}
		return false
	}
}

// itemByIDLocked 按 ID 取队列项；不在队列里时返回 nil。
func (qc *QueueController) itemByIDLocked(id string) *FileInfoItem {
	if idx := qc.indexOfLocked(id); idx >= 0 {
		return qc.items[idx]
	}
	return nil
}

// Enqueue 把一次请求登记进文件信息窗口队列：建议名字、落点、占用与重复裁决由 intake 一次
// 给出，队列只负责顺序、探测的中间状态与窗口副作用。
func (qc *QueueController) Enqueue(ctx context.Context, req DownloadRequest) (*DownloadResponse, error) {
	qc.mu.Lock()
	actions := newWindowActions(qc.windowView)
	// defer 后进先出：先注册窗口副作用、后注册解锁，于是解锁一定先于副作用执行——窗口调用
	// 绝不发生在锁内，交接响应也不必等窗口显示完成。
	defer actions.run(qc.runOps)
	defer qc.mu.Unlock()

	activeSettings := qc.settings.Get()
	preDownload := activeSettings.Download.PreDownload
	if req.PreDownload != nil {
		preDownload = *req.PreDownload
	}

	target := qc.intake.Begin(ctx, intake.Request{
		URL:        req.URL,
		Headers:    req.Headers,
		Filename:   req.Filename,
		Directory:  req.Directory,
		PageURL:    req.PageURL,
		VariantURI: req.VariantURI,
		MaxConn:    req.MaxConn,
		Mode:       intake.ModeWindow,
	}, qc.reservedNamesLocked(""))

	// 「跳过并显示完成」策略下立即唤起完成区域，且早于文件信息窗口夺焦，保持既有焦点顺序。
	if target.DuplicateDecision.ShowCompleted && qc.onShowCompleted != nil {
		showCompleted := qc.onShowCompleted
		completedTaskID := target.DuplicateTask.ID
		actions.add(func() { showCompleted(completedTaskID) })
	}

	qc.requestSeq++
	item := &FileInfoItem{
		Target:      target,
		ID:          fmt.Sprintf("req_%d", qc.requestSeq),
		PreDownload: preDownload,
		Probing:     target.URL != "",
		skipConfirm: preDownload && activeSettings.Download.SkipFileInfoConfirm && target.URL != "",
	}
	// 有链接就一定会探测：探测的完成信号在这里就先挂上，提交才知道该等谁。
	var probeCtx context.Context
	if target.URL != "" {
		item.probeDone = make(chan struct{})
		probeCtx, item.probeCancel = context.WithTimeout(context.Background(), 15*time.Second)
	}

	qc.items = append(qc.items, item)
	qc.updateQueueNumbersLocked()

	// 只有不需要跳过确认时，才在登记时立刻显示窗口并置前；跳过确认的项先静默探测
	if !item.skipConfirm {
		actions.show()
		actions.focus()
	}
	if len(qc.items) == 1 {
		itemCopy := *item
		actions.emitFunc(protocol.EventFileInfoNext, func() any {
			return qc.snapshotItem(itemCopy.ID, &itemCopy)
		})
	} else {
		// If already open with an item, update it immediately to the latest manual click so the user immediately sees response
		actions.emit(protocol.EventFileInfoQueueUpdated, map[string]int{
			"index": qc.items[0].QueueIndex,
			"total": qc.items[0].QueueTotal,
		})
	}

	// Trigger background async probe if URL is present
	if target.URL != "" {
		go qc.asyncProbeItem(probeCtx, probeJob{itemID: item.ID, target: target})
	}

	return &DownloadResponse{
		Handled:   true,
		Action:    "enqueued",
		RequestID: item.ID,
	}, nil
}

// probeJob 是一次登记探测的全部输入：登记那一刻的目标快照。锁外探测只读这份副本，
// 探测期间不碰队列里的那一项。
type probeJob struct {
	itemID string
	target intake.Target
}

func (qc *QueueController) asyncProbeItem(probeCtx context.Context, job probeJob) {
	if probeCtx == nil {
		var cancel context.CancelFunc
		probeCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
	}
	// 探测带上这一项的请求上下文（Referer/Cookie），交接已经带来清晰度时顺手把它解析成
	// 这次下载的完整事实。两件事都在锁外。
	probe, probeErr := qc.intake.Probe(probeCtx, job.target)

	start := qc.applyProbeResult(job, probe, probeErr)
	if start != nil {
		qc.startPreDownload(start)
	}
	qc.checkAutoSubmit(job.itemID)
}

func (qc *QueueController) checkAutoSubmit(itemID string) {
	qc.mu.Lock()
	targetItem := qc.itemByIDLocked(itemID)
	if targetItem == nil || !targetItem.skipConfirm {
		qc.mu.Unlock()
		return
	}

	// 必须无探测错误，且探测结果非空
	if !targetItem.Probed() {
		qc.mu.Unlock()
		qc.fallbackToShow(itemID)
		return
	}

	// 如果是 HLS，必须已经选定清晰度
	if targetItem.NeedsVariantChoice() {
		qc.mu.Unlock()
		qc.fallbackToShow(itemID)
		return
	}

	// 如果有文件冲突（且没有被自动决议为序号副本等）
	if targetItem.FileConflict {
		qc.mu.Unlock()
		qc.fallbackToShow(itemID)
		return
	}

	// 重复链接处理：如果有重复任务，但策略没有默认动作（比如 prompt 每次询问），必须弹窗让用户选
	if targetItem.DuplicateTask != nil && targetItem.DuplicateDecision.Default == "" {
		qc.mu.Unlock()
		qc.fallbackToShow(itemID)
		return
	}

	// 如果重复策略是 ActionShowCompleted（跳过并查看完成记录）：
	// 已经在 applyProbeResult 中唤起了 onShowCompleted，不需要再发起下载，直接从队列中移除此项
	if targetItem.DuplicateDecision.Default == duplicate.ActionShowCompleted {
		actions := newWindowActions(qc.windowView)
		defer actions.run(qc.runOps)
		defer qc.mu.Unlock()

		if idx := qc.indexOfLocked(targetItem.ID); idx >= 0 {
			qc.items = append(qc.items[:idx], qc.items[idx+1:]...)
			if qc.activeIndex > idx {
				qc.activeIndex--
			}
			qc.advanceQueueLocked(actions)
		}
		return
	}

	sub := FileInfoSubmission{
		RequestID:  targetItem.ID,
		URL:        targetItem.URL,
		Directory:  targetItem.Directory,
		Filename:   targetItem.Filename,
		MaxConn:    targetItem.MaxConn,
		CategoryID: targetItem.CategoryID,
		Action:     targetItem.DuplicateDecision.Default,
	}
	qc.mu.Unlock()

	// 锁外调用 submitItem 完成自动转正
	if _, err := qc.submitItem(context.Background(), itemID, sub); err != nil {
		qc.fallbackToShow(itemID)
	}
}

func (qc *QueueController) fallbackToShow(itemID string) {
	qc.mu.Lock()
	targetItem := qc.itemByIDLocked(itemID)
	if targetItem == nil {
		qc.mu.Unlock()
		return
	}
	actions := newWindowActions(qc.windowView)
	defer actions.run(qc.runOps)
	defer qc.mu.Unlock()

	targetItem.skipConfirm = false
	if idx := qc.indexOfLocked(itemID); idx >= 0 {
		qc.activeIndex = idx
	}
	qc.updateQueueNumbersLocked()
	actions.show()
	actions.focus()
	itemCopy := *targetItem
	actions.emitFunc(protocol.EventFileInfoNext, func() any {
		return qc.snapshotItem(itemID, &itemCopy)
	})
}

func (qc *QueueController) startPreDownload(start *preDownloadStart) {
	if start == nil {
		return
	}
	// 启动预下载同样会建任务，因此和上面的探测一样必须在锁外：锁被它握住的这段时间，
	// 队列的每个操作都要排队。名字、落点与请求上下文都取自这一项的目标快照。
	preTask, preErr := qc.intake.StartPreDownload(context.Background(), start.target)
	if preErr != nil {
		preTask = nil
	}
	if qc.finishPreDownloadStart(start, preTask) && preTask != nil {
		// 启动期间这一项被取消了：把刚启动的任务收掉，否则它会一直跑在后台，
		// 变成一份用户从未确认过的下载。
		_ = qc.intake.CancelPreDownload(context.Background(), preTask.ID)
	}
}

// preDownloadStart 是一次待启动的预下载：它在锁内被定下来，在锁外执行。
type preDownloadStart struct {
	item   *FileInfoItem
	target intake.Target
}

// applyProbeResult 把探测结果落进队列项（锁内），并返回需要启动的预下载。
// 不需要启动、或这一项已经不在队列里时返回 nil。它不调用会联网的引擎方法。
func (qc *QueueController) applyProbeResult(job probeJob, probe *engine.ProbeResult, probeErr error) *preDownloadStart {
	qc.mu.Lock()
	actions := newWindowActions(qc.windowView)
	// 同 Enqueue：解锁先于窗口副作用执行。
	defer actions.run(qc.runOps)
	defer qc.mu.Unlock()

	targetItem := qc.itemByIDLocked(job.itemID)
	if targetItem == nil {
		return nil
	}

	// 先把这次探测本身记下来并放行等待者，再谈别的：提交正是靠它建任务，而预下载启动失败
	// 之类的原因都不该让提交一直等一个不会再有结果的信号。
	ready := qc.intake.ApplyProbe(context.Background(), &targetItem.Target, probe, probeErr, qc.reservedNamesLocked(targetItem.ID))
	targetItem.Probing = false
	if targetItem.probeCancel != nil {
		targetItem.probeCancel()
		targetItem.probeCancel = nil
	}
	if ch := targetItem.probeDone; ch != nil {
		close(ch)
		targetItem.probeDone = nil
	}

	if probe == nil {
		// 探测连兜底文件名都没给（引擎没做出任何结论）：这一项保持登记时的样子，
		// 也不去启动预下载，但必须通知前端结束 Probing 加载动画，避免界面一直转圈。
		itemCopy := *targetItem
		actions.emitFunc(protocol.EventFileInfoUpdated, func() any {
			return qc.snapshotItem(itemCopy.ID, &itemCopy)
		})
		return nil
	}

	if targetItem.DuplicateDecision.ShowCompleted && qc.onShowCompleted != nil {
		showCompleted := qc.onShowCompleted
		completedTaskID := targetItem.DuplicateTask.ID
		actions.add(func() { showCompleted(completedTaskID) })
	}
	// PreDownloadTaskID 还是空的：此刻直接提交会在同一链接上建出第二份任务。
	var start *preDownloadStart
	canPreDownload := ready && targetItem.PreDownload && targetItem.PreDownloadTaskID == "" && !targetItem.preDownloadStarting
	if canPreDownload {
		targetItem.preDownloadStarting = true
		targetItem.preDownloadStartedCh = make(chan struct{})
		start = &preDownloadStart{item: targetItem, target: targetItem.Target}
	}

	itemCopy := *targetItem
	actions.emitFunc(protocol.EventFileInfoUpdated, func() any {
		return qc.snapshotItem(itemCopy.ID, &itemCopy)
	})

	return start
}

// hlsSelection 是一次清晰度选择的快照：锁内取走，锁外用来联网。
type hlsSelection struct {
	// url 是这一次选择要解析的清单地址，由界面把当前链接显式传进来：手输链接时队列项里
	// 没有 URL（登记时是空的），不传就会拿到空地址。
	url    string
	target intake.Target
}

// SelectHLSVariant 记下用户在文件信息窗口里选定的清晰度，并把这一版的事实取回来。
//
// 解析会读清单，必要时还要为「显示一个真实大小」逐分片询问服务器，因此它和别处的引擎调用
// 一样在锁外执行，回来再按 ID 落地——期间用户可能已经切走或取消了这一项。
func (qc *QueueController) SelectHLSVariant(ctx context.Context, requestID, urlStr, variantURI string) error {
	job, err := qc.beginHLSSelection(ctx, requestID, urlStr)
	if err != nil {
		return err
	}

	selection, err := qc.intake.SelectVariant(ctx, job.target, job.url, variantURI)
	if err != nil {
		return err
	}

	qc.mu.Lock()
	actions := newWindowActions(qc.windowView)
	defer actions.run(qc.runOps)
	defer qc.mu.Unlock()

	item := qc.itemByIDLocked(requestID)
	if item == nil {
		return ErrStaleFileInfoSubmission
	}
	item.Probing = false
	ready := qc.intake.ApplyVariant(context.Background(), &item.Target, selection, qc.reservedNamesLocked(item.ID))

	var start *preDownloadStart
	if ready && item.PreDownload && item.PreDownloadTaskID == "" && !item.preDownloadStarting {
		// PreDownloadTaskID 还是空的：此刻直接提交会在同一链接上建出第二份任务。
		item.preDownloadStarting = true
		item.preDownloadStartedCh = make(chan struct{})
		start = &preDownloadStart{item: item, target: item.Target}
	}

	itemCopy := *item
	actions.emitFunc(protocol.EventFileInfoUpdated, func() any {
		return qc.snapshotItem(itemCopy.ID, &itemCopy)
	})

	if start != nil {
		go qc.startPreDownload(start)
	}
	return nil
}

// beginHLSSelection 在锁内确认这一项还在，并取走选择清晰度要用的事实。
func (qc *QueueController) beginHLSSelection(ctx context.Context, requestID, urlStr string) (*hlsSelection, error) {
	for {
		qc.mu.Lock()

		item := qc.itemByIDLocked(requestID)
		if item == nil {
			qc.mu.Unlock()
			return nil, ErrStaleFileInfoSubmission
		}
		if strings.TrimSpace(urlStr) == "" {
			qc.mu.Unlock()
			return nil, errors.New("缺少下载链接，无法选择清晰度")
		}
		if !item.ProbeSettled() {
			// 登记时那次探测还没回来。等它落地，否则这里补出来的元数据会盖掉它，
			// 而它才是提交要复用的那一份。
			if done := item.probeDone; done != nil {
				qc.mu.Unlock()
				select {
				case <-done:
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		}

		job := &hlsSelection{url: urlStr, target: item.Target}
		qc.mu.Unlock()
		return job, nil
	}
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
	actions.emitFunc(protocol.EventFileInfoUpdated, func() any {
		return qc.snapshotItem(itemCopy.ID, &itemCopy)
	})
	return false
}

// Submit confirms the active file info item with final user choices.
//
// 分成三段：锁内取事实与占位，锁外调引擎，再回锁内落地。
// 中间那段不能放在锁里——真实引擎的建任务路径仍可能联网（探测会重试三次、还有一次 HEAD
// 兜底，HTTP 客户端没有超时），锁被它握住时读当前项、取消、下一次交接全都排在后面，
// 交接会在 2.5 秒上限处静默失败。
//
// 交接过来的链接在登记时就已经探过一次，提交直接用那次结果建任务（AddTaskFromProbe），
// 因此寻常的提交根本不联网、瞬时返回；只有用户改过链接、或链接是手输的（没有任何探测
// 结果）才退回让引擎自己探。
func (qc *QueueController) Submit(ctx context.Context, sub FileInfoSubmission) (*task.Task, error) {
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
	qc.mu.Unlock()

	return qc.submitItem(ctx, sub.RequestID, sub)
}

func (qc *QueueController) submitItem(ctx context.Context, itemID string, sub FileInfoSubmission) (*task.Task, error) {
	plan, err := qc.beginSubmitItem(ctx, itemID, sub)
	if err != nil {
		return nil, err
	}

	result, err := qc.intake.Submit(ctx, plan.target, plan.choice)

	qc.mu.Lock()
	actions := newWindowActions(qc.windowView)
	// 同 Enqueue：解锁先于窗口副作用执行，界面推进不必等确认动作里的其他调用返回。
	defer actions.run(qc.runOps)
	defer qc.mu.Unlock()

	// 锁外算出的裁决无论成败都要写回：提交失败时界面刷新看到的也必须是这一份事实。
	plan.item.DuplicateDecision = result.Decision
	plan.item.submitting = false

	if err != nil {
		return nil, err
	}
	if result.Task != nil {
		if qc.onSubmitTask != nil {
			onSubmit := qc.onSubmitTask
			resTask := result.Task
			actions.add(func() { onSubmit(resTask) })
		}
	}
	// 按 ID 摘除：提交期间用户可能已经切到别的项上，按位置删会删错人。
	if idx := qc.indexOfLocked(plan.item.ID); idx >= 0 {
		qc.items = append(qc.items[:idx], qc.items[idx+1:]...)
		if qc.activeIndex > idx {
			qc.activeIndex--
		}
		qc.advanceQueueLocked(actions)
	}

	return result.Task, nil
}

// submitPlan 是提交穿过锁的一次快照：锁内取事实，锁外交给 intake，回来后按 ID 落地。
type submitPlan struct {
	item   *FileInfoItem
	target intake.Target
	choice intake.Choice
}

// beginSubmitItem 在锁内确认这一项可以提交，标出「提交在路上」，并取走这次提交需要的事实。
func (qc *QueueController) beginSubmitItem(ctx context.Context, itemID string, sub FileInfoSubmission) (*submitPlan, error) {
	for {
		qc.mu.Lock()

		active := qc.itemByIDLocked(itemID)
		if active == nil {
			qc.mu.Unlock()
			return nil, ErrStaleFileInfoSubmission
		}
		if !active.ProbeSettled() {
			// 登记时发起的那次探测还没回来：提交只认它的结果，自己再探一次就是同一份元数据
			// 付两次请求往返。窗口弹出到用户点确认之间通常早就跑完了，这里补的是极短的时间窗。
			if done := active.probeDone; done != nil {
				qc.mu.Unlock()
				select {
				case <-done:
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
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
			item:   active,
			target: active.Target,
			choice: intake.Choice{
				URL:         sub.URL,
				Filename:    sub.Filename,
				Directory:   sub.Directory,
				CategoryID:  sub.CategoryID,
				MaxConn:     sub.MaxConn,
				Action:      sub.Action,
				ReuseTaskID: sub.ReuseTaskID,
				PreTaskID:   active.PreDownloadTaskID,
			},
		}
		qc.mu.Unlock()
		return plan, nil
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

	if active.probeCancel != nil {
		active.probeCancel()
		active.probeCancel = nil
	}
	if active.PreDownloadTaskID != "" {
		_ = qc.intake.CancelPreDownload(ctx, active.PreDownloadTaskID)
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
		nextCopy := *qc.items[qc.activeIndex]
		actions.emitFunc(protocol.EventFileInfoNext, func() any {
			return qc.snapshotItem(nextCopy.ID, &nextCopy)
		})
	} else {
		qc.activeIndex = 0
		actions.hide()
	}
}
