import * as task from '../../bindings/sheep-get/internal/task/models';
import { isProcessingFailure } from './progress';

export type TaskControlAction = 'resume' | 'pause' | 'retry';

export interface TaskControlCallbacks {
  resumeTask: (id: string) => Promise<void>;
  pauseTask: (id: string) => Promise<void>;
  retryTask: (id: string) => Promise<void>;
  retryProcessingTask: (id: string) => Promise<void>;
  showProgressWindow: (id: string) => Promise<void>;
}

/**
 * Returns the primary control action available for the given task state.
 * - StatusPaused: 'resume' (continue download and open progress window)
 * - StatusDownloading / StatusQueued: 'pause' (pause download)
 * - StatusError: 'retry' (retry download or processing, and open progress window)
 * - StatusCompleted / StatusProcessing / others: null
 */
export function getTaskControlAction(t: task.Task | null | undefined): TaskControlAction | null {
  if (!t) return null;

  switch (t.status) {
    case task.Status.StatusPaused:
      return 'resume';
    case task.Status.StatusDownloading:
    case task.Status.StatusQueued:
      return 'pause';
    case task.Status.StatusError:
      return 'retry';
    default:
      return null;
  }
}

/**
 * Executes the task control action using the provided backend callbacks.
 * When resuming or retrying, automatically opens and focuses the progress window for the task.
 */
export async function executeTaskControlAction(
  t: task.Task,
  action: TaskControlAction,
  callbacks: TaskControlCallbacks,
): Promise<void> {
  switch (action) {
    case 'resume':
      await callbacks.resumeTask(t.id);
      await callbacks.showProgressWindow(t.id);
      break;
    case 'pause':
      await callbacks.pauseTask(t.id);
      break;
    case 'retry':
      if (isProcessingFailure(t)) {
        await callbacks.retryProcessingTask(t.id);
      } else {
        await callbacks.retryTask(t.id);
      }
      await callbacks.showProgressWindow(t.id);
      break;
  }
}
