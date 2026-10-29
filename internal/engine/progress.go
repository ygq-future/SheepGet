package engine

import (
	"context"
	"sync"
	"time"

	"sheep-get/internal/task"
)

const (
	// progressPublishInterval 是界面刷新节流：约 16 FPS，足够让进度条与排序动画平滑。
	progressPublishInterval = 60 * time.Millisecond
	// progressSaveInterval / progressSaveBytes 是落盘节流：最多每 500ms 或每 1MB 写一次。
	progressSaveInterval = 500 * time.Millisecond
	progressSaveBytes    = 1 << 20
)

// progressSink 是传输期间对外发布的唯一出口。
//
// 传输侧持有正在被改写的任务对象，外部只应该看到一份此后不会再变的副本，因此这里只做两件事：
// 按节流回答「此刻要不要一份快照」，以及把收到的副本落盘并广播。速度采样同样来自相邻两次
// 已发布字节数的差值——为了拿一个数字而共享可变状态，正是这条竞态的由来。
//
// 两件事分属两段：WantsSnapshot 在传输侧的一致性锁内被调用（分片路径是 coord.mu），因此只做
// 短临界区的记账；PublishSnapshot 要做全量 JSON 落盘与监听者回调，分片路径必须把它留在
// coord.mu 之外——副本在锁内取走，发布在解锁之后。
type progressSink struct {
	manager     *Manager
	ctx         context.Context
	taskID      string
	baseElapsed int64     // 本轮运行启动前已累计的耗时（纳秒）
	runStarted  time.Time // 本轮运行开始的时刻

	mu          sync.Mutex
	published   int64     // 已发布的字节数：速度采样与节流都以它为基准
	lastPublish time.Time // 上一次预约发布窗口的时刻
	lastSave    time.Time // 上一次落盘的时刻
}

func newProgressSink(ctx context.Context, m *Manager, taskID string, baseElapsed int64, runStarted time.Time) *progressSink {
	return &progressSink{
		manager:     m,
		ctx:         ctx,
		taskID:      taskID,
		baseElapsed: baseElapsed,
		runStarted:  runStarted,
	}
}

// WantsSnapshot 报告此刻是否需要一份快照，并在肯定答复时预约这一帧：它会被多个分片工作协程
// 并发调用，只有第一个得到 true，其余在下一个发布窗口再问。
func (s *progressSink) WantsSnapshot() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.lastPublish) < progressPublishInterval {
		return false
	}
	s.lastPublish = time.Now()
	return true
}

// PublishSnapshot 落盘并广播一份自有副本。
func (s *progressSink) PublishSnapshot(snapshot *task.Task) {
	if snapshot == nil || s.manager.taskDeleted(s.taskID) {
		return
	}

	s.mu.Lock()
	if snapshot.Downloaded < s.published {
		// 并发工作协程的副本可能后到；落后的那一份不再发布，界面进度不会往回退。
		s.mu.Unlock()
		return
	}
	delta := snapshot.Downloaded - s.published
	s.published = snapshot.Downloaded
	dueSave := s.lastSave.IsZero() ||
		time.Since(s.lastSave) >= progressSaveInterval ||
		delta >= progressSaveBytes
	if dueSave {
		s.lastSave = time.Now()
	}
	s.mu.Unlock()

	if !s.runStarted.IsZero() {
		snapshot.ElapsedNano = s.baseElapsed + time.Since(s.runStarted).Nanoseconds()
	}

	m := s.manager
	m.mu.Lock()
	if delta > 0 {
		m.speedSamples[s.taskID] += delta
	}
	snapshot.Speed = m.taskSpeed[s.taskID]
	m.mu.Unlock()

	if dueSave && !m.taskDeleted(s.taskID) {
		_ = m.store.Save(s.ctx, snapshot)
	}
	// 删除是终止性的：在途的快照不能把已经删掉的任务重新推回界面。
	if m.taskDeleted(s.taskID) {
		return
	}
	// 这份快照从传输侧交出来之后就没有第二个写方了，可以直接发布，不必再拷一次。
	m.notifyPayload(snapshot)
}
