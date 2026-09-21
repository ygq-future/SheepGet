package events

import "testing"

// 事件名常量是前后端契约的一部分，值必须与 frontend/src/lib/events.ts 保持一致。
// 这里钉住字面量，防止有人误改常量值导致前后端静默断联。
func TestEventNames(t *testing.T) {
	cases := map[string]string{
		TaskUpdated:            "task:updated",
		TaskDeleted:            "task:deleted",
		SettingsUpdated:        "settings:updated",
		ProgressClearViewed:    "progress:clear_viewed",
		ProgressFocusCompleted: "progress:focus_completed",
		ProgressFocusTask:      "progress:focus_task",
		FileInfoNext:           "fileinfo:next",
		FileInfoQueueUpdated:   "fileinfo:queue_updated",
		FileInfoUpdated:        "fileinfo:updated",
		AppOpenSettings:        "app:open-settings",
		ServerStatusChanged:    "server:status_changed",
	}
	for constVal, want := range cases {
		if constVal != want {
			t.Errorf("event name constant = %q, want %q", constVal, want)
		}
	}
}
