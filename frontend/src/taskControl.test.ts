import { describe, it, expect, vi } from 'vitest';
import * as task from '../bindings/sheep-get/internal/task/models';
import { getTaskControlAction, executeTaskControlAction } from './lib/taskControl';

function createTask(opts: Partial<task.Task> = {}): task.Task {
  return new task.Task({
    id: 'test-task-1',
    filename: 'test.mp4',
    status: task.Status.StatusPaused,
    downloaded: 1024,
    totalBytes: 2048,
    ...opts,
  });
}

describe('getTaskControlAction', () => {
  it('returns null for null or undefined task', () => {
    expect(getTaskControlAction(null)).toBeNull();
    expect(getTaskControlAction(undefined)).toBeNull();
  });

  it('returns "resume" for paused tasks', () => {
    const t = createTask({ status: task.Status.StatusPaused });
    expect(getTaskControlAction(t)).toBe('resume');
  });

  it('returns "pause" for downloading and queued tasks', () => {
    expect(getTaskControlAction(createTask({ status: task.Status.StatusDownloading }))).toBe(
      'pause',
    );
    expect(getTaskControlAction(createTask({ status: task.Status.StatusQueued }))).toBe('pause');
  });

  it('returns "retry" for error tasks', () => {
    expect(getTaskControlAction(createTask({ status: task.Status.StatusError }))).toBe('retry');
  });

  it('returns null for completed or processing tasks', () => {
    expect(getTaskControlAction(createTask({ status: task.Status.StatusCompleted }))).toBeNull();
    expect(getTaskControlAction(createTask({ status: task.Status.StatusProcessing }))).toBeNull();
  });

  it('returns null for collecting zip batches: they have no transfer to pause or resume', () => {
    expect(getTaskControlAction(createTask({ status: task.Status.StatusCollecting }))).toBeNull();
  });
});

describe('executeTaskControlAction', () => {
  it('executes resume and opens progress window', async () => {
    const t = createTask({ status: task.Status.StatusPaused });
    const resumeTask = vi.fn().mockResolvedValue(undefined);
    const pauseTask = vi.fn().mockResolvedValue(undefined);
    const retryTask = vi.fn().mockResolvedValue(undefined);
    const retryProcessingTask = vi.fn().mockResolvedValue(undefined);
    const showProgressWindow = vi.fn().mockResolvedValue(undefined);

    await executeTaskControlAction(t, 'resume', {
      resumeTask,
      pauseTask,
      retryTask,
      retryProcessingTask,
      showProgressWindow,
    });

    expect(resumeTask).toHaveBeenCalledWith(t.id);
    expect(showProgressWindow).toHaveBeenCalledWith(t.id);
    expect(pauseTask).not.toHaveBeenCalled();
    expect(retryTask).not.toHaveBeenCalled();
  });

  it('executes pause without opening progress window', async () => {
    const t = createTask({ status: task.Status.StatusDownloading });
    const resumeTask = vi.fn();
    const pauseTask = vi.fn().mockResolvedValue(undefined);
    const retryTask = vi.fn();
    const retryProcessingTask = vi.fn();
    const showProgressWindow = vi.fn();

    await executeTaskControlAction(t, 'pause', {
      resumeTask,
      pauseTask,
      retryTask,
      retryProcessingTask,
      showProgressWindow,
    });

    expect(pauseTask).toHaveBeenCalledWith(t.id);
    expect(showProgressWindow).not.toHaveBeenCalled();
  });

  it('executes normal retry and opens progress window for transfer failure', async () => {
    const t = createTask({
      status: task.Status.StatusError,
      failurePhase: task.FailurePhase.FailurePhaseTransfer,
    });
    const resumeTask = vi.fn();
    const pauseTask = vi.fn();
    const retryTask = vi.fn().mockResolvedValue(undefined);
    const retryProcessingTask = vi.fn();
    const showProgressWindow = vi.fn().mockResolvedValue(undefined);

    await executeTaskControlAction(t, 'retry', {
      resumeTask,
      pauseTask,
      retryTask,
      retryProcessingTask,
      showProgressWindow,
    });

    expect(retryTask).toHaveBeenCalledWith(t.id);
    expect(retryProcessingTask).not.toHaveBeenCalled();
    expect(showProgressWindow).toHaveBeenCalledWith(t.id);
  });

  it('executes processing retry and opens progress window for media processing failure', async () => {
    const t = createTask({
      status: task.Status.StatusError,
      failurePhase: task.FailurePhase.FailurePhaseProcessing,
    });
    const resumeTask = vi.fn();
    const pauseTask = vi.fn();
    const retryTask = vi.fn();
    const retryProcessingTask = vi.fn().mockResolvedValue(undefined);
    const showProgressWindow = vi.fn().mockResolvedValue(undefined);

    await executeTaskControlAction(t, 'retry', {
      resumeTask,
      pauseTask,
      retryTask,
      retryProcessingTask,
      showProgressWindow,
    });

    expect(retryProcessingTask).toHaveBeenCalledWith(t.id);
    expect(retryTask).not.toHaveBeenCalled();
    expect(showProgressWindow).toHaveBeenCalledWith(t.id);
  });
});
