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
type FileTaskStore struct {
	mu       sync.RWMutex
	filePath string
	tasks    map[string]*Task
}

// NewFileTaskStore creates or loads a FileTaskStore from the given path.
func NewFileTaskStore(filePath string) (*FileTaskStore, error) {
	store := &FileTaskStore{
		filePath: filePath,
		tasks:    make(map[string]*Task),
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

func (s *FileTaskStore) persistLocked() error {
	list := make([]*persistentTask, 0, len(s.tasks))
	for _, t := range s.tasks {
		pt := &persistentTask{Task: *t}
		if t.RequestHeaders != nil {
			pt.RawHeaders = t.RequestHeaders.RawHeaders()
		}
		list = append(list, pt)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tasks: %w", err)
	}

	// 任务进度会频繁持久化，这里不做同步落盘，避免每次写入都等待磁盘刷写；
	// 原子改名仍保证读到的要么是旧内容、要么是新内容。
	if err := atomicfile.Write(s.filePath, data, 0644, false); err != nil {
		return fmt.Errorf("failed to persist task store: %w", err)
	}
	return nil
}

func (s *FileTaskStore) Save(_ context.Context, t *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clone task to avoid data race
	cloned := *t
	if len(t.Chunks) > 0 {
		cloned.Chunks = make([]Chunk, len(t.Chunks))
		copy(cloned.Chunks, t.Chunks)
	}
	if t.RequestHeaders != nil {
		cloned.RequestHeaders = t.RequestHeaders.Clone()
	}
	s.tasks[t.ID] = &cloned
	return s.persistLocked()
}

func (s *FileTaskStore) Get(_ context.Context, id string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, exists := s.tasks[id]
	if !exists {
		return nil, ErrTaskNotFound
	}
	cloned := *t
	if t.RequestHeaders != nil {
		cloned.RequestHeaders = t.RequestHeaders.Clone()
	}
	return &cloned, nil
}

func (s *FileTaskStore) List(_ context.Context) ([]*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		cloned := *t
		if t.RequestHeaders != nil {
			cloned.RequestHeaders = t.RequestHeaders.Clone()
		}
		list = append(list, &cloned)
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
	defer s.mu.Unlock()

	if _, exists := s.tasks[id]; !exists {
		return ErrTaskNotFound
	}
	delete(s.tasks, id)
	return s.persistLocked()
}
