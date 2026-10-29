import { afterEach, describe, expect, it, vi } from 'vitest';
import { createResizeReporter } from './lib/resizeReporter';

describe('createResizeReporter', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('把间隔内的连续高度合并成一次调用，并送达最后一个高度', () => {
    vi.useFakeTimers();
    const calls: number[] = [];
    const reporter = createResizeReporter((height) => calls.push(height), 60);

    reporter.push(100);
    reporter.push(120);
    reporter.push(140);
    expect(calls).toEqual([]);

    vi.advanceTimersByTime(60);
    expect(calls).toEqual([140]);

    reporter.push(160);
    vi.advanceTimersByTime(60);
    expect(calls).toEqual([140, 160]);

    reporter.dispose();
  });

  it('一个间隔内只调度一次定时器，不会被逐帧变化推后', () => {
    vi.useFakeTimers();
    const calls: number[] = [];
    const reporter = createResizeReporter((height) => calls.push(height), 60);

    reporter.push(100);
    vi.advanceTimersByTime(30);
    reporter.push(120);
    vi.advanceTimersByTime(30);
    expect(calls).toEqual([120]);

    reporter.dispose();
  });

  it('dispose 丢弃尚未送达的高度', () => {
    vi.useFakeTimers();
    const calls: number[] = [];
    const reporter = createResizeReporter((height) => calls.push(height), 60);

    reporter.push(200);
    reporter.dispose();
    vi.advanceTimersByTime(200);

    expect(calls).toEqual([]);
  });
});
