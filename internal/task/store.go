package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

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

	var list []*Task
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("failed to unmarshal task store: %w", err)
	}

	for _, t := range list {
		// As per spec A03 and A19: restart retains records, all wait for manual continuation
		if t.Status == StatusDownloading || t.Status == StatusQueued {
			t.Status = StatusPaused
		}
		t.Speed = 0
		s.tasks[t.ID] = t
	}
	return nil
}

func (s *FileTaskStore) persistLocked() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create task store dir: %w", err)
	}

	list := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		list = append(list, t)
	}

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tasks: %w", err)
	}

	tmpFile := s.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write tmp task store: %w", err)
	}

	if err := os.Rename(tmpFile, s.filePath); err != nil {
		return fmt.Errorf("failed to atomic rename task store: %w", err)
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
	return &cloned, nil
}

func (s *FileTaskStore) List(_ context.Context) ([]*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		cloned := *t
		list = append(list, &cloned)
	}
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
