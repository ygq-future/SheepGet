import { describe, expect, it } from 'vitest';
import {
  PROGRESS_WINDOW_CHROME_H,
  PROGRESS_WINDOW_MAX_H,
  progressWindowHeightFor,
} from './lib/windowSize';

describe('progressWindowHeightFor', () => {
  it('只有一个任务卡片时按内容高度收起，不撑到旧的下限 160px', () => {
    // 单张卡片约 66px 内容高：窗口应该是一张卡片加固定开销，而不是被顶到 160px 留下空白。
    const single = progressWindowHeightFor(66);
    expect(single).toBe(66 + PROGRESS_WINDOW_CHROME_H);
    expect(single).toBeLessThan(160);
  });

  it('没有内容时只留固定开销，由调用方决定是否显示窗口', () => {
    expect(progressWindowHeightFor(0)).toBe(PROGRESS_WINDOW_CHROME_H);
  });

  it('内容高超过上限时封顶，多出来的部分由内容区滚动', () => {
    expect(progressWindowHeightFor(1000)).toBe(PROGRESS_WINDOW_MAX_H);
    expect(progressWindowHeightFor(PROGRESS_WINDOW_MAX_H)).toBe(PROGRESS_WINDOW_MAX_H);
  });

  it('小数内容高度向上取整，避免窗口比内容矮一点导致出现滚动条', () => {
    expect(progressWindowHeightFor(65.2)).toBe(66 + PROGRESS_WINDOW_CHROME_H);
  });
});
