import * as task from '../../bindings/sheep-get/internal/task/models';

export type TaskFilterType = 'all' | 'paused' | 'completed' | 'error';

export function filterTasks(
  tasks: task.Task[],
  filter: TaskFilterType,
  selectedCategory: string,
): task.Task[] {
  return tasks.filter((t) => {
    if (filter === 'paused' && t.status !== task.Status.StatusPaused) return false;
    if (filter === 'completed' && t.status !== task.Status.StatusCompleted) return false;
    if (filter === 'error' && t.status !== task.Status.StatusError) return false;
    if (selectedCategory && t.categoryId !== selectedCategory) return false;
    return true;
  });
}
