/**
 * 把高频触发合并成受控频率的执行。
 *
 * DOM 观察器在弹幕、聊天、虚拟列表这类页面上每秒能抛出成百上千条变更记录；每条都同步做一次
 * 全页扫描会持续占满主线程。这里保证：第一次触发立即执行，随后每个窗口内最多执行一次，
 * 窗口期间的新触发会合并成一次尾随执行——既不会漏掉最后一个变更，也不会被持续变更饿死。
 */
export function createThrottledRunner(run: () => void, intervalMs: number) {
  let lastRun = Number.NEGATIVE_INFINITY;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const fire = () => {
    lastRun = Date.now();
    run();
  };

  return {
    trigger() {
      if (timer !== undefined) {
        // 已经有尾随执行排着，它会带上这期间的变更。
        return;
      }
      const elapsed = Date.now() - lastRun;
      if (elapsed >= intervalMs) {
        fire();
        return;
      }
      timer = setTimeout(() => {
        timer = undefined;
        fire();
      }, intervalMs - elapsed);
    },
    /** 取消尚未执行的尾随调用（页面卸载/管理器停止）。 */
    cancel() {
      clearTimeout(timer);
      timer = undefined;
    },
  };
}
