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
// 它在传输侧的一致性锁内被调用（分片路径是 coord.mu，HLS 是清单侧的锁），因此自己也只做
// 短临界区的记账，落盘与广播放在锁外。
type progressSink struct {
	manager *Manager
	ctx     context.Context
	taskID  string

	mu          sync.Mutex
	published   int64     // 已发布的字节数：速度采样与节流都以它为基准
	lastPublish time.Time // 上一次发布快照的时刻
	lastSave    time.Time // 上一次落盘的时刻
}

func newProgressSink(ctx context.Context, m *Manager, taskID string) *progressSink {
	return &progressSink{manager: m, ctx: ctx, taskID: taskID}
}

// WantsSnapshot 报告此刻是否需要一份快照。它可能被多个分片工作协程并发调用，
// 极窄的窗口里会有不止一个得到肯定答复；PublishSnapshot 只发布前进的那一份。
func (s *progressSink) WantsSnapshot() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.lastPublish) >= progressPublishInterval
}

// PublishSnapshot 落盘并广播一份自有副本。
func (s *progressSink) PublishSnapshot(snapshot *task.Task) {
	if snapshot == nil {
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
	s.lastPublish = time.Now()
	dueSave := s.lastSave.IsZero() ||
		time.Since(s.lastSave) >= progressSaveInterval ||
		delta >= progressSaveBytes
	if dueSave {
		s.lastSave = time.Now()
	}
	s.mu.Unlock()

	m := s.manager
	m.mu.Lock()
	if delta > 0 {
		m.speedSamples[s.taskID] += delta
	}
	snapshot.Speed = m.taskSpeed[s.taskID]
	m.mu.Unlock()

	if dueSave {
		_ = m.store.Save(s.ctx, snapshot)
	}
	// 这份快照从传输侧交出来之后就没有第二个写方了，可以直接发布，不必再拷一次。
	m.notifyPayload(snapshot)
}
