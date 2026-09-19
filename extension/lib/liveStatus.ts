import type { DesktopStatus } from './types';

/**
 * 面板打开期间重新确认链路状态的间隔。
 *
 * 面板显示的状态必须是「此刻能不能连上桌面端」，不能是打开那一刻的答案：桌面端可能中途退出，
 * 也可能在面板开着的时候被启动。只有打开面板时查一次，退出桌面端后面板会一直显示「已连接」，
 * 用户必须关掉再点开才能看到真实状态。
 */
export const STATUS_POLL_MS = 3000;

export interface StatusWatchOptions {
  /** 取一次真实状态：会真的验证链路，不是读上次的结果。 */
  probe: () => Promise<DesktopStatus | null>;
  onStatus: (status: DesktopStatus | null) => void;
  /** 默认 STATUS_POLL_MS；测试用更短的间隔。 */
  intervalMs?: number;
}

/** watchDesktopStatus 返回的句柄：stop 之后不再查询，也不再往界面写结果。 */
export interface StatusWatch {
  stop: () => void;
}

/**
 * 在面板打开期间按固定间隔重新确认链路状态。
 *
 * 上一次查询有结果之后才排下一次，因此某次验证变慢不会堆出并发的探测。第一个查询也等一个间隔：
 * 面板刚打开那一次由调用方自己带指示地做（用户需要看到「正在检查」），这里的查询只负责之后
 * 的静默刷新。
 */
export function watchDesktopStatus(options: StatusWatchOptions): StatusWatch {
  const interval = options.intervalMs ?? STATUS_POLL_MS;
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | null = null;

  const tick = async () => {
    if (stopped) return;
    const status = await options.probe();
    // 面板可能已经关掉：这一次的结果不再有意义，也不能再去排下一次。
    if (stopped) return;
    options.onStatus(status);
    timer = setTimeout(() => void tick(), interval);
  };

  timer = setTimeout(() => void tick(), interval);

  return {
    stop: () => {
      stopped = true;
      if (timer !== null) {
        clearTimeout(timer);
        timer = null;
      }
    },
  };
}
