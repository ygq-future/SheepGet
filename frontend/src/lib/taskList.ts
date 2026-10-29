import type * as task from '../../bindings/sheep-get/internal/task/models';

/**
 * 任务列表的顺序规则与「一次更新之后该不该重排」的判定。
 *
 * 进度事件每秒最多十几条，排序键却在每次事件里都要用：把时间解析集中在这里，
 * 只在排序键真的变化时才重排，列表顺序就不会因为一条进度更新而整体抖动。
 */

/** 任务排序用的时间戳（毫秒）。老记录可能没有 updatedAt，退回创建时间。 */
export function taskSortTime(t: task.Task): number {
  const raw = t.updatedAt || t.createdAt;
  const time = raw ? new Date(raw).getTime() : 0;
  return Number.isNaN(time) ? 0 : time;
}

/**
 * 按「最后连接时间」倒序：续传或重试历史任务后它同样要浮到最前，
 * 只按创建时间排序会让刚恢复的任务留在列表深处。列表右侧展示的也是这个时间。
 * 时间相同再按 id 倒序，保证顺序稳定。
 */
export function sortTasks(taskList: task.Task[]): task.Task[] {
  return taskList
    .map((t) => ({ t, time: taskSortTime(t) }))
    .sort((a, b) => {
      if (a.time !== b.time) {
        return b.time - a.time;
      }
      return b.t.id.localeCompare(a.t.id);
    })
    .map((entry) => entry.t);
}

/**
 * 把一条任务更新并进列表。排序键没变时只替换那一项——位置由（时间, id）决定，
 * 两者都没变就不必重排；新任务插到最前，其余情况重新排序。
 */
export function applyTaskUpdate(list: task.Task[], updated: task.Task): task.Task[] {
  const idx = list.findIndex((t) => t.id === updated.id);
  if (idx === -1) {
    return sortTasks([updated, ...list]);
  }
  const next = [...list];
  next[idx] = updated;
  if (taskSortTime(list[idx]) === taskSortTime(updated)) {
    return next;
  }
  return sortTasks(next);
}
