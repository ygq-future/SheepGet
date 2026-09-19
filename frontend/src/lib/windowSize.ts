/**
 * 进度窗口的高度换算。
 *
 * 面板为空时窗口根本不显示，所以内容多少就多高：只有一个任务卡片时窗口应该收成一张卡片的高度，
 * 不设下限。只保留上限，超过上限的部分交给内容区自己滚动，窗口不再继续变高。
 */

/** 内容区之外的固定开销：标题栏 32px + 内容区内边距 24px + 边框 2px。 */
export const PROGRESS_WINDOW_CHROME_H = 58;

/** 高度上限，与 Go 侧 appconsts.go 的 progressWindowMaxH 保持一致。 */
export const PROGRESS_WINDOW_MAX_H = 640;

/** 由内容区自然高度换算出窗口高度。 */
export function progressWindowHeightFor(contentHeight: number): number {
  const target = Math.ceil(contentHeight) + PROGRESS_WINDOW_CHROME_H;
  return Math.min(PROGRESS_WINDOW_MAX_H, target);
}
