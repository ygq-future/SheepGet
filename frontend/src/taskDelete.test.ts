import { describe, it, expect } from 'vitest';
import * as task from '../bindings/sheep-get/internal/task/models';
import {
  shouldDefaultDeleteDiskFileForTask,
  shouldDefaultDeleteDiskFileForBatch,
} from './lib/taskDelete';

function createTask(id: string, status: task.Status): task.Task {
  return new task.Task({
    id,
    filename: `file_${id}.zip`,
    status,
    url: `http://example.com/file_${id}.zip`,
  });
}

describe('shouldDefaultDeleteDiskFileForTask', () => {
  it('returns false when task is null or undefined', () => {
    expect(shouldDefaultDeleteDiskFileForTask(null)).toBe(false);
    expect(shouldDefaultDeleteDiskFileForTask(undefined)).toBe(false);
  });

  it('returns false for completed task', () => {
    const completedTask = createTask('1', task.Status.StatusCompleted);
    expect(shouldDefaultDeleteDiskFileForTask(completedTask)).toBe(false);
  });

  it('returns true for incomplete tasks', () => {
    expect(shouldDefaultDeleteDiskFileForTask(createTask('2', task.Status.StatusDownloading))).toBe(
      true,
    );
    expect(shouldDefaultDeleteDiskFileForTask(createTask('3', task.Status.StatusPaused))).toBe(
      true,
    );
    expect(shouldDefaultDeleteDiskFileForTask(createTask('4', task.Status.StatusQueued))).toBe(
      true,
    );
    expect(shouldDefaultDeleteDiskFileForTask(createTask('5', task.Status.StatusError))).toBe(true);
  });
});

describe('shouldDefaultDeleteDiskFileForBatch', () => {
  it('returns false for empty task list', () => {
    expect(shouldDefaultDeleteDiskFileForBatch([])).toBe(false);
  });

  it('returns false when all tasks are completed', () => {
    const list = [
      createTask('1', task.Status.StatusCompleted),
      createTask('2', task.Status.StatusCompleted),
    ];
    expect(shouldDefaultDeleteDiskFileForBatch(list)).toBe(false);
  });

  it('returns false when batch contains a completed task alongside incomplete ones', () => {
    const list = [
      createTask('1', task.Status.StatusDownloading),
      createTask('2', task.Status.StatusCompleted),
      createTask('3', task.Status.StatusError),
    ];
    expect(shouldDefaultDeleteDiskFileForBatch(list)).toBe(false);
  });

  it('returns true when all tasks in batch are incomplete', () => {
    const list = [
      createTask('1', task.Status.StatusDownloading),
      createTask('2', task.Status.StatusPaused),
      createTask('3', task.Status.StatusError),
    ];
    expect(shouldDefaultDeleteDiskFileForBatch(list)).toBe(true);
  });
});
