import { describe, it, expect } from 'vitest';
import * as task from '../bindings/sheep-get/internal/task/models';
import * as configModels from '../bindings/sheep-get/internal/config/models';
import { filterTasks } from './lib/taskFilter';

describe('filterTasks', () => {
  const dummyTasks: task.Task[] = [
    new task.Task({
      id: 'task-1',
      url: 'https://example.com/video.mp4',
      filename: 'video.mp4',
      status: task.Status.StatusDownloading,
    }),
    new task.Task({
      id: 'task-2',
      url: 'https://example.com/audio.mp3',
      filename: 'audio.mp3',
      status: task.Status.StatusPaused,
    }),
    new task.Task({
      id: 'task-3',
      url: 'https://example.com/doc.pdf',
      filename: 'doc.pdf',
      status: task.Status.StatusCompleted,
    }),
    new task.Task({
      id: 'task-4',
      url: 'https://example.com/archive.zip',
      filename: 'archive.zip',
      status: task.Status.StatusError,
    }),
    new task.Task({
      id: 'task-5',
      url: 'https://example.com/broken.mp4',
      filename: 'broken.mp4',
      status: task.Status.StatusError,
    }),
  ];

  const mockSettings = new configModels.Settings({
    download: new configModels.DownloadConfig({
      builtinCategories: [
        new configModels.CategoryConfig({
          id: 'builtin-video',
          name: '视频',
          directory: '/dl/video',
          extensions: ['mp4'],
          isBuiltin: true,
        }),
      ],
      customCategories: [],
    }),
  });

  it('filters all tasks when filter is all', () => {
    const res = filterTasks(dummyTasks, 'all', '', null);
    expect(res.map((t) => t.id)).toEqual(['task-1', 'task-2', 'task-3', 'task-4', 'task-5']);
  });

  it('filters paused tasks only', () => {
    const res = filterTasks(dummyTasks, 'paused', '', null);
    expect(res.map((t) => t.id)).toEqual(['task-2']);
  });

  it('filters completed tasks only', () => {
    const res = filterTasks(dummyTasks, 'completed', '', null);
    expect(res.map((t) => t.id)).toEqual(['task-3']);
  });

  it('filters error tasks only', () => {
    const res = filterTasks(dummyTasks, 'error', '', null);
    expect(res.map((t) => t.id)).toEqual(['task-4', 'task-5']);
  });

  it('filters error tasks with category constraint', () => {
    const res = filterTasks(dummyTasks, 'error', 'builtin-video', mockSettings);
    expect(res.map((t) => t.id)).toEqual(['task-5']);
  });

  it('returns empty array if no tasks match error filter', () => {
    const nonErrorTasks = dummyTasks.slice(0, 3);
    const res = filterTasks(nonErrorTasks, 'error', '', null);
    expect(res).toEqual([]);
  });
});
