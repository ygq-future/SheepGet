// 前后端共用的事件名，与 Go 侧 internal/events 包保持一致。
// 改名时两端需同步：这里只维护前端这一份字面量。
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
} as const;
