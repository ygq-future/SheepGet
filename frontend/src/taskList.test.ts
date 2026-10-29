import { describe, expect, it } from 'vitest';
import { applyTaskUpdate, sortTasks, taskSortTime } from './lib/taskList';
import * as taskModels from '../bindings/sheep-get/internal/task/models';

function makeTask(id: string, createdAt: string, updatedAt = createdAt): taskModels.Task {
  return new taskModels.Task({
    id,
    url: `https://example.com/${id}`,
    filename: `${id}.zip`,
    directory: '/downloads',
    status: taskModels.Status.StatusDownloading,
    downloaded: 1,
    totalBytes: 10,
    createdAt,
    updatedAt,
  });
}

describe('sortTasks', () => {
  it('按最后连接时间倒序，时间相同时按 id 倒序', () => {
    const older = makeTask('a', '2026-09-01T00:00:00Z');
    const newer = makeTask('b', '2026-09-02T00:00:00Z');
    const sameAsNewer = makeTask('c', '2026-09-02T00:00:00Z');

    const sorted = sortTasks([older, newer, sameAsNewer]);
    expect(sorted.map((t) => t.id)).toEqual(['c', 'b', 'a']);
  });

  it('没有 updatedAt 的老记录退回创建时间', () => {
    const legacy = makeTask('legacy', '2026-09-03T00:00:00Z', '');
    const resumed = makeTask('resumed', '2026-09-01T00:00:00Z', '2026-09-04T00:00:00Z');

    const sorted = sortTasks([legacy, resumed]);
    expect(sorted.map((t) => t.id)).toEqual(['resumed', 'legacy']);
  });
});

describe('applyTaskUpdate', () => {
  const a = makeTask('a', '2026-09-01T00:00:00Z');
  const b = makeTask('b', '2026-09-02T00:00:00Z');
  const c = makeTask('c', '2026-09-03T00:00:00Z');

  it('排序键没变时只替换那一项，位置不动', () => {
    const updated = new taskModels.Task({ ...b, downloaded: 5, speed: 1024 });
    expect(taskSortTime(updated)).toBe(taskSortTime(b));

    const next = applyTaskUpdate([c, b, a], updated);
    expect(next.map((t) => t.id)).toEqual(['c', 'b', 'a']);
    expect(next[1]).toBe(updated);
    // 其余项仍是同一批对象：任务项不必因为一条进度事件重渲染。
    expect(next[0]).toBe(c);
    expect(next[2]).toBe(a);
  });

  it('最后连接时间更新后任务浮到最前', () => {
    const resumed = makeTask('a', '2026-09-01T00:00:00Z', '2026-09-05T00:00:00Z');
    const next = applyTaskUpdate([c, b, a], resumed);
    expect(next.map((t) => t.id)).toEqual(['a', 'c', 'b']);
  });

  it('列表里没有的任务按排序键插进列表', () => {
    const fresh = makeTask('fresh', '2026-09-06T00:00:00Z');
    const next = applyTaskUpdate([c, b], fresh);
    expect(next.map((t) => t.id)).toEqual(['fresh', 'c', 'b']);
  });
});
