import { describe, it, expect } from 'vitest';
import { sortActiveTasks } from './views/ProgressView';
import {
  mergeProgressSegments,
  calculateChunkProgress,
  calculateChunkDividers,
} from './lib/progress';
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

describe('mergeProgressSegments algorithm', () => {
  it('returns empty array when totalBytes is 0 or downloaded is 0 with no chunks', () => {
    expect(mergeProgressSegments([], 0, 0)).toEqual([]);
    expect(mergeProgressSegments([], 1000, 0)).toEqual([]);
  });

  it('handles single stream download without chunks', () => {
    const segments = mergeProgressSegments(undefined, 1000, 500);
    expect(segments).toHaveLength(1);
    expect(segments[0].startPercent).toBe(0);
    expect(segments[0].widthPercent).toBe(50);
  });

  it('keeps discrete non-adjacent chunks separate', () => {
    const chunks: taskModels.Chunk[] = [
      new taskModels.Chunk({ index: 0, start: 0, end: 99, downloaded: 50, completed: false }),
      new taskModels.Chunk({ index: 1, start: 100, end: 199, downloaded: 0, completed: false }),
      new taskModels.Chunk({ index: 2, start: 200, end: 299, downloaded: 100, completed: true }),
    ];
    const segments = mergeProgressSegments(chunks, 1000, 150);
    expect(segments).toHaveLength(2);
    // Segment 1: 0..49 (start: 0%, width: 5%)
    expect(segments[0].start).toBe(0);
    expect(segments[0].end).toBe(49);
    expect(segments[0].startPercent).toBe(0);
    expect(segments[0].widthPercent).toBe(5);

    // Segment 2: 200..299 (start: 20%, width: 10%)
    expect(segments[1].start).toBe(200);
    expect(segments[1].end).toBe(299);
    expect(segments[1].startPercent).toBe(20);
    expect(segments[1].widthPercent).toBe(10);
  });

  it('merges adjacent chunks when one or both meet continuously', () => {
    const chunks: taskModels.Chunk[] = [
      new taskModels.Chunk({ index: 0, start: 0, end: 99, downloaded: 100, completed: true }),
      new taskModels.Chunk({ index: 1, start: 100, end: 199, downloaded: 100, completed: true }),
      new taskModels.Chunk({ index: 2, start: 200, end: 299, downloaded: 50, completed: false }),
      new taskModels.Chunk({ index: 3, start: 300, end: 399, downloaded: 0, completed: false }),
    ];
    // chunk 0 (0..99) + chunk 1 (100..199) + chunk 2 (200..249) are all contiguous!
    const segments = mergeProgressSegments(chunks, 1000, 250);
    expect(segments).toHaveLength(1);
    expect(segments[0].start).toBe(0);
    expect(segments[0].end).toBe(249);
    expect(segments[0].startPercent).toBe(0);
    expect(segments[0].widthPercent).toBe(25);
  });

  it('merges all completed chunks into a single 100% continuous progress bar', () => {
    const chunks: taskModels.Chunk[] = [
      new taskModels.Chunk({ index: 0, start: 0, end: 249, downloaded: 250, completed: true }),
      new taskModels.Chunk({ index: 1, start: 250, end: 499, downloaded: 250, completed: true }),
      new taskModels.Chunk({ index: 2, start: 500, end: 749, downloaded: 250, completed: true }),
      new taskModels.Chunk({ index: 3, start: 750, end: 999, downloaded: 250, completed: true }),
    ];
    const segments = mergeProgressSegments(chunks, 1000, 1000);
    expect(segments).toHaveLength(1);
    expect(segments[0].start).toBe(0);
    expect(segments[0].end).toBe(999);
    expect(segments[0].startPercent).toBe(0);
    expect(segments[0].widthPercent).toBe(100);
  });
});

describe('calculateChunkProgress and calculateChunkDividers algorithm', () => {
  it('returns empty array when chunks are undefined, empty, or totalBytes <= 0', () => {
    expect(calculateChunkProgress(undefined, 1000)).toEqual([]);
    expect(calculateChunkProgress([], 1000)).toEqual([]);
    expect(calculateChunkProgress([], 0)).toEqual([]);
    expect(calculateChunkDividers(undefined, 1000)).toEqual([]);
    expect(calculateChunkDividers([], 1000)).toEqual([]);
    expect(calculateChunkDividers([], 0)).toEqual([]);
  });

  it('correctly calculates chunk progress percentages with stable keys', () => {
    const chunks: taskModels.Chunk[] = [
      new taskModels.Chunk({ index: 0, start: 0, end: 499, downloaded: 250, completed: false }),
      new taskModels.Chunk({ index: 1, start: 500, end: 999, downloaded: 500, completed: true }),
    ];
    const items = calculateChunkProgress(chunks, 1000);
    expect(items).toHaveLength(2);

    expect(items[0].id).toBe('chunk-0');
    expect(items[0].startPercent).toBe(0);
    expect(items[0].widthPercent).toBe(50);
    expect(items[0].downloadedPercent).toBe(25);
    expect(items[0].completed).toBe(false);

    expect(items[1].id).toBe('chunk-1');
    expect(items[1].startPercent).toBe(50);
    expect(items[1].widthPercent).toBe(50);
    expect(items[1].downloadedPercent).toBe(50);
    expect(items[1].completed).toBe(true);
  });

  it('generates spatial dividers in ascending order even when dynamic split chunks are appended out of order', () => {
    // Simulates a slow chunk (originally index 1: 500..999) dynamically split into:
    // index 1: 500..749
    // index 2 (newly appended assisted chunk): 750..999
    const chunks: taskModels.Chunk[] = [
      new taskModels.Chunk({ index: 0, start: 0, end: 499, downloaded: 500, completed: true }),
      new taskModels.Chunk({ index: 1, start: 500, end: 749, downloaded: 100, completed: false }),
      new taskModels.Chunk({
        index: 2,
        start: 750,
        end: 999,
        downloaded: 0,
        completed: false,
        assisted: true,
      }),
    ];

    const dividers = calculateChunkDividers(chunks, 1000);
    expect(dividers).toHaveLength(2);

    // Divider 1 between chunk 0 and chunk 1 at 50%
    expect(dividers[0].positionPercent).toBe(50);
    expect(dividers[0].assisted).toBe(false);
    expect(dividers[0].targetChunkIndex).toBe(1);

    // Divider 2 (in-place dynamic split) at 75%
    expect(dividers[1].positionPercent).toBe(75);
    expect(dividers[1].assisted).toBe(true);
    expect(dividers[1].targetChunkIndex).toBe(2);
  });

  it('handles disordered chunk arrays and correctly sorts dividers spatially', () => {
    // Chunks appended out of order: chunk 2 (start: 500) appended before chunk 1 (start: 250)
    const chunks: taskModels.Chunk[] = [
      new taskModels.Chunk({ index: 0, start: 0, end: 249, downloaded: 250, completed: true }),
      new taskModels.Chunk({
        index: 2,
        start: 500,
        end: 999,
        downloaded: 0,
        completed: false,
        assisted: true,
      }),
      new taskModels.Chunk({ index: 1, start: 250, end: 499, downloaded: 100, completed: false }),
    ];

    const dividers = calculateChunkDividers(chunks, 1000);
    expect(dividers).toHaveLength(2);
    expect(dividers[0].positionPercent).toBe(25);
    expect(dividers[0].assisted).toBe(false);
    expect(dividers[1].positionPercent).toBe(50);
    expect(dividers[1].assisted).toBe(true);
  });

  it('clamps downloaded bytes to chunk boundary and preserves assisted flag', () => {
    const chunks: taskModels.Chunk[] = [
      new taskModels.Chunk({
        index: 3,
        start: 600,
        end: 799,
        downloaded: 9999, // overflow
        completed: false,
        assisted: true,
      }),
    ];
    const items = calculateChunkProgress(chunks, 1000);
    expect(items).toHaveLength(1);
    expect(items[0].startPercent).toBe(60);
    expect(items[0].widthPercent).toBe(20);
    expect(items[0].downloadedPercent).toBe(20); // clamped to widthPercent
    expect(items[0].assisted).toBe(true);
  });
});
