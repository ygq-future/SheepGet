// Package events 集中定义前后端共用的事件名。
//
// Go 侧（main 包、internal/window 包）经这些常量 Emit，前端以同名字符串监听。
// 改名只改这一处，并在 frontend/src/lib/events.ts 同步镜像。
package events

const (
	TaskUpdated            = "task:updated"
	TaskDeleted            = "task:deleted"
	SettingsUpdated        = "settings:updated"
	ProgressClearViewed    = "progress:clear_viewed"
	ProgressFocusCompleted = "progress:focus_completed"
	ProgressFocusTask      = "progress:focus_task"
	FileInfoNext           = "fileinfo:next"
	FileInfoQueueUpdated   = "fileinfo:queue_updated"
	FileInfoUpdated        = "fileinfo:updated"
	AppOpenSettings        = "app:open-settings"
	ServerStatusChanged    = "server:status_changed"
)
