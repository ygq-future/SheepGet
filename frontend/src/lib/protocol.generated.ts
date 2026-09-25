// 由 scripts/protocol.mjs 从 internal/protocol/protocol.go 生成，请勿手改。
// 改动线上事实请改 Go 侧那个文件，再运行 node scripts/protocol.mjs --write。

export const Ports = { DefaultServer: 9248, FallbackSpan: 5 };

export const Event = {
  TaskUpdated: 'task:updated',
  TaskDeleted: 'task:deleted',
  SettingsUpdated: 'settings:updated',
  ProgressClearViewed: 'progress:clear_viewed',
  ProgressFocusCompleted: 'progress:focus_completed',
  ProgressFocusTask: 'progress:focus_task',
  FileInfoNext: 'fileinfo:next',
  FileInfoQueueUpdated: 'fileinfo:queue_updated',
  FileInfoUpdated: 'fileinfo:updated',
  AppOpenSettings: 'app:open-settings',
  ServerStatusChanged: 'server:status_changed',
  UpdateAppProgress: 'update:app_progress',
  UpdateExtensionProgress: 'update:extension_progress',
} as const;
