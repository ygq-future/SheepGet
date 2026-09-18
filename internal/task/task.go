// Package task defines the download task domain models and storage interfaces.
package task

import (
	"context"
	"time"

	"sheep-get/internal/credentials"
)

// Status represents the download task status.
type Status string

const (
	StatusQueued      Status = "queued"
	StatusDownloading Status = "downloading"
	StatusProcessing  Status = "processing"
	StatusPaused      Status = "paused"
	StatusCompleted   Status = "completed"
	StatusError       Status = "error"
)

// FailurePhase records which stage of the pipeline failed. 传输与处理是两条不同的失败路径：
// 传输失败可重新传输，处理失败只能复用已下载分片重试处理（ADR-0001/ADR-0004）。
// 它属于任务契约，使界面按任务事实选择重试动作，而不是从错误文案推断失败类型。
type FailurePhase string

const (
	FailurePhaseNone       FailurePhase = ""
	FailurePhaseTransfer   FailurePhase = "transfer"
	FailurePhaseProcessing FailurePhase = "processing"
)

// Chunk represents a segment of a file being downloaded.
type Chunk struct {
	Index      int   `json:"index"`
	Start      int64 `json:"start"`
	End        int64 `json:"end"`
	Downloaded int64 `json:"downloaded"`
	Assisted   bool  `json:"assisted"`
	Completed  bool  `json:"completed"`
}

// Task represents a download task in SheepGet.
type Task struct {
	ID             string       `json:"id"`
	URL            string       `json:"url"`
	Filename       string       `json:"filename"`
	Directory      string       `json:"directory"`
	TempDir        string       `json:"tempDir,omitempty"`
	TotalBytes     int64        `json:"totalBytes"`
	Downloaded     int64        `json:"downloaded"`
	Speed          int64        `json:"speed"` // bytes per second
	Status         Status       `json:"status"`
	FailurePhase   FailurePhase `json:"failurePhase,omitempty"`
	ErrorMsg       string       `json:"errorMsg,omitempty"`
	MaxConcurrency int          `json:"maxConcurrency"`
	Resumable      bool         `json:"resumable"`
	ETag           string       `json:"etag,omitempty"`
	LastModified   string       `json:"lastModified,omitempty"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`

	// Chunks for multi-connection download state
	Chunks []Chunk `json:"chunks,omitempty"`

	// RequestHeaders carries request context (Referer, Cookie, Authorization, …) required by
	// links whose authorization has expired; applied to every probe and transfer request.
	// Encapsulated in RequestCredentials to ensure sensitive fields are masked by default on serialization.
	RequestHeaders credentials.RequestCredentials `json:"requestHeaders,omitempty"`
}

// TaskStore defines the storage interface for persisting and querying tasks.
type TaskStore interface {
	Save(ctx context.Context, t *Task) error
	Get(ctx context.Context, id string) (*Task, error)
	List(ctx context.Context) ([]*Task, error)
	Delete(ctx context.Context, id string) error
}
