package engine

import (
	"context"
	"errors"
	"fmt"
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

// Manager orchestrates task queues, concurrency, lifecycle, and progress reporting.
type Manager struct {
	mu           sync.Mutex
	store        task.TaskStore
	downloader   *HTTPDownloader
	config       Config
	activeTasks  map[string]context.CancelFunc
	taskSpeed    map[string]int64
	speedSamples map[string]int64
	listeners    []TaskListener
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

// AddTask probes the URL, checks duplicates, creates a task in queued or paused state, and triggers queue scheduling.
func (m *Manager) AddTask(ctx context.Context, urlStr, dir, filename string, maxConn int) (*task.Task, error) {
	// Check duplicate URL in existing tasks
	existingList, _ := m.store.List(ctx)
	for _, ext := range existingList {
		if ext.URL == urlStr && ext.Status != task.StatusCompleted {
			return nil, fmt.Errorf("该下载链接已存在于任务列表中（状态：%s），请勿重复添加", ext.Status)
		}
	}
	info, err := m.downloader.Probe(ctx, urlStr)
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

				go m.runTask(taskCtx, t.ID)
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
		if errors.Is(err, context.Canceled) {
			t.Status = task.StatusPaused
		} else {
			t.Status = task.StatusError
			t.ErrorMsg = err.Error()
		}
	} else {
		t.Status = task.StatusCompleted
		t.Downloaded = t.TotalBytes
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
}
