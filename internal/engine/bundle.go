package engine

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"sheep-get/internal/credentials"
	"sheep-get/internal/logging"
	"sheep-get/internal/task"
)

const (
	// maxBundleMembers 保护任务库与界面：成员明细要随任务一起落盘，没有上限时一次
	// 恶意或失控的批量登记就能把 tasks.json 撑大。
	maxBundleMembers = 10000
	// maxBundleParallelMembers 是一个 zip 任务内部同时在飞的成员数上限。连接预算仍由
	// 任务的 MaxConcurrency（单任务分块连接数）决定，并在这些成员之间均分。
	maxBundleParallelMembers = 4
	bundleProbeTimeout       = 5 * time.Second
	bundleMaxZipIDLength     = 128
	bundleMaxEntryPathLength = 1024
	bundleMaxSegmentLength   = 255
)

var (
	// ErrBundleSealed 表示批次已经闭合，不能继续追加成员。
	ErrBundleSealed = errors.New("zip 批次已闭合，不能继续追加成员")
	// ErrBundleNameMismatch 表示同一个 zipId 被用于不同的 zip 名称。
	ErrBundleNameMismatch = errors.New("zip 名称与已有批次不一致")
	// ErrBundleDirectoryMismatch 表示同一个 zipId 被用于不同的保存目录。
	ErrBundleDirectoryMismatch = errors.New("zip 保存目录与已有批次不一致")
	// ErrBundleMemberConflict 表示 zip 内已经存在同路径成员（大小写不敏感）。
	ErrBundleMemberConflict = errors.New("zip 内已存在同路径成员")
	// ErrBundleInputsMissing 表示打包时成员文件已经不在暂存目录，需要重新下载。
	ErrBundleInputsMissing = errors.New("zip 成员文件缺失，需要重新下载")
	// ErrNotBundle 表示任务不是 zip 聚合任务。
	ErrNotBundle = errors.New("任务不是 zip 聚合任务")
)

// bundleConflictError 让环回接口把批次冲突报成 409，而不是笼统的 400。
// server 包只认 HTTPStatus 接口，不必反过来依赖引擎类型。
type bundleConflictError struct {
	err error
}

func (e *bundleConflictError) Error() string   { return e.err.Error() }
func (e *bundleConflictError) Unwrap() error   { return e.err }
func (e *bundleConflictError) HTTPStatus() int { return http.StatusConflict }

func bundleConflict(err error) error {
	return &bundleConflictError{err: err}
}

// BundleMemberRequest 是一次成员登记里入口已知的事实。Probe/ProbeErr 由调用方已经
// 探过时传入；都为空时由引擎自己探一次。
type BundleMemberRequest struct {
	URL       string
	Filename  string
	EntryPath string
	Headers   map[string]string
	Probe     *ProbeResult
	ProbeErr  error
}

// BundleRegisterRequest 是登记一个 zip 成员所需的全部输入。
type BundleRegisterRequest struct {
	ZipID      string
	ZipName    string
	Directory  string
	CategoryID string
	Final      bool
	MaxConn    int
	// ProgressDisplay 只在创建批次时生效；后续成员不会改变已经固化的展示约定。
	ProgressDisplay *task.ProgressDisplay
	Member          BundleMemberRequest
}

// BundleRegistration 是成员登记的结果。Created 表示这次调用创建了批次任务。
type BundleRegistration struct {
	Task      *task.Task
	Member    task.ZipMember
	Created   bool
	Duplicate bool
}

// BundleFilename 把调用方给的 zip 名称清洗成一个安全的 .zip 文件名。
// 名字为空时退回 zipId；仍然为空时使用固定兜底名。调用方在解析分类目录时也用它，
// 保证「目录按哪个名字算」与最终落点用的是同一个名字。
func BundleFilename(requested, zipID string) string {
	name := CleanFilename(requested)
	if name == "" {
		name = CleanFilename(zipID)
	}
	name = sanitizeZipFilename(name)
	ext := filepath.Ext(name)
	if !strings.EqualFold(ext, ".zip") {
		name += ".zip"
	} else if strings.TrimSuffix(name, ext) == "" {
		// 名字只有 ".zip" 时补一个可读的基名。
		name = "SheepGet_bundle" + ext
	}
	return name
}

// RegisterBundleMember 登记一个 zip 成员。首个成员创建批次任务，后续成员追加到同一任务。
//
// 探测在锁外完成，登记本身按 zipId 串行化：Agent 可能并发发送多个成员，不能因为两个
// 请求同时读到旧列表而丢掉其中一个。
func (m *Manager) RegisterBundleMember(ctx context.Context, req BundleRegisterRequest) (*BundleRegistration, error) {
	zipID := strings.TrimSpace(req.ZipID)
	if zipID == "" {
		return nil, errors.New("zipId 不能为空")
	}
	if len(zipID) > bundleMaxZipIDLength {
		return nil, fmt.Errorf("zipId 过长（上限 %d）", bundleMaxZipIDLength)
	}
	if strings.TrimSpace(req.Member.URL) == "" {
		return nil, errors.New("成员缺少下载 URL")
	}

	probe, probeErr := req.Member.Probe, req.Member.ProbeErr
	if probe == nil && probeErr == nil {
		probeCtx, cancel := context.WithTimeout(ctx, bundleProbeTimeout)
		probe, probeErr = m.ProbeURL(probeCtx, req.Member.URL, req.Member.Headers)
		cancel()
	}
	if probeErr != nil {
		if isProbeTimeout(probeErr) {
			// 探测超时不代表资源不存在：按未知大小收下，下载阶段再用真实响应补齐。
			// 这里不阻塞登记，否则一次慢探测就会把 Agent 的整批登记卡住。
			m.log().Warn("bundle member probe timed out, accepting with unknown size",
				"zip", zipID, "url", logging.SafeURL(req.Member.URL), "error", probeErr.Error())
			probe = &ProbeResult{
				URL:        req.Member.URL,
				TotalBytes: -1,
				Filename:   extractFilenameFromURL(req.Member.URL),
			}
			probeErr = nil
		} else {
			return nil, fmt.Errorf("成员探测失败: %w", probeErr)
		}
	}
	if probe == nil {
		probe = &ProbeResult{
			URL:        req.Member.URL,
			TotalBytes: -1,
			Filename:   extractFilenameFromURL(req.Member.URL),
		}
	}
	if probe.HLS != nil {
		return nil, errors.New("zip 聚合暂不支持 HLS 成员，请单独下载该媒体")
	}

	if strings.ContainsAny(req.Member.Filename, `/\`) {
		return nil, errors.New("成员 filename 不能包含路径分隔符，请改用 zipPath")
	}
	filename := CleanFilename(req.Member.Filename)
	if filename == "" && probe != nil {
		filename = CleanFilename(probe.Filename)
	}
	if filename == "" {
		filename = CleanFilename(URLFilename(req.Member.URL))
	}
	if filename == "" {
		return nil, errors.New("无法确定成员文件名，请显式传入 filename")
	}

	entryRaw := strings.TrimSpace(req.Member.EntryPath)
	if entryRaw == "" {
		entryRaw = filename
	}
	entryPath, err := normalizeZipEntryPath(entryRaw)
	if err != nil {
		return nil, err
	}

	member := task.ZipMember{
		URL:            req.Member.URL,
		FinalURL:       finalURLFrom("", probe),
		EntryPath:      entryPath,
		Filename:       filename,
		TotalBytes:     -1,
		Headers:        cloneStringMap(req.Member.Headers),
		Resumable:      probe.Resumable,
		ETag:           probe.ETag,
		LastModified:   probe.LastModified,
		DirectTransfer: probe.DirectTransfer,
		ProxyAddr:      probe.ProxyAddr,
	}
	if probe.TotalBytes >= 0 {
		member.TotalBytes = probe.TotalBytes
	}

	m.bundleMu.Lock()
	defer m.bundleMu.Unlock()

	tasks, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	var existing *task.Task
	for _, t := range tasks {
		if t.IsBundle() && t.Bundle.ZipID == zipID {
			existing = t
			break
		}
	}

	if existing != nil {
		if existing.Bundle.Sealed || existing.Status == task.StatusCompleted {
			return nil, bundleConflict(ErrBundleSealed)
		}
		if requested := strings.TrimSpace(req.ZipName); requested != "" {
			requestedName := BundleFilename(requested, zipID)
			if !strings.EqualFold(requestedName, existing.Bundle.RequestedName) {
				return nil, bundleConflict(fmt.Errorf("%w: 请求 %q，已有批次 %q",
					ErrBundleNameMismatch, requestedName, existing.Bundle.RequestedName))
			}
		}
		if dir := strings.TrimSpace(req.Directory); dir != "" && !SamePath(dir, existing.Directory) {
			return nil, bundleConflict(fmt.Errorf("%w: 请求 %q，已有批次 %q",
				ErrBundleDirectoryMismatch, dir, existing.Directory))
		}
		if idx, conflict := bundleEntryConflict(existing.Bundle.Members, entryPath); conflict {
			if existing.Bundle.Members[idx].URL == member.URL {
				// Agent 超时重试同一条登记：幂等返回，不产生重复条目。
				// 未完成的成员顺手刷新请求上下文：重试往往正是为了换一个已更新的令牌。
				if !existing.Bundle.Members[idx].Done {
					existing.Bundle.Members[idx].Headers = member.Headers
					existing.Bundle.Members[idx].FinalURL = member.FinalURL
					existing.Bundle.Members[idx].ETag = member.ETag
					existing.Bundle.Members[idx].LastModified = member.LastModified
					existing.Bundle.Members[idx].Resumable = member.Resumable
					if member.TotalBytes >= 0 {
						existing.Bundle.Members[idx].TotalBytes = member.TotalBytes
					}
					existing.Bundle.Members[idx].Error = ""
					existing.UpdatedAt = time.Now()
					if err := m.store.Save(ctx, existing); err != nil {
						return nil, err
					}
					m.notifyPayload(existing.EventView())
				}
				return &BundleRegistration{
					Task:      existing.Clone(),
					Member:    existing.Bundle.Members[idx],
					Duplicate: true,
				}, nil
			}
			return nil, bundleConflict(fmt.Errorf("%w: %q", ErrBundleMemberConflict, entryPath))
		}
		if len(existing.Bundle.Members) >= maxBundleMembers {
			return nil, fmt.Errorf("zip 批次成员数超过上限 %d", maxBundleMembers)
		}

		member.StagingName = fmt.Sprintf("m%05d.part", len(existing.Bundle.Members)+1)
		existing.Bundle.Members = append(existing.Bundle.Members, member)
		recomputeBundle(existing)
		if req.Final {
			existing.Bundle.Sealed = true
			existing.Status = task.StatusQueued
		}
		existing.UpdatedAt = time.Now()
		if err := m.store.Save(ctx, existing); err != nil {
			return nil, err
		}
		m.notifyPayload(existing.EventView())
		if req.Final {
			m.schedule()
		}
		return &BundleRegistration{Task: existing.Clone(), Member: member}, nil
	}

	dir := strings.TrimSpace(req.Directory)
	if dir == "" {
		return nil, errors.New("zip 聚合缺少保存目录")
	}
	requestedName := BundleFilename(req.ZipName, zipID)
	// 落点名在创建时固化：之后即使磁盘上出现同名文件，也不会悄悄换名字。
	finalName := m.Occupancy(ctx, nil).Suggest(dir, requestedName)
	member.StagingName = "m00001.part"

	now := time.Now()
	t := &task.Task{
		ID:              nextTaskID(),
		Filename:        finalName,
		Directory:       dir,
		CategoryID:      req.CategoryID,
		TempDir:         m.getTempDir(),
		TotalBytes:      -1,
		Status:          task.StatusCollecting,
		MaxConcurrency:  req.MaxConn,
		ProgressDisplay: req.ProgressDisplay,
		Bundle: &task.ZipBundle{
			ZipID:         zipID,
			RequestedName: requestedName,
			Name:          finalName,
			Members:       []task.ZipMember{member},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if t.MaxConcurrency <= 0 {
		t.MaxConcurrency = m.config.DefaultConnectionsPerTask
	}
	recomputeBundle(t)
	if req.Final {
		t.Bundle.Sealed = true
		t.Status = task.StatusQueued
	}
	if err := m.store.Save(ctx, t); err != nil {
		return nil, err
	}
	m.notifyPayload(t.EventView())
	if req.Final {
		m.schedule()
	}
	return &BundleRegistration{Task: t.Clone(), Member: member, Created: true}, nil
}

// SealBundle 手动闭合一个仍在收集的批次。它是 Agent 忘记发送 zipFinal 时的安全阀：
// 没有它，一个批次会永远停在 collecting，既不下载也不失败。
func (m *Manager) SealBundle(ctx context.Context, id string) error {
	m.bundleMu.Lock()
	defer m.bundleMu.Unlock()

	t, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if !t.IsBundle() {
		return ErrNotBundle
	}
	if t.Bundle.Sealed {
		return nil
	}
	if len(t.Bundle.Members) == 0 {
		return errors.New("zip 批次还没有成员")
	}
	t.Bundle.Sealed = true
	if t.Status == task.StatusCollecting {
		t.Status = task.StatusQueued
	}
	t.UpdatedAt = time.Now()
	if err := m.store.Save(ctx, t); err != nil {
		return err
	}
	m.notifyPayload(t.EventView())
	m.schedule()
	return nil
}

// retryBundle 重新排队一次 zip 下载。已完成的成员保留，重试只补失败或缺失的成员；
// TransferDone 清零让下一次运行重新核对暂存目录，而不是直接相信旧记录。
func (m *Manager) retryBundle(ctx context.Context, t *task.Task) error {
	if !t.Bundle.Sealed {
		return errors.New("zip 批次仍在收集成员，无法重试下载")
	}
	if t.Status == task.StatusCompleted {
		return nil
	}
	for i := range t.Bundle.Members {
		t.Bundle.Members[i].Error = ""
	}
	t.TransferDone = false
	t.Status = task.StatusQueued
	t.FailurePhase = task.FailurePhaseNone
	t.ErrorMsg = ""
	t.UpdatedAt = time.Now()
	if err := m.store.Save(ctx, t); err != nil {
		return err
	}
	m.notifyPayload(t.EventView())
	m.schedule()
	return nil
}

// runBundleTransfer 下载批次里尚未完成的成员。
//
// 连接预算沿用任务自己的 MaxConcurrency（来自「单任务分块连接数」设置）：成员池最多
// 4 个文件同时在飞，预算在这些成员之间均分。小文件由下载器的 MinChunkSize 自然收敛到
// 单连接，不会因为预算大就开满。
func (m *Manager) runBundleTransfer(ctx context.Context, t *task.Task, _ TransferSink) error {
	if t.Bundle == nil {
		return errors.New("任务缺少 zip 批次")
	}
	staging := m.bundleStagingDir(t)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return fmt.Errorf("无法创建 zip 暂存目录: %w", err)
	}

	changed := m.reconcileBundleMembers(t, staging)
	recomputeBundle(t)
	if changed {
		_ = m.saveIfPresent(ctx, t)
	}

	pending := make([]int, 0, len(t.Bundle.Members))
	for i := range t.Bundle.Members {
		if !t.Bundle.Members[i].Done {
			pending = append(pending, i)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	budget := t.MaxConcurrency
	if budget <= 0 {
		budget = m.config.DefaultConnectionsPerTask
	}
	poolSize := min(maxBundleParallelMembers, budget, len(pending))
	if poolSize < 1 {
		poolSize = 1
	}
	memberMaxConn := budget / poolSize
	if memberMaxConn < 1 {
		memberMaxConn = 1
	}

	transfer := &bundleTransfer{
		manager: m,
		ctx:     ctx,
		task:    t,
		staging: staging,
		progress: &bundleProgress{
			manager:     m,
			ctx:         ctx,
			taskID:      t.ID,
			baseElapsed: t.ElapsedNano,
			runStarted:  time.Now(),
		},
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int)
	errs := make(chan error, poolSize)
	var wg sync.WaitGroup
	for range poolSize {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if runCtx.Err() != nil {
					return
				}
				if err := transfer.downloadMember(runCtx, idx, memberMaxConn); err != nil {
					select {
					case errs <- err:
					default:
					}
					cancel()
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, idx := range pending {
			select {
			case <-runCtx.Done():
				return
			case jobs <- idx:
			}
		}
	}()

	wg.Wait()
	close(errs)
	var firstErr error
	for err := range errs {
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return firstErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	for i := range t.Bundle.Members {
		if !t.Bundle.Members[i].Done {
			return fmt.Errorf("成员 %q 未完成", t.Bundle.Members[i].EntryPath)
		}
	}
	recomputeBundle(t)
	transfer.publishFinal()
	return nil
}

// reconcileBundleMembers 用暂存目录里的事实修正成员状态：重启或崩溃后任务记录可能落后于
// 磁盘（成员文件已落盘但记录还没写），也可能反过来（记录说完成但文件被清理）。以磁盘为准。
func (m *Manager) reconcileBundleMembers(t *task.Task, staging string) bool {
	changed := false
	for i := range t.Bundle.Members {
		member := &t.Bundle.Members[i]
		finalPath := filepath.Join(staging, member.StagingName)
		if info, err := os.Stat(finalPath); err == nil && !info.IsDir() {
			// 最终暂存文件只在下载器确认完整后才由 part 改名而来，因此它存在就是完成事实。
			size := info.Size()
			if !member.Done || member.Downloaded != size || member.TotalBytes != size {
				member.Done = true
				member.Downloaded = size
				member.TotalBytes = size
				member.Error = ""
				changed = true
			}
			continue
		}
		if member.Done {
			member.Done = false
			member.Downloaded = 0
			member.Chunks = nil
			member.Error = ""
			changed = true
		}
		// 分片文件不在时不能沿用旧的 Chunks：下载器会把已记录的分片当成已落盘而跳过。
		if _, err := os.Stat(finalPath + ".sheepget"); err != nil {
			if len(member.Chunks) > 0 || member.Downloaded != 0 {
				member.Chunks = nil
				member.Downloaded = 0
				changed = true
			}
		}
	}
	return changed
}

type bundleTransfer struct {
	manager  *Manager
	ctx      context.Context
	task     *task.Task
	staging  string
	progress *bundleProgress

	mu             sync.Mutex
	pendingPublish bool
}

func (b *bundleTransfer) downloadMember(ctx context.Context, index int, maxConn int) error {
	b.mu.Lock()
	member := b.task.Bundle.Members[index]
	total := len(b.task.Bundle.Members)
	b.mu.Unlock()

	memberTask := &task.Task{
		ID:             fmt.Sprintf("%s:m%05d", b.task.ID, index+1),
		URL:            member.URL,
		FinalURL:       member.FinalURL,
		Filename:       member.StagingName,
		Directory:      b.staging,
		TempDir:        "",
		TotalBytes:     member.TotalBytes,
		Downloaded:     member.Downloaded,
		MaxConcurrency: maxConn,
		Resumable:      member.Resumable,
		ETag:           member.ETag,
		LastModified:   member.LastModified,
		Chunks:         append([]task.Chunk(nil), member.Chunks...),
		RequestHeaders: credentials.New(member.Headers),
		DirectTransfer: member.DirectTransfer,
		ProxyAddr:      member.ProxyAddr,
	}
	sink := &bundleMemberSink{transfer: b, index: index}
	downloadErr := b.manager.downloader.Download(ctx, memberTask, sink)
	if finishErr := b.finishMember(index, memberTask, downloadErr); finishErr != nil {
		return fmt.Errorf("成员 %d/%d %q 下载失败: %w", index+1, total, member.EntryPath, finishErr)
	}
	return nil
}

func (b *bundleTransfer) finishMember(index int, memberTask *task.Task, downloadErr error) error {
	b.mu.Lock()
	member := &b.task.Bundle.Members[index]
	member.ETag = memberTask.ETag
	member.LastModified = memberTask.LastModified
	member.Resumable = memberTask.Resumable
	member.DirectTransfer = memberTask.DirectTransfer
	member.ProxyAddr = memberTask.ProxyAddr
	member.Chunks = append([]task.Chunk(nil), memberTask.Chunks...)

	var finishErr error
	if downloadErr == nil {
		if info, err := os.Stat(filepath.Join(b.staging, member.StagingName)); err == nil && !info.IsDir() {
			member.Done = true
			member.Downloaded = info.Size()
			member.TotalBytes = info.Size()
			member.Error = ""
		} else {
			member.Done = false
			member.Error = fmt.Sprintf("成员文件未落盘: %v", err)
			finishErr = errors.New(member.Error)
		}
	} else {
		member.Error = downloadErr.Error()
		finishErr = downloadErr
	}
	recomputeBundle(b.task)
	// 成员完成也走常规节流：1000 个小文件逐个强制发事件会把界面刷成风暴；
	// 最后一个成员由 publishFinal 强制收口。
	event, full := b.progress.snapshotLocked(b.task, false, false)
	b.mu.Unlock()
	b.progress.publish(event, full)
	return finishErr
}

func (b *bundleTransfer) updateMember(index int, memberTask *task.Task) {
	b.mu.Lock()
	member := &b.task.Bundle.Members[index]
	member.Downloaded = memberTask.Downloaded
	if memberTask.TotalBytes > 0 {
		member.TotalBytes = memberTask.TotalBytes
	}
	member.ETag = memberTask.ETag
	member.LastModified = memberTask.LastModified
	member.Resumable = memberTask.Resumable
	member.DirectTransfer = memberTask.DirectTransfer
	member.ProxyAddr = memberTask.ProxyAddr
	member.Chunks = append([]task.Chunk(nil), memberTask.Chunks...)
	recomputeBundle(b.task)
	forceEvent := b.pendingPublish
	b.pendingPublish = false
	event, full := b.progress.snapshotLocked(b.task, forceEvent, false)
	b.mu.Unlock()
	b.progress.publish(event, full)
}

func (b *bundleTransfer) wantSnapshot() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if time.Since(b.progress.lastPublish) < progressPublishInterval {
		return false
	}
	// 先预约这一帧：并发成员里只有一个会真正发布，避免多个成员在同一窗口各发一帧。
	b.progress.lastPublish = time.Now()
	b.pendingPublish = true
	return true
}

func (b *bundleTransfer) publishFinal() {
	b.mu.Lock()
	recomputeBundle(b.task)
	event, full := b.progress.snapshotLocked(b.task, true, true)
	b.mu.Unlock()
	b.progress.publish(event, full)
}

type bundleMemberSink struct {
	transfer *bundleTransfer
	index    int
}

func (s *bundleMemberSink) WantsSnapshot() bool {
	return s.transfer.wantSnapshot()
}

func (s *bundleMemberSink) PublishSnapshot(member *task.Task) {
	s.transfer.updateMember(s.index, member)
}

// bundleProgress 是聚合任务的进度出口：事件载荷只带摘要，落盘按节流写入完整成员表。
// 它不能直接复用 progressSink——后者每帧都要求一份完整任务副本，成员一多就会把
// 60ms 的界面刷新变成平方级的分配。
type bundleProgress struct {
	manager     *Manager
	ctx         context.Context
	taskID      string
	baseElapsed int64
	runStarted  time.Time

	lastPublish    time.Time
	lastSave       time.Time
	lastSavedBytes int64
	published      int64
}

func (p *bundleProgress) snapshotLocked(t *task.Task, forceEvent, forceSave bool) (*task.Task, *task.Task) {
	now := time.Now()
	eventDue := forceEvent || now.Sub(p.lastPublish) >= progressPublishInterval
	saveDue := forceSave || p.lastSave.IsZero() ||
		(now.Sub(p.lastSave) >= progressSaveInterval && t.Downloaded-p.lastSavedBytes >= progressSaveBytes)

	var event, full *task.Task
	if eventDue {
		p.lastPublish = now
		event = t.EventView()
	}
	if saveDue {
		p.lastSave = now
		p.lastSavedBytes = t.Downloaded
		full = t.Clone()
	}
	return event, full
}

func (p *bundleProgress) publish(event, full *task.Task) {
	elapsed := p.baseElapsed + time.Since(p.runStarted).Nanoseconds()
	if full != nil {
		full.ElapsedNano = elapsed
		if !p.manager.taskDeleted(p.taskID) {
			_ = p.manager.store.Save(p.ctx, full)
		}
	}
	if event == nil {
		return
	}
	event.ElapsedNano = elapsed

	p.manager.mu.Lock()
	if delta := event.Downloaded - p.published; delta > 0 {
		p.manager.speedSamples[p.taskID] += delta
	}
	p.published = event.Downloaded
	event.Speed = p.manager.taskSpeed[p.taskID]
	p.manager.mu.Unlock()

	if !p.manager.taskDeleted(p.taskID) {
		p.manager.notifyPayload(event)
	}
}

// runBundlePackaging 把暂存目录里的成员按登记顺序写成一个 zip。
// 写的是 <目标>.sheepget，最后再原子改名——任何中途失败都不会留下半个成品。
func (m *Manager) runBundlePackaging(ctx context.Context, t *task.Task) error {
	if t.Bundle == nil {
		return errors.New("任务缺少 zip 批次")
	}
	if err := os.MkdirAll(t.Directory, 0o755); err != nil {
		return fmt.Errorf("无法创建保存目录: %w", err)
	}
	staging := m.bundleStagingDir(t)

	var total int64
	for i := range t.Bundle.Members {
		member := &t.Bundle.Members[i]
		info, err := os.Stat(filepath.Join(staging, member.StagingName))
		if err != nil || info.IsDir() {
			return fmt.Errorf("%w: 成员 %q", ErrBundleInputsMissing, member.EntryPath)
		}
		total += info.Size()
		member.Done = true
		member.Downloaded = info.Size()
		member.TotalBytes = info.Size()
	}
	recomputeBundle(t)
	if total > 0 {
		if err := checkPathsDiskSpace(staging, t.Directory, "", total); err != nil {
			return err
		}
	}

	destPath := filepath.Join(t.Directory, t.Filename)
	if _, err := os.Stat(destPath); err == nil {
		// 落点名在创建时已经固化；打包时同名文件出现说明有别的程序抢先落盘。
		// 不覆盖、不改名，让用户决定怎么处理，保证 Agent 拿到的名字始终稳定。
		return fmt.Errorf("目标文件已存在：%s，请移动或删除后重试", destPath)
	}
	partPath := destPath + ".sheepget"
	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("无法创建 zip 临时文件: %w", err)
	}

	zw := zip.NewWriter(file)
	writeErr := func() error {
		for i := range t.Bundle.Members {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := writeZipMember(zw, staging, &t.Bundle.Members[i]); err != nil {
				return err
			}
			t.Bundle.PackagedFiles = i + 1
			t.UpdatedAt = time.Now()
			if i%10 == 0 || i == len(t.Bundle.Members)-1 {
				m.notifyPayload(t.EventView())
			}
		}
		return nil
	}()

	closeErr := zw.Close()
	syncErr := file.Sync()
	fileErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(partPath)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(partPath)
		return closeErr
	}
	if syncErr != nil {
		_ = os.Remove(partPath)
		return syncErr
	}
	if fileErr != nil {
		_ = os.Remove(partPath)
		return fileErr
	}

	if err := moveFile(partPath, destPath); err != nil {
		return fmt.Errorf("无法将 zip 移动到目标位置: %w", err)
	}
	if info, err := os.Stat(destPath); err == nil {
		t.TotalBytes = info.Size()
		t.Downloaded = info.Size()
	}
	t.Bundle.PackagedFiles = len(t.Bundle.Members)
	m.removeBundleStaging(t)
	return nil
}

func writeZipMember(zw *zip.Writer, staging string, member *task.ZipMember) error {
	src, err := os.Open(filepath.Join(staging, member.StagingName))
	if err != nil {
		return fmt.Errorf("%w: 成员 %q", ErrBundleInputsMissing, member.EntryPath)
	}
	defer func() { _ = src.Close() }()

	header := &zip.FileHeader{
		Name:   member.EntryPath,
		Method: zip.Store,
	}
	header.SetMode(0o644)
	header.Modified = zipModifiedTime(member.LastModified)

	dst, err := zw.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("无法写入 zip 条目 %q: %w", member.EntryPath, err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("写入 zip 条目 %q 失败: %w", member.EntryPath, err)
	}
	return nil
}

func zipModifiedTime(lastModified string) time.Time {
	if lastModified != "" {
		if parsed, err := http.ParseTime(lastModified); err == nil {
			if parsed.Year() < 1980 {
				return time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
			}
			return parsed
		}
	}
	return time.Now()
}

func (m *Manager) bundleStagingDir(t *task.Task) string {
	return m.GetPartPath(t) + ".files"
}

func (m *Manager) removeBundleStaging(t *task.Task) {
	if t == nil {
		return
	}
	_ = os.RemoveAll(m.bundleStagingDir(t))
}

// recomputeBundle 从成员事实重算批次汇总。未知大小的成员会让任务总大小保持 -1：
// 界面据此显示「未知大小」，而不是拿一个只含已知成员的假总和冒充完整进度。
func recomputeBundle(t *task.Task) {
	if t == nil || t.Bundle == nil {
		return
	}
	var sum, known int64
	allKnown := true
	done := 0
	for i := range t.Bundle.Members {
		member := &t.Bundle.Members[i]
		sum += member.Downloaded
		if member.TotalBytes >= 0 {
			known += member.TotalBytes
		} else {
			allKnown = false
		}
		if member.Done {
			done++
		}
	}
	t.Bundle.FilesTotal = len(t.Bundle.Members)
	t.Bundle.FilesDone = done
	t.Bundle.KnownBytes = known
	t.Downloaded = sum
	if allKnown {
		t.TotalBytes = known
	} else {
		t.TotalBytes = -1
	}
}

// sanitizeZipFilename 清掉文件名里 Windows 不允许的字符，并避开保留名。
// 它只处理最终文件名，不处理 zip 内条目路径（那条路径由 normalizeZipEntryPath 管）。
func sanitizeZipFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"/\|?*`, r) {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	cleaned := strings.TrimSpace(b.String())
	cleaned = strings.TrimRight(cleaned, ". ")
	if cleaned == "" || isWindowsReservedName(cleaned) {
		return "SheepGet_bundle"
	}
	return cleaned
}

func isProbeTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "deadline exceeded")
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// normalizeZipEntryPath 把成员在 zip 内的路径收敛成安全的相对路径。
// 这个路径会被解压工具直接使用，因此 zip slip、Windows 保留名、控制字符都要在登记时拒绝。
func normalizeZipEntryPath(raw string) (string, error) {
	entry := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	if entry == "" {
		return "", errors.New("zip 成员路径不能为空")
	}
	if strings.HasPrefix(entry, "/") {
		return "", errors.New("zip 成员路径不能是绝对路径")
	}
	if len(entry) > bundleMaxEntryPathLength {
		return "", fmt.Errorf("zip 成员路径过长（上限 %d）", bundleMaxEntryPathLength)
	}

	segments := strings.Split(entry, "/")
	cleaned := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("zip 成员路径包含非法段 %q", segment)
		}
		if len(segment) > bundleMaxSegmentLength {
			return "", fmt.Errorf("zip 成员路径段过长: %q", segment)
		}
		if strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
			return "", fmt.Errorf("zip 成员路径段不能以点或空格结尾: %q", segment)
		}
		if strings.ContainsAny(segment, ":*?\"<>|") {
			return "", fmt.Errorf("zip 成员路径段包含非法字符: %q", segment)
		}
		for _, r := range segment {
			if r < 0x20 || r == 0x7f {
				return "", fmt.Errorf("zip 成员路径包含控制字符: %q", segment)
			}
		}
		if isWindowsReservedName(segment) {
			return "", fmt.Errorf("zip 成员路径使用了 Windows 保留名: %q", segment)
		}
		cleaned = append(cleaned, segment)
	}
	return strings.Join(cleaned, "/"), nil
}

var windowsReservedNames = map[string]struct{}{
	"CON": {}, "PRN": {}, "AUX": {}, "NUL": {}, "CLOCK$": {},
	"COM1": {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {},
	"COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
	"LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {},
	"LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
}

func isWindowsReservedName(segment string) bool {
	base := segment
	if idx := strings.IndexByte(base, '.'); idx >= 0 {
		base = base[:idx]
	}
	base = strings.ToUpper(strings.TrimSpace(base))
	if base == "" {
		return false
	}
	_, reserved := windowsReservedNames[base]
	return reserved
}

// bundleEntryConflict 判断新条目是否与已有条目冲突。zip 条目是文件，因此
// 「a」与「a/b」也不能共存——解压时前者会挡住后者的目录。
func bundleEntryConflict(members []task.ZipMember, entry string) (int, bool) {
	target := strings.ToLower(entry)
	for i := range members {
		existing := strings.ToLower(members[i].EntryPath)
		if existing == target ||
			strings.HasPrefix(target, existing+"/") ||
			strings.HasPrefix(existing, target+"/") {
			return i, true
		}
	}
	return 0, false
}
