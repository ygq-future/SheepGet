import * as task from '../../bindings/sheep-get/internal/task/models';

/**
 * 判断删除单任务时是否应默认勾选「同时删除磁盘上的本地文件」。
 * 未完成任务（非 StatusCompleted）默认勾选以清理临时分片/文件；已完成任务默认不勾选以避免误删成品。
 */
export function shouldDefaultDeleteDiskFileForTask(t: task.Task | null | undefined): boolean {
  if (!t) return false;
  return t.status !== task.Status.StatusCompleted;
}

/**
 * 判断批量删除任务时是否应默认勾选「同时删除磁盘上的本地文件」。
 * 当选中的任务全部为未完成任务时默认勾选；若包含已完成任务或为空，出于安全性考虑默认不勾选。
 */
export function shouldDefaultDeleteDiskFileForBatch(tasks: task.Task[]): boolean {
  if (tasks.length === 0) return false;
  return tasks.every((t) => t.status !== task.Status.StatusCompleted);
}
