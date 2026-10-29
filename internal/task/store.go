package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"

	"sheep-get/internal/atomicfile"
	"sheep-get/internal/credentials"
)

type persistentTask struct {
	Task
	RawHeaders map[string]string `json:"rawHeaders,omitempty"`
}

var (
	ErrTaskNotFound = errors.New("task not found")
)

// FileTaskStore implements TaskStore using a local JSON file.
//
// 内存里的任务是唯一权威状态，磁盘只是它的快照：每次变更递增 revision 并在锁内取走一份
// 可序列化的副本，序列化与写盘留在锁外。这样 Get/List 不会被正在进行的写盘挡住，多个写盘
// 之间也不会互相覆盖——只写不旧于已写内容的快照。
type FileTaskStore struct {
	mu       sync.RWMutex
	filePath string
	tasks    map[string]*Task
	revision uint64

	persistMu         sync.Mutex // 串行化磁盘写；调用方不持有 mu
	persistedRevision uint64
	// writeFile 是唯一写盘动作，默认 atomicfile.Write。测试用它把写盘卡在中间，
	// 验证读写方不会被正在进行的落盘挡住。
	writeFile func(path string, data []byte, perm os.FileMode, sync bool) error
}

// NewFileTaskStore creates or loads a FileTaskStore from the given path.
func NewFileTaskStore(filePath string) (*FileTaskStore, error) {
	store := &FileTaskStore{
		filePath:  filePath,
		tasks:     make(map[string]*Task),
		writeFile: atomicfile.Write,
	}

	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *FileTaskStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read task store file: %w", err)
	}

	if len(data) == 0 {
		return nil
	}
	var list []*persistentTask
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("failed to unmarshal task store: %w", err)
	}

	for _, pt := range list {
		t := pt.Task
		if len(pt.RawHeaders) > 0 {
			t.RequestHeaders = credentials.New(pt.RawHeaders)
		}
		// As per spec A03 and A19: restart retains records, all wait for manual continuation
		if t.Status == StatusDownloading || t.Status == StatusQueued {
			t.Status = StatusPaused
		}
		t.Speed = 0
		s.tasks[t.ID] = &t
	}
	return nil
}

// snapshotLocked 复制当前内存状态，供锁外序列化。任务对象一旦进入 map 就不再被就地改写
// （Save 换的是整份副本），因此快照与 map 共享的切片在序列化期间是只读的。
func (s *FileTaskStore) snapshotLocked() (uint64, []*persistentTask) {
	list := make([]*persistentTask, 0, len(s.tasks))
	for _, t := range s.tasks {
		pt := &persistentTask{Task: *t}
		if t.RequestHeaders != nil {
			pt.RawHeaders = t.RequestHeaders.RawHeaders()
		}
		list = append(list, pt)
	}
	return s.revision, list
}

// persist 把一份快照写进文件。调用方不持有 s.mu：序列化与磁盘写都不能挡住读写方。
func (s *FileTaskStore) persist(revision uint64, list []*persistentTask) error {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	if revision <= s.persistedRevision {
		// 更新的快照已经在写或写完，它包含了这份快照的全部内容；再写一次只会让磁盘回到旧状态。
		return nil
	}

	data, err := json.Marshal(list)
	if err != nil {
		return fmt.Errorf("failed to marshal tasks: %w", err)
	}

	// 任务进度会频繁持久化，这里不做同步落盘，避免每次写入都等待磁盘刷写；
	// 原子改名仍保证读到的要么是旧内容、要么是新内容。
	if err := s.writeFile(s.filePath, data, 0644, false); err != nil {
		return fmt.Errorf("failed to persist task store: %w", err)
	}
	s.persistedRevision = revision
	return nil
}

func (s *FileTaskStore) Save(_ context.Context, t *Task) error {
	s.mu.Lock()
	// 存进来的是调用方的对象，它之后还可能被改写；库里只放副本。
	s.tasks[t.ID] = t.Clone()
	s.revision++
	revision, list := s.snapshotLocked()
	s.mu.Unlock()

	return s.persist(revision, list)
}

// SaveMany 一次性写入多条变更：启动迁移这类批量修正不该为每条记录重写整个库。
func (s *FileTaskStore) SaveMany(_ context.Context, tasks []*Task) error {
	if len(tasks) == 0 {
		return nil
	}

	s.mu.Lock()
	for _, t := range tasks {
		s.tasks[t.ID] = t.Clone()
	}
	s.revision++
	revision, list := s.snapshotLocked()
	s.mu.Unlock()

	return s.persist(revision, list)
}

func (s *FileTaskStore) Get(_ context.Context, id string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, exists := s.tasks[id]
	if !exists {
		return nil, ErrTaskNotFound
	}
	return t.Clone(), nil
}

func (s *FileTaskStore) List(_ context.Context) ([]*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		list = append(list, t.Clone())
	}

	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].CreatedAt.After(list[j].CreatedAt)
		}
		return list[i].ID > list[j].ID
	})

	return list, nil
}

func (s *FileTaskStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	if _, exists := s.tasks[id]; !exists {
		s.mu.Unlock()
		return ErrTaskNotFound
	}
	delete(s.tasks, id)
	s.revision++
	revision, list := s.snapshotLocked()
	s.mu.Unlock()

	return s.persist(revision, list)
}
