import type { HandoverFailureKind } from './types';

/**
 * 交接失败之后的处置策略，以及「什么时候必须重新验证链路」的判定。
 *
 * 桌面端每次启动都会换一个 loopback 端口（ADR-0005），所以「扩展存过会话」并不等于
 * 「扩展连得上」。曾经的现象：桌面端重启后扩展仍拿着旧端口的会话去交接，失败后把下载
 * 还给浏览器，而 Chrome 自己的下载界面又按「已连接」压着，于是用户看到的是一次
 * 完全没有反馈的下载。这里把两件必须显式判断的事做成纯函数，便于直接测。
 */

export type HandoverFailurePlan = 'retry_after_rediscover' | 'give_back_to_browser';

/**
 * 只有「能证明请求没被受理」的失败才允许重试，否则重试可能造出两个同样的任务：
 *  - not_delivered：连接没建立，一定没到达 → 重新发现会话后重试；
 *  - unauthorized：到达了但令牌过期，说明桌面端已换会话 → 重新发现后重试；
 *  - no_response：请求可能已被受理，只是响应没回来 → 不重试，还给浏览器；
 *  - rejected：桌面端明确拒绝（如重复链接策略）→ 不重试，还给浏览器。
 */
const FAILURE_PLANS: Record<HandoverFailureKind, HandoverFailurePlan> = {
  not_delivered: 'retry_after_rediscover',
  unauthorized: 'retry_after_rediscover',
  no_response: 'give_back_to_browser',
  rejected: 'give_back_to_browser',
};

export function planHandoverFailure(kind: HandoverFailureKind): HandoverFailurePlan {
  return FAILURE_PLANS[kind];
}

/**
 * 链路「上次被证实可用」到现在的最大容忍时长；超过它就重新验证一次。
 * 取值与保活定时器周期一致：即使 WebSocket 关闭事件因为任何原因没送到，链路状态也有
 * 最多 30 秒的陈旧上界。
 */
export const LINK_VERIFY_TTL_MS = 30_000;

export interface LinkLiveness {
  online: boolean;
  lastVerifiedAt: number | null;
}

export function shouldReverifyLink(liveness: LinkLiveness, now: number, ttlMs: number): boolean {
  if (!liveness.online) return true;
  if (liveness.lastVerifiedAt === null) return true;
  return now - liveness.lastVerifiedAt >= ttlMs;
}
