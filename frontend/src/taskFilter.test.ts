import { describe, it, expect } from 'vitest';
import * as task from '../bindings/sheep-get/internal/task/models';
import { filterTasks } from './lib/taskFilter';

describe('filterTasks', () => {
  const dummyTasks: task.Task[] = [
    new task.Task({
      id: 'task-1',
      url: 'https://example.com/video.mp4',
      filename: 'video.mp4',
      status: task.Status.StatusDownloading,
      categoryId: 'builtin-video',
    }),
    new task.Task({
      id: 'task-2',
      url: 'https://example.com/audio.mp3',
      filename: 'audio.mp3',
      status: task.Status.StatusPaused,
      categoryId: 'builtin-audio',
    }),
    new task.Task({
      id: 'task-3',
      url: 'https://example.com/doc.pdf',
      filename: 'doc.pdf',
      status: task.Status.StatusCompleted,
      categoryId: 'builtin-file',
    }),
    new task.Task({
      id: 'task-4',
      url: 'https://example.com/archive.zip',
      filename: 'archive.zip',
      status: task.Status.StatusError,
      categoryId: 'builtin-archive',
    }),
    new task.Task({
      id: 'task-5',
      url: 'https://example.com/broken.mp4',
      filename: 'broken.mp4',
      status: task.Status.StatusError,
      categoryId: 'builtin-video',
    }),
  ];

  it('filters all tasks when filter is all', () => {
    const res = filterTasks(dummyTasks, 'all', '');
    expect(res.map((t) => t.id)).toEqual(['task-1', 'task-2', 'task-3', 'task-4', 'task-5']);
  });

  it('filters paused tasks only', () => {
    const res = filterTasks(dummyTasks, 'paused', '');
    expect(res.map((t) => t.id)).toEqual(['task-2']);
  });

  it('filters completed tasks only', () => {
    const res = filterTasks(dummyTasks, 'completed', '');
    expect(res.map((t) => t.id)).toEqual(['task-3']);
  });

  it('filters error tasks only', () => {
    const res = filterTasks(dummyTasks, 'error', '');
    expect(res.map((t) => t.id)).toEqual(['task-4', 'task-5']);
  });

  it('filters error tasks with category constraint', () => {
    const res = filterTasks(dummyTasks, 'error', 'builtin-video');
    expect(res.map((t) => t.id)).toEqual(['task-5']);
  });

  it('filters by categoryId accurately (historical attribution)', () => {
    const tasksWithCategory: task.Task[] = [
      new task.Task({
        id: 'task-c1',
        filename: 'movie.mp4',
        categoryId: 'custom-special',
        status: task.Status.StatusDownloading,
      }),
      new task.Task({
        id: 'task-c2',
        filename: 'clip.mp4',
        categoryId: 'builtin-video',
        status: task.Status.StatusDownloading,
      }),
    ];

    const specialRes = filterTasks(tasksWithCategory, 'all', 'custom-special');
    expect(specialRes.map((t) => t.id)).toEqual(['task-c1']);

    const videoRes = filterTasks(tasksWithCategory, 'all', 'builtin-video');
    expect(videoRes.map((t) => t.id)).toEqual(['task-c2']);
  });

  it('returns empty array if no tasks match error filter', () => {
    const nonErrorTasks = dummyTasks.slice(0, 3);
    const res = filterTasks(nonErrorTasks, 'error', '');
    expect(res).toEqual([]);
  });
});
