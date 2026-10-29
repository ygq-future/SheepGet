/**
 * 把连续的高度变化合并成对原生窗口的少量调用。
 *
 * 内容高度由动画与滚动逐帧变化，ResizeObserver 会跟着逐帧触发；直接每次都调 Wails 的
 * SetXWindowHeight 等于逐帧做一次原生窗口 resize。这里按固定间隔合并，并保证最后一个高度
 * 一定送达——窗口最终停在内容真实高度上。
 */
export function createResizeReporter(report: (height: number) => void, intervalMs = 60) {
  let pending: number | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const flush = () => {
    timer = undefined;
    if (pending === undefined) {
      return;
    }
    const height = pending;
    pending = undefined;
    report(height);
  };

  return {
    /** 记下最新高度；一个间隔内只会真正调用一次 report。 */
    push(height: number) {
      pending = height;
      if (timer === undefined) {
        timer = setTimeout(flush, intervalMs);
      }
    },
    /** 停止观察（窗口关闭/组件卸载）：丢弃尚未送达的高度。 */
    dispose() {
      clearTimeout(timer);
      timer = undefined;
      pending = undefined;
    },
  };
}
