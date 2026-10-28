package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"sheep-get/internal/atomicfile"
)

// SettingsService 管理配置文件的读取与原子写入。它只负责持久化快照：把快照推给运行中的
// 子系统是设置应用路径（internal/settingsapply）的事，两者分开后「保存」与「生效」各自
// 只有一处实现。
type SettingsService struct {
	mu                  sync.RWMutex
	filePath            string
	fallbackDownloadDir string
	fallbackTempDir     string
	current             Settings
}

// NewSettingsService creates a new SettingsService, loading from filePath if present or initializing defaults.
func NewSettingsService(filePath, fallbackDownloadDir, fallbackTempDir string) *SettingsService {
	s := &SettingsService{
		filePath:            filePath,
		fallbackDownloadDir: fallbackDownloadDir,
		fallbackTempDir:     fallbackTempDir,
	}

	s.load()
	return s
}

// load reads the configuration from disk, validating fields and falling back to safe defaults.
func (s *SettingsService) load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		s.current = DefaultSettings(s.fallbackDownloadDir, s.fallbackTempDir)
		return
	}

	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil {
		s.current = DefaultSettings(s.fallbackDownloadDir, s.fallbackTempDir)
		return
	}

	s.current = loaded.ValidateAndFallback(s.fallbackDownloadDir, s.fallbackTempDir)
}

// Get returns the current active settings snapshot safely.
func (s *SettingsService) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// Update validates, atomically persists and updates the in-memory snapshot.
func (s *SettingsService) Update(req Settings) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	validated := req.ValidateAndFallback(s.fallbackDownloadDir, s.fallbackTempDir)

	data, err := json.MarshalIndent(validated, "", "  ")
	if err != nil {
		return s.current, fmt.Errorf("failed to marshal settings: %w", err)
	}

	// 配置只在用户点保存时写入，频率低，因此同步落盘，避免关机或断电后读到空配置。
	if err := atomicfile.Write(s.filePath, data, 0644, true); err != nil {
		return s.current, fmt.Errorf("failed to persist settings: %w", err)
	}

	s.current = validated
	return validated, nil
}
