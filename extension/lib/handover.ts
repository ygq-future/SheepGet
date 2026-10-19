import type {
  Aria2Config,
  HandoverFailureKind,
  HandoverRequest,
  HandoverResponse,
  WebhookConfig,
} from './types';

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

/** 生成携带完整请求凭据（Referer、UA、Cookie）的标准 cURL 下载命令。 */
export function buildCurlCommand(req: HandoverRequest): string {
  const parts: string[] = ['curl', '-L'];
  if (req.filenameSuggestion) {
    parts.push('-o', JSON.stringify(req.filenameSuggestion));
  }
  const headers = req.credentials?.headers || {};
  for (const [key, value] of Object.entries(headers)) {
    if (value) {
      parts.push('-H', JSON.stringify(`${key}: ${value}`));
    }
  }
  if (req.credentials?.cookies) {
    parts.push('-H', JSON.stringify(`Cookie: ${req.credentials.cookies}`));
  }
  parts.push(JSON.stringify(req.url));
  return parts.join(' ');
}

/** 使用 aria2.getVersion 轻量探活（超时 800ms）。 */
export async function probeAria2Liveness(
  config: Aria2Config,
  fetchFn: typeof fetch = fetch,
): Promise<boolean> {
  const rpcUrl = config.rpcUrl?.trim() || 'http://127.0.0.1:6800/jsonrpc';
  const params: unknown[] = [];
  if (config.secret?.trim()) {
    params.push(`token:${config.secret.trim()}`);
  }
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 800);
  try {
    const res = await fetchFn(rpcUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        jsonrpc: '2.0',
        id: 'probe',
        method: 'aria2.getVersion',
        params,
      }),
      signal: controller.signal,
    });
    clearTimeout(timer);
    if (!res.ok) return false;
    const data = (await res.json()) as { error?: unknown; result?: { version?: string } };
    return !data.error && Boolean(data.result?.version);
  } catch {
    clearTimeout(timer);
    return false;
  }
}

/** 向 Aria2 / Motrix JSON-RPC 服务投递下载任务。 */
export async function sendAria2Handover(
  req: HandoverRequest,
  config: Aria2Config,
  fetchFn: typeof fetch = fetch,
): Promise<HandoverResponse> {
  const rpcUrl = config.rpcUrl?.trim() || 'http://127.0.0.1:6800/jsonrpc';
  const params: unknown[] = [];
  if (config.secret?.trim()) {
    params.push(`token:${config.secret.trim()}`);
  }
  params.push([req.url]);

  const options: Record<string, unknown> = {};
  if (req.filenameSuggestion) {
    options.out = req.filenameSuggestion;
  }
  if (config.dir?.trim()) {
    options.dir = config.dir.trim();
  }
  const headerList: string[] = [];
  if (req.credentials?.headers) {
    for (const [k, v] of Object.entries(req.credentials.headers)) {
      if (v) headerList.push(`${k}: ${v}`);
    }
  }
  if (req.credentials?.cookies) {
    headerList.push(`Cookie: ${req.credentials.cookies}`);
  }
  if (headerList.length > 0) {
    options.header = headerList;
  }
  params.push(options);

  const payload = {
    jsonrpc: '2.0',
    id: `sheepget_${Date.now()}`,
    method: 'aria2.addUri',
    params,
  };

  try {
    const res = await fetchFn(rpcUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!res.ok) {
      return {
        accepted: false,
        failure: 'rejected',
        reason: `Aria2 RPC returned HTTP ${res.status}`,
      };
    }
    const data = (await res.json()) as { error?: { message?: string }; result?: string };
    if (data.error) {
      return {
        accepted: false,
        failure: 'rejected',
        reason: data.error.message || 'Aria2 RPC error',
      };
    }
    return {
      accepted: true,
      queueItemId: data.result,
    };
  } catch (err) {
    return {
      accepted: false,
      failure: 'not_delivered',
      reason: `Cannot reach Aria2: ${String(err)}`,
    };
  }
}

/** 向通用 HTTP Webhook (REST API) 投递 HandoverRequest 数据。 */
export async function sendWebhookHandover(
  req: HandoverRequest,
  config: WebhookConfig,
  fetchFn: typeof fetch = fetch,
): Promise<HandoverResponse> {
  const url = config.url?.trim();
  if (!url) {
    return {
      accepted: false,
      failure: 'rejected',
      reason: 'Webhook URL 未配置',
    };
  }

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(config.customHeaders || {}),
  };
  if (config.token?.trim()) {
    const t = config.token.trim();
    headers.Authorization = t.toLowerCase().startsWith('bearer ') ? t : `Bearer ${t}`;
  }

  try {
    const res = await fetchFn(url, {
      method: config.method || 'POST',
      headers,
      body: JSON.stringify(req),
    });
    if (!res.ok) {
      return {
        accepted: false,
        failure: 'rejected',
        reason: `Webhook returned HTTP ${res.status}`,
      };
    }
    let queueItemId: string | undefined;
    try {
      const data = (await res.json()) as { queueItemId?: string; id?: string };
      queueItemId = data?.queueItemId || data?.id;
    } catch {
      // JSON response is optional for webhooks
    }
    return {
      accepted: true,
      queueItemId,
    };
  } catch (err) {
    return {
      accepted: false,
      failure: 'not_delivered',
      reason: `Webhook request failed: ${String(err)}`,
    };
  }
}

/** 调用浏览器原生 Downloads API 直接执行下载。 */
export async function sendBrowserDownload(req: {
  url: string;
  filenameSuggestion?: string;
}): Promise<HandoverResponse> {
  if (typeof chrome === 'undefined' || !chrome.downloads?.download) {
    return {
      accepted: false,
      failure: 'rejected',
      reason: 'Browser downloads API unavailable',
    };
  }
  return new Promise((resolve) => {
    chrome.downloads.download(
      {
        url: req.url,
        filename: req.filenameSuggestion,
      },
      (downloadId) => {
        const err = chrome.runtime?.lastError;
        if (err || downloadId === undefined) {
          resolve({
            accepted: false,
            failure: 'rejected',
            reason: err?.message || 'Download start failed',
          });
        } else {
          resolve({
            accepted: true,
            queueItemId: String(downloadId),
          });
        }
      },
    );
  });
}
