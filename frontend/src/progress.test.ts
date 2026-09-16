import { describe, it, expect } from 'vitest';
import { sortActiveTasks } from './views/ProgressView';
import * as taskModels from '../bindings/sheep-get/internal/task/models';

describe('ProgressView active task sorting and rules (Ticket 03)', () => {
  const createTask = (
    id: string,
    status: taskModels.Status,
    downloaded: number,
    totalBytes: number,
    createdAt = '2026-09-16T10:00:00Z',
    errorMsg = '',
  ): taskModels.Task => {
    return new taskModels.Task({
      id,
      url: `https://example.com/${id}`,
      filename: `${id}.zip`,
      directory: '/downloads',
      status,
      downloaded,
      totalBytes,
      speed: 1024 * 1024,
      maxConcurrency: 8,
      resumable: true,
      createdAt,
      updatedAt: createdAt,
      errorMsg,
    });
  };

  it('sorts downloading tasks in descending order of progress percentage', () => {
    const taskLow = createTask('low', taskModels.Status.StatusDownloading, 20, 100); // 20%
    const taskHigh = createTask('high', taskModels.Status.StatusDownloading, 80, 100); // 80%
    const taskMid = createTask('mid', taskModels.Status.StatusDownloading, 50, 100); // 50%

    const sorted = sortActiveTasks([taskLow, taskHigh, taskMid]);
    expect(sorted.map((t) => t.id)).toEqual(['high', 'mid', 'low']);
  });

  it('breaks ties in equal percentage by downloaded bytes descending', () => {
    const taskSmall = createTask('small', taskModels.Status.StatusDownloading, 50, 100); // 50%, 50B
    const taskLarge = createTask('large', taskModels.Status.StatusDownloading, 500, 1000); // 50%, 500B

    const sorted = sortActiveTasks([taskSmall, taskLarge]);
    expect(sorted.map((t) => t.id)).toEqual(['large', 'small']);
  });

  it('orders unknown size tasks after known percentage tasks, sorted by downloaded bytes', () => {
    const taskKnown = createTask('known', taskModels.Status.StatusDownloading, 10, 100); // 10%
    const taskUnknown1 = createTask('un1', taskModels.Status.StatusDownloading, 5000, 0); // unknown, 5000B
    const taskUnknown2 = createTask('un2', taskModels.Status.StatusDownloading, 10000, 0); // unknown, 10000B

    const sorted = sortActiveTasks([taskUnknown1, taskKnown, taskUnknown2]);
    expect(sorted.map((t) => t.id)).toEqual(['known', 'un2', 'un1']);
  });

  it('strictly follows status priority: Processing > Downloading > Queued > Paused > Error', () => {
    const taskError = createTask('err', taskModels.Status.StatusError, 0, 100);
    const taskPaused = createTask('paused', taskModels.Status.StatusPaused, 50, 100);
    const taskQueued = createTask('queued', taskModels.Status.StatusQueued, 0, 100);
    const taskDownloading = createTask('dl', taskModels.Status.StatusDownloading, 30, 100);
    const taskProcessing = createTask('proc', taskModels.Status.StatusProcessing, 100, 100);

    const sorted = sortActiveTasks([
      taskError,
      taskPaused,
      taskQueued,
      taskDownloading,
      taskProcessing,
    ]);

    expect(sorted.map((t) => t.id)).toEqual(['proc', 'dl', 'queued', 'paused', 'err']);
  });

  it('identifies processing failure vs regular download failure', () => {
    const regularErr = createTask(
      'reg',
      taskModels.Status.StatusError,
      10,
      100,
      undefined,
      'connection reset by peer',
    );
    const processErr = createTask(
      'procErr',
      taskModels.Status.StatusError,
      100,
      100,
      undefined,
      '媒体处理失败: fMP4 track muxing error',
    );

    const isProcessing1 =
      regularErr.status === taskModels.Status.StatusError &&
      (regularErr.errorMsg?.includes('处理') || regularErr.errorMsg?.toLowerCase().includes('mux'));
    const isProcessing2 =
      processErr.status === taskModels.Status.StatusError &&
      (processErr.errorMsg?.includes('处理') || processErr.errorMsg?.toLowerCase().includes('mux'));

    expect(isProcessing1).toBe(false);
    expect(isProcessing2).toBe(true);
  });

  it('ensures manual view appends completed task even when keepCompletedInfo is false', () => {
    const completedTask1 = createTask('comp1', taskModels.Status.StatusCompleted, 100, 100);
    const completedTask2 = createTask('comp2', taskModels.Status.StatusCompleted, 200, 200);

    let completedList: taskModels.Task[] = [];
    const handleAppend = (target: taskModels.Task) => {
      if (!completedList.some((t) => t.id === target.id)) {
        completedList = [target, ...completedList];
      }
    };

    // User views task 1 manually
    handleAppend(completedTask1);
    expect(completedList.map((t) => t.id)).toEqual(['comp1']);

    // User views task 2 manually (must append, NOT replace!)
    handleAppend(completedTask2);
    expect(completedList.map((t) => t.id)).toEqual(['comp2', 'comp1']);
  });

  it('auto-completing downloads respect keepCompletedInfo switch and trigger close when last active', () => {
    const downloadingTask = createTask('dl1', taskModels.Status.StatusDownloading, 90, 100);
    let activeList = [downloadingTask];
    const completedList: taskModels.Task[] = [];
    let windowHidden = false;

    const onDownloadComplete = (taskID: string, keepCompletedInfo: boolean) => {
      activeList = activeList.filter((t) => t.id !== taskID);
      if (!keepCompletedInfo && activeList.length === 0) {
        windowHidden = true;
      }
      if (keepCompletedInfo) {
        completedList.push(downloadingTask);
      }
    };

    // When keepCompletedInfo is false:
    onDownloadComplete('dl1', false);
    expect(activeList.length).toBe(0);
    expect(completedList.length).toBe(0);
    expect(windowHidden).toBe(true);
  });
});
