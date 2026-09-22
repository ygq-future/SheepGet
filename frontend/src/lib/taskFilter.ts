import * as task from '../../bindings/sheep-get/internal/task/models';
import * as configModels from '../../bindings/sheep-get/internal/config/models';
import { matchTaskCategory } from './category';

export type TaskFilterType = 'all' | 'paused' | 'completed' | 'error';

export function filterTasks(
  tasks: task.Task[],
  filter: TaskFilterType,
  selectedCategory: string,
  settings: configModels.Settings | null | undefined,
): task.Task[] {
  return tasks.filter((t) => {
    if (filter === 'paused' && t.status !== task.Status.StatusPaused) return false;
    if (filter === 'completed' && t.status !== task.Status.StatusCompleted) return false;
    if (filter === 'error' && t.status !== task.Status.StatusError) return false;
    if (selectedCategory) {
      const catId = matchTaskCategory(t.filename, settings);
      if (catId !== selectedCategory) return false;
    }
    return true;
  });
}
